package svcauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	revocationRefresh  = 2 * time.Minute
	revocationMaxBytes = 1 << 22
)

// ErrRevocationUnknown is what Revoked reports before any list has been
// loaded. A customer key verifies offline, so a product that has never read
// the list cannot tell a live key from a revoked one — and an empty list is
// indistinguishable from one it failed to fetch. Callers answer 503 for this,
// never 200: accepting by default would bring every revoked key back to life.
var ErrRevocationUnknown = errors.New("svcauth: no revocation list loaded yet")

// ErrKeyRevoked is a token whose jti the issuer has withdrawn. Its signature
// is still perfectly valid — the list is the only thing that says otherwise.
var ErrKeyRevoked = errors.New("svcauth: key revoked")

type revocationEntry struct {
	JTI       string `json:"jti"`
	ClientID  string `json:"client_id"`
	RevokedAt string `json:"revoked_at"`
	ExpiresAt string `json:"expires_at"`
}

type revocationPage struct {
	AsOf    string            `json:"as_of"`
	Entries []revocationEntry `json:"entries"`
}

// TokenSource supplies the bearer a product authenticates to the list with.
// ClientCredentials satisfies it.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// RevocationList holds the keys a product must refuse although their
// signature verifies.
//
// It is a projection, not a store: the issuer owns the truth and this copy
// trails it by at most one refresh. That delay is the price of verifying
// offline, and it is bounded — which is why the list is mandatory rather
// than an optimisation. A product without one cannot revoke at all.
type RevocationList struct {
	url     string
	tokens  TokenSource
	client  *http.Client
	refresh time.Duration
	now     func() time.Time

	mu      sync.RWMutex
	revoked map[string]struct{}
	asOf    string
	loaded  bool
}

type RevocationOption func(*RevocationList)

// WithRevocationClient replaces the HTTP client, for tests or for a caller
// that needs its own transport.
func WithRevocationClient(c *http.Client) RevocationOption {
	return func(l *RevocationList) { l.client = c }
}

// WithRevocationRefresh sets how long the list may trail the issuer. Shorter
// narrows the window a revoked key keeps working; it does not make the check
// itself cost anything, which is a map lookup either way.
func WithRevocationRefresh(d time.Duration) RevocationOption {
	return func(l *RevocationList) { l.refresh = d }
}

func withRevocationClock(now func() time.Time) RevocationOption {
	return func(l *RevocationList) { l.now = now }
}

// NewRevocationList builds the list a product polls. It fetches nothing until
// Load or Run is called.
func NewRevocationList(listURL string, tokens TokenSource, opts ...RevocationOption) (*RevocationList, error) {
	u, err := url.Parse(listURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("svcauth: revocation url %q is not absolute", listURL)
	}
	l := &RevocationList{
		url:     listURL,
		tokens:  tokens,
		client:  &http.Client{Timeout: 10 * time.Second},
		refresh: revocationRefresh,
		now:     time.Now,
		revoked: map[string]struct{}{},
	}
	for _, o := range opts {
		o(l)
	}
	return l, nil
}

// Revoked reports whether a verified token has been withdrawn.
//
// It reads memory and touches no network, so it belongs on the hot path. The
// error it returns before the first successful load is the one case a caller
// must not treat as "not revoked".
func (l *RevocationList) Revoked(jti string) (bool, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if !l.loaded {
		return false, ErrRevocationUnknown
	}
	if jti == "" {
		return false, nil
	}
	_, found := l.revoked[jti]
	return found, nil
}

// Check is Revoked as a guard: nil means the token may be honoured.
func (l *RevocationList) Check(claims Claims) error {
	revoked, err := l.Revoked(claims.JTI)
	if err != nil {
		return err
	}
	if revoked {
		return ErrKeyRevoked
	}
	return nil
}

// Loaded reports whether any list has been read. A service can refuse to
// declare itself ready until it has one.
func (l *RevocationList) Loaded() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.loaded
}

// Load fetches once. The first call asks for everything; later calls send the
// cursor of the previous answer and receive only what changed since, so a
// steady state transfers nothing.
func (l *RevocationList) Load(ctx context.Context) error {
	l.mu.RLock()
	cursor := l.asOf
	l.mu.RUnlock()

	page, err := l.fetch(ctx, cursor)
	if err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	// A full read replaces the set; a delta adds to it. Without the cursor
	// the issuer sent everything it still enforces, so anything held and not
	// listed has expired on its own and must not linger here.
	if cursor == "" {
		l.revoked = make(map[string]struct{}, len(page.Entries))
	}
	for _, e := range page.Entries {
		if e.JTI != "" {
			l.revoked[e.JTI] = struct{}{}
		}
	}
	if page.AsOf != "" {
		l.asOf = page.AsOf
	}
	l.loaded = true
	return nil
}

// Run loads once and keeps the list fresh until ctx ends. It returns the
// error of the first load, so a service can refuse to start without a list,
// and logs nothing afterwards: later failures leave the last good copy in
// place, which keeps verification working through an outage of the issuer.
func (l *RevocationList) Run(ctx context.Context) error {
	if err := l.Load(ctx); err != nil {
		return err
	}
	go func() {
		ticker := time.NewTicker(l.refresh)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = l.Load(ctx)
			}
		}
	}()
	return nil
}

func (l *RevocationList) fetch(ctx context.Context, cursor string) (revocationPage, error) {
	target := l.url
	if cursor != "" {
		u, err := url.Parse(l.url)
		if err != nil {
			return revocationPage{}, fmt.Errorf("svcauth: revocation url: %w", err)
		}
		q := u.Query()
		q.Set("as_of", cursor)
		u.RawQuery = q.Encode()
		target = u.String()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return revocationPage{}, fmt.Errorf("svcauth: revocation request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if l.tokens != nil {
		token, err := l.tokens.Token(ctx)
		if err != nil {
			return revocationPage{}, fmt.Errorf("svcauth: revocation token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := l.client.Do(req)
	if err != nil {
		return revocationPage{}, fmt.Errorf("svcauth: revocation fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return revocationPage{}, fmt.Errorf("svcauth: revocation list answered %d", resp.StatusCode)
	}

	var page revocationPage
	if err := json.NewDecoder(io.LimitReader(resp.Body, revocationMaxBytes)).Decode(&page); err != nil {
		return revocationPage{}, fmt.Errorf("svcauth: revocation decode: %w", err)
	}
	return page, nil
}
