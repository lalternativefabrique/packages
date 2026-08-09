// Package policy holds the rules a consumer of Lungor applies around the SDK:
// when a tenant is registered on the ledger, and what happens to a request when
// the ledger cannot answer.
//
// The split with lungor/sdk-go is the point. The SDK owns the transport — every
// path and field generated from Lungor's contract. This package owns the
// decisions no contract can express: that a signup grant must not fail a
// signup, that an unreachable ledger is not a reason to grant, that an
// anonymous caller has no business on a durable ledger. Each consumer wrote
// those itself and reached a slightly different answer, which is the same
// divergence the SDK exists to prevent one layer down.
//
// Nothing here knows what is being sold. Plans, units, tariffs and the shape of
// a billable act stay with the consumer; this package only says when to call
// and how to fail.
package policy

import "context"

// Provisioner registers a tenant on the ledger with the plan they are entitled
// to on signup — the free tier in every consumer so far.
//
// Lungor holds the allowance, so a tenant it has never heard of reads as a zero
// balance and can do nothing. Registering is therefore part of signup, not of
// the first purchase.
//
// Implementations must be idempotent: three separate triggers call it, and none
// of them is guaranteed to be the first.
type Provisioner interface {
	Grant(ctx context.Context, tenantID, email string) error
}

// GrantState remembers whether a tenant's grant landed, so a failed one can be
// retried instead of lost.
//
// It is a local record on purpose: the whole point is to survive the ledger
// being unreachable, which rules out asking the ledger. A consumer stores it
// wherever its accounts live (a column, a row, a key) — this package only needs
// the two operations.
//
// Optional. Without it every call re-grants, which idempotency absorbs at the
// cost of a round trip.
type GrantState interface {
	Granted(ctx context.Context, tenantID string) (bool, error)
	MarkGranted(ctx context.Context, tenantID string) error
}

// EmailResolver resolves a tenant's real, deliverable address for the paths
// that hold only an id.
//
// Lungor requires an email on a grant, and a synthetic one would register a
// customer nobody can be billed or contacted at. ok=false is a state rather
// than a failure: an anonymous or deleted tenant has none.
type EmailResolver interface {
	Email(ctx context.Context, tenantID string) (string, bool, error)
}
