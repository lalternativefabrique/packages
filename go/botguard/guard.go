// Package botguard keeps abusive bots off an HTTP service with no third
// party: a rate limit first, an ALTCHA proof-of-work only past it, then a
// short-lived signed token. The wire contract is tornad's ADR 0009.
package botguard

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/netip"
	"strings"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go/v2"
)

const (
	// PathPrefix is where Handler serves the challenge, verify and widget
	// routes.
	PathPrefix = "/botguard/"
	CookieName = "botguard"
	HeaderName = "Botguard-Token"

	challengeAlgorithm = "PBKDF2/SHA-256"
	maxVerifyBody      = 16 << 10
)

// Config wires a guard into a deployment.
type Config struct {
	// Audience names the deployment; a token is only valid where it was
	// issued.
	Audience string
	// Secret is the deployment secret, at least 32 bytes.
	Secret   []byte
	Settings Settings
	Limiter  Limiter
	// TrustedProxies are the networks allowed to set X-Forwarded-For.
	TrustedProxies []netip.Prefix
	// ClientAddr overrides how the client address is read, so a service that
	// already resolves it (an Echo IPExtractor, see AddrFrom) counts the same
	// client its other limiters count. TrustedProxies is then ignored.
	ClientAddr func(*http.Request) netip.Addr
	// Exempt lets a request through untouched: signed-in sessions, known API
	// clients.
	Exempt func(*http.Request) bool
	// Subject keys the budget by account instead of by network when it
	// returns a non-empty value.
	Subject func(*http.Request) string
	Logger  *slog.Logger
	// InsecureCookie sends the token cookie over plain HTTP, for local
	// development only.
	InsecureCookie bool
	// Interstitial answers a browser navigation that needs a challenge with
	// a page that solves it and reloads, instead of the JSON 428.
	Interstitial bool
}

// Guard is one deployment's bot protection.
type Guard struct {
	cfg       Config
	keys      keys
	used      *usedChallenges
	deriveKey altcha.DeriveKeyFunc
	stats     *stats
	now       func() time.Time
}

func New(cfg Config) (*Guard, error) {
	if cfg.Audience == "" {
		return nil, errors.New("botguard: Audience is required")
	}
	if len(cfg.Secret) < 32 {
		return nil, errors.New("botguard: Secret must be at least 32 bytes")
	}
	if cfg.Settings == nil {
		cfg.Settings = StaticSettings(DefaultPolicy())
	}
	if cfg.Limiter == nil {
		cfg.Limiter = NewMemoryLimiter()
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Guard{
		cfg:       cfg,
		keys:      deriveKeys(cfg.Secret),
		used:      newUsedChallenges(),
		deriveKey: altcha.DeriveKeyPBKDF2(),
		stats:     &stats{},
		now:       time.Now,
	}, nil
}

func (g *Guard) clientAddr(r *http.Request) netip.Addr {
	if g.cfg.ClientAddr != nil {
		return g.cfg.ClientAddr(r)
	}
	return clientAddr(r, g.cfg.TrustedProxies)
}

func (g *Guard) policy(ctx context.Context) (Policy, error) {
	p, err := g.cfg.Settings.Policy(ctx)
	if err != nil {
		return DefaultPolicy(), err
	}
	return p.normalized(), nil
}

// Handler serves GET challenge, POST verify and GET widget.js under
// PathPrefix. Mount it at PathPrefix, outside Protect.
func (g *Guard) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathPrefix+"challenge", g.serveChallenge)
	mux.HandleFunc("POST "+PathPrefix+"verify", g.serveVerify)
	mux.HandleFunc("GET "+PathPrefix+"widget.js", serveWidget)
	return mux
}

// Protect spends the client's budget on every request and asks for a
// challenge once it is over.
func (g *Guard) Protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.cfg.Exempt != nil && g.cfg.Exempt(r) {
			next.ServeHTTP(w, r)
			return
		}
		p, err := g.policy(r.Context())
		if err != nil {
			g.failOpen(r, "settings", err)
		}
		net := network(g.clientAddr(r))
		key := "net:" + net
		if g.cfg.Subject != nil {
			if s := g.cfg.Subject(r); s != "" {
				key = "sub:" + s
			}
		}
		spend, err := g.cfg.Limiter.Take(r.Context(), key, p)
		if err != nil {
			g.failOpen(r, "limiter", err)
			next.ServeHTTP(w, r)
			return
		}
		switch {
		case spend.OverCeiling:
			g.stats.refusedCeiling.Add(1)
			if p.Enforce {
				refuse(w, http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
				return
			}
			g.observe(r, key, "ceiling")
		case spend.OverBudget && !g.hasValidToken(r, net):
			g.stats.refusedChallenge.Add(1)
			if p.Enforce && g.cfg.Interstitial && wantsPage(r) {
				serveInterstitial(w)
				return
			}
			if p.Enforce {
				refuse(w, http.StatusPreconditionRequired, map[string]string{
					"error":         "challenge_required",
					"challenge_url": PathPrefix + "challenge",
				})
				return
			}
			g.observe(r, key, "challenge")
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Guard) hasValidToken(r *http.Request, net string) bool {
	token := r.Header.Get(HeaderName)
	if token == "" {
		if c, err := r.Cookie(CookieName); err == nil {
			token = c.Value
		}
	}
	if token == "" {
		return false
	}
	if err := g.keys.checkToken(token, g.cfg.Audience, net, g.now()); err != nil {
		g.stats.reject(err.Error())
		return false
	}
	return true
}

func (g *Guard) serveChallenge(w http.ResponseWriter, r *http.Request) {
	p, err := g.policy(r.Context())
	if err != nil {
		g.failOpen(r, "settings", err)
	}
	counter := p.MinCounter + rand.IntN(p.MaxCounter-p.MinCounter)
	expires := g.now().Add(p.ChallengeTTL)
	c, err := altcha.CreateChallenge(altcha.CreateChallengeOptions{
		Algorithm:              challengeAlgorithm,
		DeriveKey:              g.deriveKey,
		HMACSignatureSecret:    string(g.keys.challenge),
		HMACKeySignatureSecret: string(g.keys.keySignature),
		Cost:                   p.Cost,
		KeyLength:              32,
		Counter:                &counter,
		ExpiresAt:              &expires,
	})
	if err != nil {
		g.cfg.Logger.Error("botguard: create challenge", "err", err)
		refuse(w, http.StatusInternalServerError, map[string]string{"error": "unavailable"})
		return
	}
	g.stats.challengesIssued.Add(1)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, c)
}

func (g *Guard) serveVerify(w http.ResponseWriter, r *http.Request) {
	raw, err := readSolution(r)
	if err != nil {
		g.stats.reject("malformed")
		refuse(w, http.StatusBadRequest, map[string]string{"error": "malformed"})
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		g.stats.reject("malformed")
		refuse(w, http.StatusBadRequest, map[string]string{"error": "malformed"})
		return
	}
	var payload altcha.Payload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		g.stats.reject("malformed")
		refuse(w, http.StatusBadRequest, map[string]string{"error": "malformed"})
		return
	}
	res, err := altcha.VerifySolution(altcha.VerifySolutionOptions{
		Challenge:              payload.Challenge,
		Solution:               payload.Solution,
		DeriveKey:              g.deriveKey,
		HMACSignatureSecret:    string(g.keys.challenge),
		HMACKeySignatureSecret: string(g.keys.keySignature),
	})
	switch {
	case err != nil:
		g.stats.reject("malformed")
		refuse(w, http.StatusBadRequest, map[string]string{"error": "malformed"})
		return
	case res.Expired:
		g.stats.reject("expired")
		refuse(w, http.StatusUnprocessableEntity, map[string]string{"error": "challenge_expired"})
		return
	case !res.Verified:
		g.stats.reject("invalid")
		refuse(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_solution"})
		return
	}
	now := g.now()
	if !g.used.claim(payload.Challenge.Signature, time.Unix(payload.Challenge.Parameters.ExpiresAt, 0), now) {
		g.stats.reject("replayed")
		refuse(w, http.StatusUnprocessableEntity, map[string]string{"error": "challenge_used"})
		return
	}
	p, err := g.policy(r.Context())
	if err != nil {
		g.failOpen(r, "settings", err)
	}
	token := g.keys.issueToken(g.cfg.Audience, network(g.clientAddr(r)), p.TokenTTL, now)
	g.stats.challengesSolved.Add(1)
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(p.TokenTTL.Seconds()),
		HttpOnly: true,
		Secure:   !g.cfg.InsecureCookie,
		SameSite: http.SameSiteLaxMode,
	})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_in": int(p.TokenTTL.Seconds())})
}

// readSolution accepts the widget's form field, a JSON body {"altcha": …},
// or the bare base64 payload.
func readSolution(r *http.Request) (string, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxVerifyBody))
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(body))
	switch ct := r.Header.Get("Content-Type"); {
	case strings.HasPrefix(ct, "application/json"):
		var v struct {
			Altcha string `json:"altcha"`
		}
		if err := json.Unmarshal(body, &v); err != nil {
			return "", err
		}
		text = v.Altcha
	case strings.HasPrefix(ct, "application/x-www-form-urlencoded"):
		r.Body = io.NopCloser(strings.NewReader(text))
		if err := r.ParseForm(); err != nil {
			return "", err
		}
		text = r.PostForm.Get("altcha")
	}
	if text == "" {
		return "", errors.New("empty solution")
	}
	return text, nil
}

func (g *Guard) failOpen(r *http.Request, stage string, err error) {
	g.stats.failOpen.Add(1)
	g.cfg.Logger.Warn("botguard: failing open", "stage", stage, "path", r.URL.Path, "err", err)
}

func (g *Guard) observe(r *http.Request, key, verdict string) {
	g.cfg.Logger.Info("botguard: observe", "verdict", verdict, "client", key, "path", r.URL.Path)
}

func refuse(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, body)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
