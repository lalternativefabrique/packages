// Package signed builds and checks the presigned /speak URLs a browser plays a
// reading from: an HMAC over one reading's scope, id, text hash and expiry, so
// the browser holds a capability for that reading and never a credential.
package signed

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Params are the fields a signature covers.
type Params struct {
	Scope    string
	ID       string
	TextHash string
	Expires  time.Time
}

var (
	ErrNoSignature  = errors.New("signed: no signature")
	ErrUnknownIssue = errors.New("signed: unknown issuer")
	ErrExpired      = errors.New("signed: signature expired")
	ErrBadSignature = errors.New("signed: signature does not match")
	ErrInvalidField = errors.New("signed: scope and id must hold no control character")
)

const (
	QueryIssuer    = "iss"
	QueryExpires   = "exp"
	QuerySignature = "sig"
)

// MaxFieldLen bounds scope and id, which travel in every signed request body.
const MaxFieldLen = 256

// Validate refuses a scope or id the canonical form could not tell apart from
// another: the signed string joins fields with a newline, so a value carrying
// one could be re-split into different fields under the same MAC.
func (p Params) Validate() error {
	for _, v := range []string{p.Scope, p.ID} {
		if len(v) > MaxFieldLen {
			return fmt.Errorf("%w: longer than %d bytes", ErrInvalidField, MaxFieldLen)
		}
		if strings.IndexFunc(v, unicode.IsControl) >= 0 {
			return ErrInvalidField
		}
	}
	return nil
}

// Sign builds the query parameters authorising one reading until p.Expires,
// with key the application's vvaves key.
func Sign(issuer, key string, p Params) (url.Values, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set(QueryIssuer, issuer)
	q.Set(QueryExpires, strconv.FormatInt(p.Expires.Unix(), 10))
	q.Set(QuerySignature, sign(signingKey(key), p))
	return q, nil
}

// HashText names a text the way the signature covers it.
func HashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Verifier checks signatures against the keys each issuer signs with.
type Verifier struct {
	keys func(issuer string) [][]byte
	now  func() time.Time
}

// NewLookupVerifier checks against whatever lookup returns for an issuer at
// verification time: several secrets during a rotation, none once revoked.
func NewLookupVerifier(lookup func(issuer string) []string) *Verifier {
	if lookup == nil {
		return nil
	}
	return &Verifier{keys: func(issuer string) [][]byte {
		var keys [][]byte
		for _, s := range lookup(issuer) {
			if s != "" {
				keys = append(keys, signingKey(s))
			}
		}
		return keys
	}, now: time.Now}
}

// Verify checks the signature in q against the scope, id and text taken from
// the request body, never from the URL.
func (v *Verifier) Verify(q url.Values, scope, id, text string) error {
	if v == nil {
		return ErrNoSignature
	}
	sig := q.Get(QuerySignature)
	if sig == "" {
		return ErrNoSignature
	}
	keys := v.keys(q.Get(QueryIssuer))
	if len(keys) == 0 {
		return ErrUnknownIssue
	}
	unix, err := strconv.ParseInt(q.Get(QueryExpires), 10, 64)
	if err != nil {
		return ErrBadSignature
	}
	p := Params{Scope: scope, ID: id, TextHash: HashText(text), Expires: time.Unix(unix, 0)}
	if p.Validate() != nil {
		return ErrBadSignature
	}
	var genuine bool
	for _, key := range keys {
		if hmac.Equal([]byte(sig), []byte(sign(key, p))) {
			genuine = true
		}
	}
	if !genuine {
		return ErrBadSignature
	}
	// After the MAC, so a forged and an expired link take the same path
	// until the signature is known to be genuine.
	if v.now().After(p.Expires) {
		return ErrExpired
	}
	return nil
}

func signingKey(key string) []byte {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("vvaves/sign/v1"))
	return mac.Sum(nil)
}

func sign(key []byte, p Params) string {
	fields := []string{
		"scope=" + p.Scope,
		"id=" + p.ID,
		"text=" + p.TextHash,
		"exp=" + strconv.FormatInt(p.Expires.Unix(), 10),
	}
	sort.Strings(fields)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(strings.Join(fields, "\n")))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
