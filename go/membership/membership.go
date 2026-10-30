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
	// StateNeedsIdentifier is a member the product cannot provision until the
	// person chooses an identifier under the owned domain.
	StateNeedsIdentifier State = "needs_identifier"
	StateErasing         State = "erasing"
	StateErased          State = "erased"
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
	// Unreserve, when set, lets a reserved identifier through: with the claim
	// a sign-up presents to the identifier check, then with an empty claim
	// when the member opens under that address.
	Unreserve func(ctx context.Context, local, claim string) (bool, error)
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
	// ReopenAfter is how long an erased member stays refused before a token
	// of the same identity opens it again. It must outlive the longest token
	// urbangate signed before the deletion revoked the sessions: inside the
	// window a stale token is the tombstone's whole reason, past it only a
	// fresh sign-in can carry the identity.
	ReopenAfter time.Duration
	Logger      *slog.Logger
	Now         func() time.Time
}

func (c *Config) withDefaults() {
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 5
	}
	if c.ReopenAfter <= 0 {
		c.ReopenAfter = 20 * time.Minute
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
	// ErrErased is a token of a member being erased, or erased less than
	// Config.ReopenAfter ago: the tombstone.
	ErrErased = errors.New("member erased")
	// ErrConflict is a resource that exists outside the member's knowledge, or
	// an identity whose identifier the product refuses.
	ErrConflict = errors.New("member in conflict")
	ErrTaken    = errors.New("identifier taken")
	// ErrNeedsIdentifier is returned by Resources.Provision for a member with
	// no identifier, and by Resolve for a member waiting to choose one.
	ErrNeedsIdentifier = errors.New("member needs an identifier")
	// ErrIdentifierNotExpected refuses ChooseIdentifier for a member that is
	// not waiting to choose one.
	ErrIdentifierNotExpected = errors.New("member does not expect an identifier")
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
// first time, and reopened in that state when the identity comes back past
// Config.ReopenAfter of its erasure. ErrErased refuses a token that outlived
// the deletion, ErrConflict a member the product could not provision, and
// ErrNeedsIdentifier, with the member, one waiting for ChooseIdentifier.
func (s *Service) Resolve(ctx context.Context, id Identity) (Member, error) {
	if id.ID == "" {
		return Member{}, errors.New("membership: identity id is required")
	}
	m, err := s.store.Get(ctx, id.ID)
	if err == nil {
		if s.reopens(m) {
			return s.reopen(ctx, id, m)
		}
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
	case StateNeedsIdentifier:
		return ErrNeedsIdentifier
	}
	return nil
}

// reopens is an erased member whose tombstone has done its work: the identity
// belongs to the person, not to the product, and a deletion is not a ban.
func (s *Service) reopens(m Member) bool {
	at, ok := s.ReopensAt(m)
	return ok && !s.cfg.Now().Before(at)
}

// ReopensAt is when a token of the erased member opens it again, for the
// refusal to say so. False for a member that is not erased.
func (s *Service) ReopensAt(m Member) (time.Time, bool) {
	if m.State != StateErased || m.ErasedAt == nil {
		return time.Time{}, false
	}
	return m.ErasedAt.Add(s.cfg.ReopenAfter), true
}

// reopen moves the erased member back to provisioning. Purge dropped the
// product's own rows, so Open runs again in the same transaction. A member
// that never had an address (a tombstone left for an identity met only by
// its deletion) claims the one its e-mail names, like a first open. Two
// requests racing here both end on the reopened member: only one moves it.
func (s *Service) reopen(ctx context.Context, id Identity, m Member) (Member, error) {
	claimed := Member{State: StateProvisioning}
	if m.Address == "" {
		if err := s.claimAddress(ctx, id, &claimed); err != nil {
			return Member{}, err
		}
	}
	within := func(ctx context.Context, m *Member) error {
		if claimed.Address != "" {
			m.Address = claimed.Address
		}
		if claimed.State == StateConflict {
			m.State = StateConflict
			m.LastError = claimed.LastError
		}
		return s.opener(id)(ctx, m)
	}
	_, err := s.store.Transition(ctx, id.ID, StateErased, StateProvisioning, Patch{Resources: json.RawMessage("{}"), NextAttemptAt: s.cfg.Now()}, within)
	if err != nil {
		return Member{}, err
	}
	m, err = s.store.Get(ctx, id.ID)
	if err != nil {
		return Member{}, err
	}
	return m, s.admit(m)
}

func (s *Service) opener(id Identity) Within {
	return func(ctx context.Context, m *Member) error {
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
	}
}

// claimAddress gives the member the address its e-mail names under the owned
// domain, or parks it in conflict when the product refuses that identifier.
func (s *Service) claimAddress(ctx context.Context, id Identity, m *Member) error {
	local, ours := s.p.Identifier.OwnedLocalPart(id.Email)
	if !ours {
		return nil
	}
	address := local + "@" + s.p.Identifier.OwnedDomain
	switch err := s.Available(ctx, local); {
	case err == nil:
		m.Address = address
	case errors.Is(err, ErrTaken), errors.Is(err, ErrReserved), errors.Is(err, ErrInvalidIdentifier):
		m.State = StateConflict
		m.LastError = address + ": " + err.Error()
	default:
		return err
	}
	return nil
}

func (s *Service) open(ctx context.Context, id Identity) (Member, error) {
	now := s.cfg.Now()
	m := Member{IdentityID: id.ID, LocalID: id.ID, State: StateProvisioning, Resources: json.RawMessage("{}"), NextAttemptAt: now, OpenedAt: now, UpdatedAt: now}
	if err := s.claimAddress(ctx, id, &m); err != nil {
		return Member{}, err
	}
	inserted, err := s.store.Insert(ctx, m, s.opener(id))
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
	return s.AvailableWithClaim(ctx, local, "")
}

// AvailableWithClaim is Available where Product.Unreserve may lift ErrReserved
// for the claim.
func (s *Service) AvailableWithClaim(ctx context.Context, local, claim string) error {
	if s.p.Identifier.OwnedDomain == "" {
		return fmt.Errorf("%w: no owned domain", ErrInvalidIdentifier)
	}
	local = strings.ToLower(strings.TrimSpace(local))
	if err := s.p.Identifier.Check(local); err != nil {
		if !errors.Is(err, ErrReserved) || s.p.Unreserve == nil {
			return err
		}
		lifted, uerr := s.p.Unreserve(ctx, local, claim)
		if uerr != nil {
			return uerr
		}
		if !lifted {
			return err
		}
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

// ChooseIdentifier gives a member in StateNeedsIdentifier the address local
// names under the owned domain and sends it back to provisioning. It answers
// what Available answers, ErrTaken when another member won the name meanwhile,
// and ErrIdentifierNotExpected for a member in any other state. Choosing the
// address the member already holds is success, so a repeated submit is safe.
func (s *Service) ChooseIdentifier(ctx context.Context, identityID, local string) (Member, error) {
	m, err := s.store.Get(ctx, identityID)
	if err != nil {
		return Member{}, err
	}
	local = strings.ToLower(strings.TrimSpace(local))
	address := local + "@" + s.p.Identifier.OwnedDomain
	if m.State != StateNeedsIdentifier {
		if m.Address == address && m.State != StateErasing && m.State != StateErased {
			return m, nil
		}
		return Member{}, ErrIdentifierNotExpected
	}
	if err := s.Available(ctx, local); err != nil {
		return Member{}, err
	}
	moved, err := s.store.Transition(ctx, identityID, StateNeedsIdentifier, StateProvisioning, Patch{NextAttemptAt: s.cfg.Now()}, func(_ context.Context, m *Member) error {
		m.Address = address
		return nil
	})
	if err != nil {
		return Member{}, err
	}
	if !moved {
		return Member{}, ErrIdentifierNotExpected
	}
	return s.store.Get(ctx, identityID)
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
