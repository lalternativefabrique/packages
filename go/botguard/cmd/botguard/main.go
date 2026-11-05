// Command botguard is a reverse proxy that puts bot protection in front of
// any HTTP application, whatever its language.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lalternative/packages/go/botguard"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("botguard", "err", err)
		os.Exit(1)
	}
}

type config struct {
	listen, metricsListen string
	upstream              *url.URL
	protect               []string
	guard                 botguard.Config
}

func loadConfig(getenv func(string) string) (config, error) {
	var c config
	c.listen = orDefault(getenv("BOTGUARD_LISTEN"), ":8080")
	c.metricsListen = getenv("BOTGUARD_METRICS_LISTEN")

	up, err := url.Parse(getenv("BOTGUARD_UPSTREAM"))
	if err != nil || up.Scheme == "" || up.Host == "" {
		return c, errors.New("BOTGUARD_UPSTREAM must be an absolute URL")
	}
	c.upstream = up

	for _, p := range strings.Split(orDefault(getenv("BOTGUARD_PROTECT"), "/"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			c.protect = append(c.protect, p)
		}
	}

	var trusted []netip.Prefix
	for _, s := range strings.Split(getenv("BOTGUARD_TRUSTED_PROXIES"), ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return c, fmt.Errorf("BOTGUARD_TRUSTED_PROXIES: %w", err)
		}
		trusted = append(trusted, p)
	}

	policy := botguard.DefaultPolicy()
	if v := getenv("BOTGUARD_OBSERVE"); v != "" {
		observe, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("BOTGUARD_OBSERVE: %w", err)
		}
		policy.Enforce = !observe
	}
	if err := parseFloat(getenv, "BOTGUARD_PER_MINUTE", &policy.PerMinute); err != nil {
		return c, err
	}
	if err := parseInt(getenv, "BOTGUARD_BURST", &policy.Burst); err != nil {
		return c, err
	}
	if err := parseInt(getenv, "BOTGUARD_COST", &policy.Cost); err != nil {
		return c, err
	}

	c.guard = botguard.Config{
		Audience:       orDefault(getenv("BOTGUARD_AUDIENCE"), up.Host),
		Secret:         []byte(getenv("BOTGUARD_SECRET")),
		Settings:       botguard.StaticSettings(policy),
		TrustedProxies: trusted,
		InsecureCookie: getenv("BOTGUARD_INSECURE_COOKIE") == "true",
		Interstitial:   true,
	}
	return c, nil
}

func run(log *slog.Logger) error {
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		return err
	}
	cfg.guard.Logger = log
	g, err := botguard.New(cfg.guard)
	if err != nil {
		return err
	}

	proxy := httputil.NewSingleHostReverseProxy(cfg.upstream)
	protected := g.Protect(proxy)
	mux := http.NewServeMux()
	mux.Handle(botguard.PathPrefix, g.Handler())
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if matchesAny(r.URL.Path, cfg.protect) {
			protected.ServeHTTP(w, r)
			return
		}
		proxy.ServeHTTP(w, r)
	}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	servers := []*http.Server{{Addr: cfg.listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}}
	if cfg.metricsListen != "" {
		servers = append(servers, &http.Server{Addr: cfg.metricsListen, Handler: g.MetricsHandler(), ReadHeaderTimeout: 10 * time.Second})
	}
	errs := make(chan error, len(servers))
	for _, s := range servers {
		log.Info("botguard listening", "addr", s.Addr, "upstream", cfg.upstream.String())
		go func() { errs <- s.ListenAndServe() }()
	}
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, s := range servers {
		_ = s.Shutdown(shutdown)
	}
	return nil
}

func matchesAny(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func parseFloat(getenv func(string) string, name string, dst *float64) error {
	v := getenv(name)
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	*dst = f
	return nil
}

func parseInt(getenv func(string) string, name string, dst *int) error {
	v := getenv(name)
	if v == "" {
		return nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	*dst = n
	return nil
}
