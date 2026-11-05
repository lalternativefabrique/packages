package botguard

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
)

const (
	// HoneypotField must stay empty; render it hidden from humans.
	HoneypotField = "botguard_hp"
	// StampField carries the signed render time from FormStamp.
	StampField = "botguard_ts"
)

var (
	ErrHoneypot = errors.New("botguard: honeypot filled")
	ErrStamp    = errors.New("botguard: missing or forged form stamp")
	ErrTooFast  = errors.New("botguard: form submitted too fast")
)

// FormStamp is the value to render in a hidden StampField input.
func (g *Guard) FormStamp() string {
	ts := strconv.FormatInt(g.now().UnixMilli(), 10)
	return ts + "." + sign(g.keys.form, ts)
}

// CheckForm refuses a submission whose honeypot is filled or which arrives
// sooner after rendering than the policy's MinFormDelay. Call it after
// ParseForm.
func (g *Guard) CheckForm(r *http.Request) error {
	if r.Form.Get(HoneypotField) != "" {
		g.stats.reject("honeypot")
		return ErrHoneypot
	}
	ts, sig, ok := strings.Cut(r.Form.Get(StampField), ".")
	if !ok || sign(g.keys.form, ts) != sig {
		g.stats.reject("form_stamp")
		return ErrStamp
	}
	ms, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		g.stats.reject("form_stamp")
		return ErrStamp
	}
	p, err := g.policy(r.Context())
	if err != nil {
		g.failOpen(r, "settings", err)
	}
	if g.now().UnixMilli()-ms < p.MinFormDelay.Milliseconds() {
		g.stats.reject("too_fast")
		return ErrTooFast
	}
	return nil
}
