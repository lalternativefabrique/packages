package entitlement

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/lalternative/packages/lungor/policy"
	sdk "github.com/lalternative/packages/lungor/sdk-go"
)

type cachedBalance struct {
	balance sdk.Balance
	at      time.Time
}

// Admit reports whether the subject may consume n more of a metered unit,
// from a live Balance read (cached for Config.AdmitTTL). It records nothing:
// call Consume or Record once the work is done. A refusal is a *LimitError;
// an unreadable ledger is ErrLedgerUnavailable, or admits under
// Config.AdmitOnOutage == Allow.
func (g *Gate) Admit(ctx context.Context, subject, unit string, n int64) error {
	if ok, err := g.passes(ctx, subject); err != nil || ok {
		return err
	}
	if g.cfg.AnonLedger == nil && policy.IsAnon(subject) {
		return g.Allow(ctx, subject, unit, n)
	}
	b, err := g.balance(ctx, g.ledgerFor(subject), subject, unit)
	if missingUnit(b, err) {
		if g.missingLimit() >= Unlimited {
			return nil
		}
		return g.limitError(ctx, subject, unit, 0, 0)
	}
	if err != nil {
		if g.cfg.AdmitOnOutage == Allow {
			slog.Warn("entitlement: ledger unreachable, admitting", "subject", subject, "unit", unit, "err", err)
			return nil
		}
		return fmt.Errorf("%w: %w", ErrLedgerUnavailable, err)
	}
	if b.Unlimited || b.Remaining >= n {
		return nil
	}
	return g.limitError(ctx, subject, unit, b.Limit, b.Consumed)
}

// Consume debits n of a metered unit, deduplicated on idemKey, and refuses
// with a *LimitError when the ledger does. An unreachable ledger is
// ErrLedgerUnavailable: nothing was recorded.
func (g *Gate) Consume(ctx context.Context, subject, unit string, n int64, idemKey string) error {
	if ok, err := g.passes(ctx, subject); err != nil || ok {
		return err
	}
	d, err := g.consume(ctx, subject, unit, n, idemKey)
	if err != nil {
		return err
	}
	if !d.Allowed {
		return g.spendRefusal(ctx, subject, unit, d)
	}
	return nil
}

// Record is Consume that never refuses: the act already happened and only has
// to be metered. A refusal is logged; an unreachable ledger is still an error.
func (g *Gate) Record(ctx context.Context, subject, unit string, n int64, idemKey string) error {
	if ok, err := g.passes(ctx, subject); err != nil || ok {
		return err
	}
	d, err := g.consume(ctx, subject, unit, n, idemKey)
	if err != nil {
		return err
	}
	if !d.Allowed {
		slog.Info("entitlement: recorded past the allowance", "subject", subject, "unit", unit, "reason", d.Reason)
	}
	return nil
}

func (g *Gate) consume(ctx context.Context, subject, unit string, n int64, idemKey string) (sdk.Decision, error) {
	g.forgetBalances(subject, unit)
	d, err := g.ledgerFor(subject).Consume(ctx, sdk.Usage{
		ExternalUserID: subject, Unit: unit, Quantity: n, IdempotencyKey: idemKey,
	})
	if err != nil {
		return sdk.Decision{}, fmt.Errorf("%w: %w", ErrLedgerUnavailable, err)
	}
	return d, nil
}

func (g *Gate) balance(ctx context.Context, ledger Ledger, subject, unit string) (sdk.Balance, error) {
	if g.cfg.AdmitTTL <= 0 {
		return ledger.Balance(ctx, subject, unit)
	}
	key := balanceKey(subject, unit)
	g.mu.RLock()
	c, ok := g.balances[key]
	g.mu.RUnlock()
	if ok && g.now().Sub(c.at) < g.cfg.AdmitTTL {
		return c.balance, nil
	}
	b, err := ledger.Balance(ctx, subject, unit)
	if err != nil {
		return b, err
	}
	g.mu.Lock()
	g.balances[key] = cachedBalance{balance: b, at: g.now()}
	g.mu.Unlock()
	return b, nil
}

func (g *Gate) forgetBalances(subject, unit string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if unit != "" {
		delete(g.balances, balanceKey(subject, unit))
		return
	}
	for key := range g.balances {
		if len(key) > len(subject) && key[:len(subject)] == subject && key[len(subject)] == 0 {
			delete(g.balances, key)
		}
	}
}

func balanceKey(subject, unit string) string { return subject + "\x00" + unit }

func (g *Gate) limitError(ctx context.Context, subject, unit string, limit, used int64) error {
	lerr := &LimitError{Unit: unit, Limit: limit, Used: used}
	if s, err := g.snapshot(ctx, subject); err == nil {
		lerr.Plan = s.Plan
	}
	return lerr
}
