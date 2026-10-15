// Package connect lets a core of the suite read and write another product
// with the consent of the person who connects it (urbangate ADR 0012): one
// grant per owner and product, obtained on urbangate's consent screen,
// sealed at rest, refreshed under a row lock, and relayed to the product.
package connect

import (
	"errors"
	"time"
)

type Product struct {
	ID       string
	Label    string
	CoreURL  string
	Audience string
	Scopes   []string
}

// RequestedScopes is what the host asks urbangate for on the product: reading
// it, unless the product names what the host needs from it.
func (p Product) RequestedScopes() []string {
	if len(p.Scopes) > 0 {
		return p.Scopes
	}
	return []string{ReadScope(p.ID)}
}

// GrantAudience is the one audience a grant is issued for: the product's id
// unless configured otherwise.
func (p Product) GrantAudience() string {
	if p.Audience != "" {
		return p.Audience
	}
	return p.ID
}

// ReadScope is the scope asked of a product that names none.
func ReadScope(product string) string { return product + ":read" }

// Products are those a person may connect, in the order the page shows them.
// A product without a core URL is not offered.
type Products []Product

func (ps Products) Find(id string) (Product, bool) {
	for _, p := range ps {
		if p.ID == id && p.CoreURL != "" {
			return p, true
		}
	}
	return Product{}, false
}

func (ps Products) Offered() Products {
	out := Products{}
	for _, p := range ps {
		if p.CoreURL != "" {
			out = append(out, p)
		}
	}
	return out
}

type Status string

const (
	StatusActive  Status = "active"
	StatusRevoked Status = "revoked"
)

// Connection is an owner's grant on a product. Owner is the host's key: a
// member id, or whatever the host scopes grants by.
type Connection struct {
	Owner           string
	Product         string
	Status          Status
	RefreshToken    string
	AccessToken     string
	AccessExpiresAt time.Time
	Scopes          []string
	Resources       []string
	ConnectedAt     time.Time
}

const accessSkew = 30 * time.Second

// FreshAccess is the access token still worth sending, or "" when it has to
// be refreshed first.
func (c Connection) FreshAccess(now time.Time) string {
	if c.AccessToken == "" || !now.Add(accessSkew).Before(c.AccessExpiresAt) {
		return ""
	}
	return c.AccessToken
}

var (
	ErrUnknownState    = errors.New("unknown or used state")
	ErrStateExpired    = errors.New("state expired")
	ErrStateOfAnother  = errors.New("state started by another owner")
	ErrNotConnected    = errors.New("not connected")
	ErrAccessRemoved   = errors.New("the person removed the access on urbangate")
	ErrUnavailable     = errors.New("unavailable")
	ErrUnknownProduct  = errors.New("unknown product")
	ErrUnknownResource = errors.New("resource not shared")
	ErrNotMappable     = errors.New("resource cannot be linked")
	ErrLinkedElsewhere = errors.New("resource linked elsewhere")
	ErrUnknownEntity   = errors.New("unknown entity")
	ErrEntityExists    = errors.New("entity already exists")
	ErrGrantRefused    = errors.New("invalid_grant")
	ErrTokenRefused    = errors.New("product refused the token")
	ErrRevokeFailed    = errors.New("urbangate did not revoke the grant")
)
