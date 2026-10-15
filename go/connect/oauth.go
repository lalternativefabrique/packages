package connect

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const pendingTTL = 10 * time.Minute

// Pending is a consent in flight: single use, bound to its owner, and gone
// after pendingTTL.
type Pending struct {
	State     string
	Verifier  string
	Owner     string
	Product   string
	ExpiresAt time.Time
}

func NewPending(owner, product string, now time.Time) (Pending, error) {
	state, err := randomToken()
	if err != nil {
		return Pending{}, err
	}
	verifier, err := randomToken()
	if err != nil {
		return Pending{}, err
	}
	return Pending{State: state, Verifier: verifier, Owner: owner, Product: product, ExpiresAt: now.Add(pendingTTL)}, nil
}

// Check refuses a state that expired or that another owner started.
func (p Pending) Check(owner string, now time.Time) error {
	if p.Owner != owner {
		return ErrStateOfAnother
	}
	if !now.Before(p.ExpiresAt) {
		return ErrStateExpired
	}
	return nil
}

func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DefaultPrompt makes the person sign in to the account that owns the data,
// then choose again what they share.
const DefaultPrompt = "login consent"

// AuthorizeURL names exactly one audience: urbangate refuses a request that
// mixes them.
func AuthorizeURL(issuer, clientID, redirectURI, prompt string, p Pending, product Product) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("audience", product.GrantAudience())
	q.Set("scope", "offline_access "+strings.Join(product.RequestedScopes(), " "))
	q.Set("state", p.State)
	q.Set("code_challenge", Challenge(p.Verifier))
	q.Set("code_challenge_method", "S256")
	if prompt == "" {
		prompt = DefaultPrompt
	}
	q.Set("prompt", prompt)
	return strings.TrimRight(issuer, "/") + "/oauth2/auth?" + q.Encode()
}

// Tokens is a token endpoint's answer. Refresh tokens rotate: the one in each
// answer replaces the one sent.
type Tokens struct {
	Access    string
	Refresh   string
	ExpiresAt time.Time
	Scopes    []string
}

type TokenEndpoint interface {
	Exchange(ctx context.Context, code, verifier string) (Tokens, error)
	Refresh(ctx context.Context, refresh string) (Tokens, error)
	Revoke(ctx context.Context, refresh string) error
}

// Hydra speaks to urbangate's public OAuth2 endpoints as the host's connect
// client.
type Hydra struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	HTTP         *http.Client
}

func (h *Hydra) Exchange(ctx context.Context, code, verifier string) (Tokens, error) {
	return h.token(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {h.RedirectURI},
	})
}

func (h *Hydra) Refresh(ctx context.Context, refresh string) (Tokens, error) {
	return h.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
}

// Revoke revokes the refresh token and, at Hydra, every access token issued
// from it.
func (h *Hydra) Revoke(ctx context.Context, refresh string) error {
	res, err := h.post(ctx, "/oauth2/revoke", url.Values{"token": {refresh}, "token_type_hint": {"refresh_token"}})
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode == http.StatusOK {
		return nil
	}
	return classifyTokenError(res.StatusCode, body)
}

func (h *Hydra) token(ctx context.Context, form url.Values) (Tokens, error) {
	res, err := h.post(ctx, "/oauth2/token", form)
	if err != nil {
		return Tokens{}, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	if res.StatusCode != http.StatusOK {
		return Tokens{}, classifyTokenError(res.StatusCode, body)
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return Tokens{}, fmt.Errorf("%w: token answer unreadable", ErrUnavailable)
	}
	return Tokens{
		Access:    out.AccessToken,
		Refresh:   out.RefreshToken,
		ExpiresAt: time.Now().Add(time.Duration(out.ExpiresIn) * time.Second),
		Scopes:    strings.Fields(out.Scope),
	}, nil
}

func (h *Hydra) post(ctx context.Context, path string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(h.Issuer, "/")+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	// RFC 6749 §2.3.1: both halves are form-encoded before the basic
	// encoding, and Hydra decodes them that way.
	req.SetBasicAuth(url.QueryEscape(h.ClientID), url.QueryEscape(h.ClientSecret))
	res, err := httpClient(h.HTTP).Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return res, nil
}

// classifyTokenError tells a grant that is gone from anything else. Only
// invalid_grant is the person's doing; a refused client is the host's own
// misconfiguration, and a 5xx an outage: neither may cost the person their
// connection.
func classifyTokenError(status int, body []byte) error {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	if status >= 400 && status < 500 && e.Error == "invalid_grant" {
		return ErrGrantRefused
	}
	return fmt.Errorf("%w: urbangate %d %s", ErrUnavailable, status, e.Error)
}

func httpClient(c *http.Client) *http.Client {
	if c == nil {
		return http.DefaultClient
	}
	return c
}
