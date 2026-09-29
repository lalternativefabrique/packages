// Package membership is the account lifecycle every product core shares
// (urbangate ADR 0013): one members table keyed by the person's identity id,
// a member created by the first token urbangate signed, erased by the
// account.deletion_requested event, and reconciled against urbangate.
//
// The package drives the state machine and delegates what only the product
// knows to Product: the resources it provisions (a mailbox, a bucket, or
// nothing), the identifier policy of a domain it owns, and the local id its
// own tables are keyed on.
package membership

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type State string

const (
	StateProvisioning State = "provisioning"
	StateReady        State = "ready"
	StateConflict     State = "conflict"
	StateErasing      State = "erasing"
	StateErased       State = "erased"
)

// Identity is what the token carries about the person. Nothing of it but ID
// is stored.
type Identity struct {
	ID    string
	Email string
	Name  string
}

type Member struct {
	IdentityID string
	// LocalID is the id the product's own tables are keyed on: the identity id
	// for an account opened through urbangate, an older id for one that
	// predates it.
	LocalID string
	// Address is the identifier under the product's owned domain, empty
	// otherwise. It stays on an erased member so the address is never reused.
	Address       string
	State         State
	Resources     json.RawMessage
	Attempts      int
	LastError     string
	NextAttemptAt time.Time
	OpenedAt      time.Time
	UpdatedAt     time.Time
	ReadyAt       *time.Time
	ErasedAt      *time.Time
}

// Key is what Resources.List reports a resource under: the address when the
// product owns a domain, the local id otherwise.
func (m Member) Key() string {
	if m.Address != "" {
		return m.Address
	}
	return m.LocalID
}

// Resources is what a product provisions for a member. Both mutations must be
// idempotent: a retry after a crash runs them again on the same member.
type Resources interface {
	// Provision creates the member's resources under m.Key(), a name the core
	// recorded before the call. It returns what the core keeps in
	// Member.Resources. A resource that already exists without this member
	// having created it is ErrConflict, never adopted.
	Provision(ctx context.Context, m Member) (json.RawMessage, error)
	// Erase destroys the member's resources; absent resources are success.
	Erase(ctx context.Context, m Member) error
	// List reports the keys of every resource the external system holds, for
	// reconciliation.
	List(ctx context.Context) ([]string, error)
}

// Identities is urbangate's machine API, used by the reconciliation alone.
type Identities interface {
	WithRole(ctx context.Context, role string) ([]string, error)
}

// Product is what a core plugs into the package. Resources is required; the
// rest has a default.
type Product struct {
	// Name is the product as urbangate names it, e.g. "messag".
	Name      string
	Resources Resources
	// Identifier applies when the product owns a mail domain.
	Identifier IdentifierPolicy
	// Open runs in the transaction that inserts the member, before the insert,
	// so the product writes or adopts its own user row in the same commit. It
	// returns the id the product's tables use, "" for the identity id. The
	// pgx store hands the transaction through pgx.Tx(ctx).
	Open func(ctx context.Context, id Identity) (localID string, err error)
	// Purge deletes the product's own rows for the member, idempotently, in
	// the transaction that writes the tombstone, after Resources.Erase.
	Purge func(ctx context.Context, m Member) error
	// Identities enables Reconcile.
	Identities Identities
}

// Role is the urbangate role a member of the product carries.
func (p Product) Role() string { return p.Name + ":user" }

type Config struct {
	// MaxAttempts is how many failures of Provision or Erase raise OnStuck.
	// The member keeps being retried at MaxBackoff afterwards.
	MaxAttempts int
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	// Lease is how long a claimed member is invisible to other workers.
	Lease     time.Duration
	BatchSize int
	// OnStuck is called each time a member fails past MaxAttempts.
	OnStuck func(m Member, err error)
	Logger  *slog.Logger
	Now     func() time.Time
}

func (c *Config) withDefaults() {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 5 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 10 * time.Minute
	}
	if c.Lease <= 0 {
		c.Lease = time.Minute
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 20
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.Now == nil {
		c.Now = func() time.Time { return time.Now().UTC() }
	}
}

var (
	ErrNotMember = errors.New("not a member")
	// ErrErased is a token of a member erased or being erased: the tombstone.
	ErrErased = errors.New("member erased")
	// ErrConflict is a resource that exists outside the member's knowledge, or
	// an identity whose identifier the product refuses.
	ErrConflict = errors.New("member in conflict")
	ErrTaken    = errors.New("identifier taken")
)

type Service struct {
	store Store
	p     Product
	cfg   Config
}

func New(store Store, p Product, cfg Config) (*Service, error) {
	if p.Name == "" {
		return nil, errors.New("membership: Product.Name is required")
	}
	if p.Resources == nil {
		return nil, errors.New("membership: Product.Resources is required, use Noop for none")
	}
	if store == nil {
		return nil, errors.New("membership: Store is required")
	}
	cfg.withDefaults()
	return &Service{store: store, p: p, cfg: cfg}, nil
}

func (s *Service) Product() Product { return s.p }

// Resolve is what the request middleware calls once the token is verified:
// the member, created in state provisioning when the identity is met for the
// first time. ErrErased refuses a token that outlived the deletion,
// ErrConflict a member the product could not provision.
func (s *Service) Resolve(ctx context.Context, id Identity) (Member, error) {
	if id.ID == "" {
		return Member{}, errors.New("membership: identity id is required")
	}
	m, err := s.store.Get(ctx, id.ID)
	if err == nil {
		return m, s.admit(m)
	}
	if !errors.Is(err, ErrNotMember) {
		return Member{}, err
	}
	m, err = s.open(ctx, id)
	if err != nil {
		return Member{}, err
	}
	return m, s.admit(m)
}

func (s *Service) admit(m Member) error {
	switch m.State {
	case StateErasing, StateErased:
		return ErrErased
	case StateConflict:
		return fmt.Errorf("%w: %s", ErrConflict, m.LastError)
	}
	return nil
}

func (s *Service) open(ctx context.Context, id Identity) (Member, error) {
	now := s.cfg.Now()
	m := Member{IdentityID: id.ID, LocalID: id.ID, State: StateProvisioning, Resources: json.RawMessage("{}"), NextAttemptAt: now, OpenedAt: now, UpdatedAt: now}
	if local, ours := s.p.Identifier.OwnedLocalPart(id.Email); ours {
		address := local + "@" + s.p.Identifier.OwnedDomain
		switch err := s.Available(ctx, local); {
		case err == nil:
			m.Address = address
		case errors.Is(err, ErrTaken), errors.Is(err, ErrReserved), errors.Is(err, ErrInvalidIdentifier):
			m.State = StateConflict
			m.LastError = address + ": " + err.Error()
		default:
			return Member{}, err
		}
	}
	inserted, err := s.store.Insert(ctx, m, func(ctx context.Context, m *Member) error {
		if s.p.Open == nil {
			return nil
		}
		local, err := s.p.Open(ctx, id)
		if err != nil {
			return fmt.Errorf("open: %w", err)
		}
		if local != "" {
			m.LocalID = local
		}
		return nil
	})
	if err != nil {
		return Member{}, err
	}
	if inserted.State == StateConflict {
		s.cfg.Logger.WarnContext(ctx, "member opened in conflict", "product", s.p.Name, "identity_id", inserted.IdentityID, "cause", inserted.LastError)
	}
	return inserted, nil
}

// Get answers the member without creating one.
func (s *Service) Get(ctx context.Context, identityID string) (Member, error) {
	return s.store.Get(ctx, identityID)
}

// Available answers whether local@<owned domain> can be handed to a sign-up:
// ErrInvalidIdentifier, ErrReserved, ErrTaken, or nil. A product without an
// owned domain answers ErrInvalidIdentifier for everything.
func (s *Service) Available(ctx context.Context, local string) error {
	if s.p.Identifier.OwnedDomain == "" {
		return fmt.Errorf("%w: no owned domain", ErrInvalidIdentifier)
	}
	local = strings.ToLower(strings.TrimSpace(local))
	if err := s.p.Identifier.Check(local); err != nil {
		return err
	}
	taken, err := s.store.AddressTaken(ctx, local+"@"+s.p.Identifier.OwnedDomain)
	if err != nil {
		return err
	}
	if taken {
		return ErrTaken
	}
	return nil
}

// RequestErase moves the member to erasing, where the worker picks it up. An
// identity never met here gets a tombstone, so a token of it is refused too.
// Already erasing or erased is success.
func (s *Service) RequestErase(ctx context.Context, identityID string) error {
	m, err := s.store.Get(ctx, identityID)
	if errors.Is(err, ErrNotMember) {
		now := s.cfg.Now()
		_, err := s.store.Insert(ctx, Member{IdentityID: identityID, LocalID: identityID, State: StateErased, Resources: json.RawMessage("{}"), OpenedAt: now, UpdatedAt: now, ErasedAt: &now}, nil)
		return err
	}
	if err != nil {
		return err
	}
	switch m.State {
	case StateErasing, StateErased:
		return nil
	}
	_, err = s.store.Transition(ctx, identityID, m.State, StateErasing, Patch{NextAttemptAt: s.cfg.Now()}, nil)
	return err
}
