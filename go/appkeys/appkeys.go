// Package appkeys is the product side of urbangate's customer app keys: the
// relay a product's front calls, and the guard its API puts on the hot path.
//
// urbangate issues the keys because it is the one that knows the tenants; a
// product holds no customer credential and verifies offline, so an urbangate
// outage stops the issuing of new keys and nothing else.
package appkeys

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lalternative/packages/go/svcauth"
)

// DefaultMaxStale is how long a revocation list may go unrefreshed before the
// guard stops honouring keys.
const DefaultMaxStale = 5 * time.Minute

// Config describes one product's side of the key path.
type Config struct {
	// Product is the product's id in the suite: the audience its keys carry,
	// and the prefix of the key string it hands its customers.
	Product string

	// Urbangate is the issuer's public URL.
	Urbangate string

	// Provisioner is the product's own machine credential, the
	// "<product>-provisioner" client holding urbangate:keys:issue. It never
	// leaves the server.
	Provisioner svcauth.TokenSource

	// OwnerOf reads the signed-in person's provider identity id from the
	// request. False answers 401: a key is issued against a person, and the
	// relay has no other way to name one.
	OwnerOf func(*http.Request) (string, bool)

	// DefaultScopes are the scopes a creation asks for when the front names
	// none. The vocabulary itself lives in urbangate, never here.
	DefaultScopes []string

	// MaxStale bounds how old the revocation list may be. Zero means
	// DefaultMaxStale.
	MaxStale time.Duration

	// HTTPClient is used for both the relay and the revocation list.
	HTTPClient *http.Client
}

// Keys is a product's key path: Relay serves its front, Require guards its
// API, and Run keeps the revocation list fresh.
type Keys struct {
	product   string
	urbangate string
	prefix    string
	defaults  []string
	maxStale  time.Duration

	provisioner svcauth.TokenSource
	ownerOf     func(*http.Request) (string, bool)
	client      *http.Client

	verifier *svcauth.Verifier
	list     *svcauth.RevocationList
	fresh    freshness
	clock    func() time.Time
}

// KeyPrefix is what a product's key string starts with, before the JWT.
func KeyPrefix(product string) string { return product + "_key_" }

// JWKSURL is where urbangate publishes the key that signs customer keys. It is
// deliberately not Hydra's /.well-known/jwks.json: an app key is not an OAuth2
// access token, and the two documents answer different questions.
func JWKSURL(urbangate string) string {
	return strings.TrimRight(urbangate, "/") + "/api/machine/keys/jwks"
}

// New wires a product's key path. It returns an error rather than panicking on
// a missing field: a product that forgets its provisioner would otherwise
// serve a relay that 500s on every call.
func New(cfg Config) (*Keys, error) {
	if cfg.Product == "" {
		return nil, errors.New("appkeys: Product is required")
	}
	if cfg.Urbangate == "" {
		return nil, errors.New("appkeys: Urbangate is required")
	}
	if cfg.Provisioner == nil {
		return nil, errors.New("appkeys: Provisioner is required")
	}
	if cfg.OwnerOf == nil {
		return nil, errors.New("appkeys: OwnerOf is required")
	}

	base := strings.TrimRight(cfg.Urbangate, "/")
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	maxStale := cfg.MaxStale
	if maxStale <= 0 {
		maxStale = DefaultMaxStale
	}

	// A customer key is signed by urbangate's own key pair, so the issuer is
	// declared with that document rather than Hydra's.
	issuer := svcauth.Issuer{
		URL:       base,
		JWKSURL:   JWKSURL(base),
		Audiences: []string{cfg.Product},
	}
	verifier, err := svcauth.New([]svcauth.Issuer{issuer}, svcauth.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("appkeys: %w", err)
	}

	list, err := svcauth.NewRevocationList(
		base+"/api/machine/revoked",
		cfg.Provisioner,
		svcauth.WithRevocationClient(client),
	)
	if err != nil {
		return nil, fmt.Errorf("appkeys: %w", err)
	}

	return &Keys{
		product:     cfg.Product,
		urbangate:   base,
		prefix:      KeyPrefix(cfg.Product),
		defaults:    append([]string(nil), cfg.DefaultScopes...),
		maxStale:    maxStale,
		provisioner: cfg.Provisioner,
		ownerOf:     cfg.OwnerOf,
		client:      client,
		verifier:    verifier,
		list:        list,
		clock:       time.Now,
	}, nil
}
