package policy

import (
	"context"
	"errors"
	"log/slog"
)

// Grant registers tenants on the ledger and remembers that it did.
//
// One type behind three triggers — signup, a balance read that finds no ledger
// entry, and a background sweep — because they differ only in WHEN they notice.
// The operation is the same and is idempotent, so none of them has to be the
// one that succeeds first.
//
// A zero Grant (no Provisioner) is the unconfigured case: every method reports
// "not granted" without erroring, and the consumer stays on whatever it does
// when Lungor is absent.
type Grant struct {
	Provisioner Provisioner
	State       GrantState
	Emails      EmailResolver
}

// Ensure registers the tenant unless the local state already records it.
//
// Reports whether the ledger now holds a subscription: true means a balance
// read will answer, so a caller that just got ErrTenantNotProvisioned can retry
// its read rather than show an empty state.
//
// email may be empty; it is resolved through Emails when the caller does not
// already hold it.
func (g *Grant) Ensure(ctx context.Context, tenantID, email string) (bool, error) {
	if g == nil || g.Provisioner == nil {
		return false, nil
	}
	if g.State != nil {
		granted, err := g.State.Granted(ctx, tenantID)
		if err != nil {
			return false, err
		}
		if granted {
			return true, nil
		}
	}
	email, err := g.resolveEmail(ctx, tenantID, email)
	if err != nil {
		return false, err
	}
	if err := g.Provisioner.Grant(ctx, tenantID, email); err != nil {
		return false, err
	}
	if g.State != nil {
		if err := g.State.MarkGranted(ctx, tenantID); err != nil {
			// The grant landed; only the bookkeeping did not. Not an error for
			// the caller — the tenant works — but the next call will grant
			// again, which idempotency absorbs.
			slog.Error("lungor: granted but not recorded", "tenant", tenantID, "err", err)
		}
	}
	return true, nil
}

// resolveEmail keeps the caller's address when it has one, and looks it up
// otherwise.
//
// The signup path carries the address in the session claim, so it never pays
// for a query; the recovery paths hold only an id and must resolve it.
func (g *Grant) resolveEmail(ctx context.Context, tenantID, email string) (string, error) {
	if email != "" {
		return email, nil
	}
	if g.Emails == nil {
		return "", ErrNoEmail
	}
	resolved, ok, err := g.Emails.Email(ctx, tenantID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNoEmail
	}
	return resolved, nil
}

// EnsureQuietly runs Ensure and swallows the failure, for the paths where the
// grant must not decide whether the request succeeds.
//
// Signup is the case it exists for: an account is worth creating even when
// Lungor cannot be reached, because the state leaves the grant pending for the
// read paths and the sweep to retry. Failing signup on a billing outage turns a
// provider's downtime into an acquisition outage.
//
// This is the open half of the rule this package encodes: open on the way in,
// closed on the way out. Nothing is spent at signup, so nothing has to be
// proven.
func (g *Grant) EnsureQuietly(ctx context.Context, tenantID, email string) {
	if _, err := g.Ensure(ctx, tenantID, email); err != nil {
		slog.Error("lungor: grant failed, left pending", "tenant", tenantID, "err", err)
	}
}

// EnsureOnMissing registers the tenant when — and only when — a ledger read
// reported they are not provisioned.
//
// It turns an empty balance into a resolved one without the user doing
// anything: the read that found nothing is also what triggers the repair, and
// the return value says whether a retry is now worth it.
//
// Any other error passes through untouched. Granting in response to an outage
// would add a doomed call to a call that is already failing.
func (g *Grant) EnsureOnMissing(ctx context.Context, tenantID, email string, readErr error) bool {
	if !errors.Is(readErr, ErrTenantNotProvisioned) {
		return false
	}
	granted, err := g.Ensure(ctx, tenantID, email)
	if err != nil {
		slog.Error("lungor: recovery grant failed", "tenant", tenantID, "err", err)
		return false
	}
	return granted
}
