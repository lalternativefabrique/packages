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
// Idempotent, because Grant.Ensure is: a tenant already recorded as granted
// answers without a call to Lungor, which is what makes the button behind this
// safe to press twice.
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
	return grant.Ensure(ctx, tenantID, "")
}
