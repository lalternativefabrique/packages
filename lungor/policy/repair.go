package policy

import "context"

// Repair establishes a tenant's subscription when the ledger holds none, and
// reports whether it now does.
//
// It exists because the grant at signup is deliberately best-effort: failing a
// signup on a billing outage would turn a provider's downtime into an
// acquisition outage. The cost is an account the ledger has never heard of,
// which reads as a zero balance and is refused everything. Balance reads repair
// that on their own, but only once the tenant reaches one — and nothing lets an
// operator act for someone stuck before that.
//
// Idempotent, but on the ledger rather than on the local record: granting a
// tenant who already holds a subscription is a no-op on Lungor's side, so the
// button behind this stays safe to press twice.
//
// It deliberately does NOT skip the call for a tenant recorded as granted. That
// record only says a grant once succeeded; nothing reconciles it with the
// ledger afterwards, so a tenant the provider no longer holds is marked granted
// forever and every cheap path skips them. Reading it here would make the
// repair refuse exactly the case it exists for.
//
// Consumers wrap it in one handler each. The wrapping is where they differ —
// route, admin gate, response shape, how an error becomes a status — and the
// decisions here are where they must not.
func Repair(ctx context.Context, grant *Grant, tenantID string) (bool, error) {
	if tenantID == "" {
		return false, ErrNoTenant
	}
	if IsAnon(tenantID) {
		// An anonymous id is minted for a single trial and holds no durable
		// balance, so granting one a subscription would leak a tenant per
		// visitor.
		return false, ErrAnonymousTenant
	}
	if grant == nil || grant.Provisioner == nil {
		// No ledger to provision against — the standalone case, where whatever
		// the consumer meters locally answers for everyone.
		return false, ErrNoLedger
	}
	// Email empty: the repair paths hold only an id, so the address is resolved
	// through the grant's own resolver.
	return grant.EnsureNow(ctx, tenantID, "")
}
