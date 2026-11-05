package botguard

import (
	"context"
	"time"
)

// Policy is the operator-tunable part of a guard, read on every request so a
// console can change it without a restart.
type Policy struct {
	// Enforce false is observe mode: verdicts are counted and logged, nothing
	// is refused.
	Enforce bool
	// PerMinute and Burst size the budget a client spends before it is asked
	// for a challenge.
	PerMinute float64
	Burst     int
	// CeilingFactor multiplies the budget into a hard ceiling that refuses
	// with 429 even a client holding a valid token.
	CeilingFactor float64
	// Cost is the PBKDF2 iteration count of one proof-of-work attempt;
	// MinCounter and MaxCounter bound how many attempts a client needs.
	Cost         int
	MinCounter   int
	MaxCounter   int
	ChallengeTTL time.Duration
	TokenTTL     time.Duration
	// MinFormDelay is the shortest time a human takes between a form
	// rendering and its submission.
	MinFormDelay time.Duration
}

// DefaultPolicy enforces a budget generous enough that a person searching
// by hand never meets a challenge.
func DefaultPolicy() Policy {
	return Policy{
		Enforce:       true,
		PerMinute:     30,
		Burst:         20,
		CeilingFactor: 4,
		Cost:          1000,
		MinCounter:    2000,
		MaxCounter:    5000,
		ChallengeTTL:  5 * time.Minute,
		TokenTTL:      15 * time.Minute,
		MinFormDelay:  2 * time.Second,
	}
}

// Settings supplies the current policy. Applications back it with their own
// database; a standalone proxy with its config file.
type Settings interface {
	Policy(ctx context.Context) (Policy, error)
}

// StaticSettings serves a fixed policy.
type StaticSettings Policy

func (s StaticSettings) Policy(context.Context) (Policy, error) { return Policy(s), nil }

func (p Policy) normalized() Policy {
	d := DefaultPolicy()
	if p.PerMinute <= 0 {
		p.PerMinute = d.PerMinute
	}
	if p.Burst <= 0 {
		p.Burst = d.Burst
	}
	if p.CeilingFactor < 1 {
		p.CeilingFactor = d.CeilingFactor
	}
	if p.Cost <= 0 {
		p.Cost = d.Cost
	}
	if p.MinCounter <= 0 {
		p.MinCounter = d.MinCounter
	}
	if p.MaxCounter <= p.MinCounter {
		p.MaxCounter = p.MinCounter + 1
	}
	if p.ChallengeTTL <= 0 {
		p.ChallengeTTL = d.ChallengeTTL
	}
	if p.TokenTTL <= 0 {
		p.TokenTTL = d.TokenTTL
	}
	if p.MinFormDelay < 0 {
		p.MinFormDelay = 0
	}
	return p
}
