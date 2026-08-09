package policy

import (
	"context"
	"strings"
)

// AnonPrefix marks a synthetic tenant id minted for an anonymous caller.
//
// Defined here so the consumers that mint such ids and the code that keeps them
// off the ledger agree by construction. Techtuel held this constant twice — in
// its auth middleware and in its quota package — kept in step by a test whose
// only job was to notice they had drifted.
const AnonPrefix = "anon:"

// IsAnon reports whether an id belongs to an anonymous caller.
func IsAnon(tenantID string) bool { return strings.HasPrefix(tenantID, AnonPrefix) }

// Ledger is the port onto Lungor's balance read, satisfied by an adapter over
// the SDK. Only the read is here: a debit must go straight to the ledger, whose
// check and write are one operation.
type Ledger interface {
	Balance(ctx context.Context, tenantID, unit string) (Balance, error)
}

// Balance is a tenant's standing for one unit within the current period.
type Balance struct {
	Consumed  int64
	Limit     int64
	Remaining int64
}

// Reader reads balances and repairs the tenant when the ledger turns out not to
// hold them.
//
// A zero Reader (no Ledger) reports that no remote ledger answers, which is the
// standalone case: the consumer falls back to whatever it meters locally.
type Reader struct {
	Ledger Ledger
	Grant  *Grant
}

// LedgerFor returns the ledger answering for this tenant, or nil when none
// does.
//
// Anonymous callers stay off it: their id is minted for one trial, so there is
// nothing for a durable ledger to keep a balance against. Metering them
// remotely would either leak a row per visitor or hand one balance to everyone
// behind an address.
func (r *Reader) LedgerFor(tenantID string) Ledger {
	if r == nil || r.Ledger == nil || IsAnon(tenantID) {
		return nil
	}
	return r.Ledger
}

// Balance reads the tenant's standing, granting and reading again when the
// ledger turns out to hold no subscription for them.
//
// This is the recovery path for a tenant whose signup grant never landed —
// Lungor down at the time, or an account created before signup granted at all.
// Such a tenant has no allowance and is refused everything, so the read that
// discovers it is the natural place to repair it.
//
// An unreachable ledger is passed through untouched. This is the closed half of
// the rule: nothing is served on a balance that could not be proven. Consumers
// surface it as a 5xx rather than admitting the request — a lookup failure is
// not evidence of credit.
func (r *Reader) Balance(ctx context.Context, tenantID, unit string) (Balance, error) {
	ledger := r.LedgerFor(tenantID)
	if ledger == nil {
		return Balance{}, ErrNoLedger
	}
	bal, err := ledger.Balance(ctx, tenantID, unit)
	if err == nil || r.Grant == nil {
		return bal, err
	}
	if !r.Grant.EnsureOnMissing(ctx, tenantID, "", err) {
		return Balance{}, err
	}
	return ledger.Balance(ctx, tenantID, unit)
}
