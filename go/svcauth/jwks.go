package svcauth

import (
	"context"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	jwksCacheTTL        = 10 * time.Minute
	jwksRefreshCooldown = 30 * time.Second
	jwksMaxBytes        = 1 << 20
)

type publicKey struct {
	public any
	alg    string
}

func (k publicKey) accepts(m jwt.SigningMethod) bool {
	switch k.public.(type) {
	case *rsa.PublicKey:
		return m.Alg() == jwt.SigningMethodRS256.Alg() && (k.alg == "" || k.alg == m.Alg())
	case ed25519.PublicKey:
		return m.Alg() == jwt.SigningMethodEdDSA.Alg() && (k.alg == "" || k.alg == m.Alg())
	}
	return false
}

type issuerKeys struct {
	url       string
	jwksURL   string
	audiences []string
	client    *http.Client

	mu          sync.Mutex
	keys        map[string]publicKey
	fetchedAt   time.Time
	lastAttempt time.Time
	lastErr     error
}

func newIssuerKeys(is Issuer) *issuerKeys {
	return &issuerKeys{
		url:       trimSlash(is.URL),
		jwksURL:   is.JWKSURL,
		audiences: is.Audiences,
		client:    &http.Client{Timeout: 5 * time.Second},
		keys:      map[string]publicKey{},
	}
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func (ik *issuerKeys) keyFor(ctx context.Context, kid string, forceRefresh bool) (publicKey, error) {
	ik.mu.Lock()
	defer ik.mu.Unlock()

	if !forceRefresh {
		if key, ok := ik.lookupFresh(kid); ok {
			return key, nil
		}
	}
	// A token forged with a random kid, or a caller retrying through an
	// outage, must not turn every request into a fetch against the issuer.
	attempted := !ik.fetchedAt.IsZero() || ik.lastErr != nil
	if attempted && time.Since(ik.lastAttempt) < jwksRefreshCooldown {
		if ik.lastErr == nil {
			return publicKey{}, errUnknownKeyID
		}
		if key, ok := ik.lookupAny(kid); ok {
			return key, nil
		}
		return publicKey{}, ik.lastErr
	}
	ik.lastAttempt = time.Now()

	keys, err := ik.fetch(ctx)
	if err != nil {
		ik.lastErr = fmt.Errorf("%w: %w", ErrUnavailable, err)
		// Stale keys beat refusing every caller while the issuer is down.
		if key, ok := ik.lookupAny(kid); ok {
			return key, nil
		}
		return publicKey{}, ik.lastErr
	}
	ik.keys = keys
	ik.fetchedAt = time.Now()
	ik.lastErr = nil
	if key, ok := ik.lookupAny(kid); ok {
		return key, nil
	}
	return publicKey{}, errUnknownKeyID
}

func (ik *issuerKeys) lookupFresh(kid string) (publicKey, bool) {
	if time.Since(ik.fetchedAt) > jwksCacheTTL {
		return publicKey{}, false
	}
	return ik.lookupAny(kid)
}

func (ik *issuerKeys) lookupAny(kid string) (publicKey, bool) {
	if len(ik.keys) == 0 {
		return publicKey{}, false
	}
	if kid != "" {
		k, ok := ik.keys[kid]
		return k, ok
	}
	if len(ik.keys) == 1 {
		for _, k := range ik.keys {
			return k, true
		}
	}
	return publicKey{}, false
}

type jwksDocument struct {
	Keys []struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Alg string `json:"alg"`
		Use string `json:"use"`
		Crv string `json:"crv"`
		X   string `json:"x"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

func (ik *issuerKeys) fetch(ctx context.Context) (map[string]publicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ik.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := ik.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("svcauth: fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("svcauth: fetch jwks: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, jwksMaxBytes))
	if err != nil {
		return nil, fmt.Errorf("svcauth: read jwks: %w", err)
	}
	var doc jwksDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("svcauth: decode jwks: %w", err)
	}
	keys := map[string]publicKey{}
	for _, k := range doc.Keys {
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		switch {
		case k.Kty == "RSA":
			n, errN := base64.RawURLEncoding.DecodeString(k.N)
			e, errE := base64.RawURLEncoding.DecodeString(k.E)
			if errN != nil || errE != nil || len(n) == 0 || len(e) == 0 {
				continue
			}
			pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
			keys[k.Kid] = publicKey{public: pub, alg: k.Alg}
		case k.Kty == "OKP" && k.Crv == "Ed25519":
			x, err := base64.RawURLEncoding.DecodeString(k.X)
			if err != nil || len(x) != ed25519.PublicKeySize {
				continue
			}
			keys[k.Kid] = publicKey{public: ed25519.PublicKey(x), alg: k.Alg}
		}
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("svcauth: jwks at %s has no usable RSA or Ed25519 key", ik.jwksURL)
	}
	return keys, nil
}
