package policy

import "errors"

// ErrTenantNotProvisioned means the ledger holds no subscription for this
// tenant — it answered, and the answer is that it has never heard of them.
//
// Distinct from an unreachable ledger because the two demand opposite
// reactions: this one is repaired by granting, and a retry after the grant
// succeeds. Collapsing them is how a consumer ends up firing a doomed grant at
// a provider that is already down.
//
// Adapters wrap it around the transport error: fmt.Errorf("%w: %w",
// policy.ErrTenantNotProvisioned, err).
var ErrTenantNotProvisioned = errors.New("lungor: tenant not provisioned")

// ErrNoLedger means no remote ledger answers for this tenant: either none is
// configured, or the tenant is anonymous and deliberately kept off it.
//
// Not a failure. It tells the caller that the authority is local, so the
// consumer meters with its own engine rather than refusing. Callers that treat
// it as an outage lock out every anonymous visitor.
var ErrNoLedger = errors.New("lungor: no remote ledger for tenant")

// ErrNoTenant means the caller named no tenant to act on.
//
// Its own error rather than a bare "bad request": on the caller-scoped repair
// it means the request was not authenticated, which a consumer answers 401,
// while the operator route answers 400 for a missing path segment. Same
// condition here, different status there.
var ErrNoTenant = errors.New("lungor: no tenant")

// ErrAnonymousTenant means the id belongs to an anonymous caller, who holds no
// durable balance to establish a subscription against.
var ErrAnonymousTenant = errors.New("lungor: anonymous tenant")

// ErrNoEmail means the tenant has no deliverable address, so no grant can be
// made for them.
//
// Reported rather than papered over: Lungor refuses a grant without one, and
// inventing an address would register an unreachable customer.
var ErrNoEmail = errors.New("lungor: tenant has no email")
