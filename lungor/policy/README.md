# lungor/policy

The rules a consumer of Lungor applies around the SDK: when a tenant is
registered on the ledger, and what happens to a request when the ledger cannot
answer.

```bash
go get github.com/lalternative/packages/lungor/policy
```

## Why it exists

`lungor/sdk-go` removed one duplication — every consumer had hand-rolled the
same HTTP client. A second one was left standing, one layer up: what to *do*
with the answers.

Techtuel and Synthiz each worked out, independently, that a signup must survive
a billing outage, that a 409 on a grant means success, that a ledger which has
never heard of a tenant is repaired while an unreachable one is not. They reached
the same conclusions in different words, in different files, with different bugs
on the way. Synthiz's version even coined the name — *"policy acts on sentinels,
no HTTP status numbers in the application layer"* — without knowing Techtuel had
already written it.

None of that is expressible in an OpenAPI contract, so none of it could live in
the generated SDK.

## The rule

**Open on the way in, closed on the way out.**

Nothing is spent at signup, so nothing has to be proven: a grant that fails
leaves the account created and the grant pending. Something *is* spent at use, so
a balance that cannot be read is not evidence of credit: the request is refused.

Getting this backwards in either direction is expensive. Fail signup on a billing
outage and a provider's downtime becomes an acquisition outage. Serve on an
unreadable balance and you give away what nobody is paying for.

## The distinction everything turns on

Two failures that look alike and demand opposite reactions:

| | meaning | reaction |
|---|---|---|
| `ErrTenantNotProvisioned` | the ledger answered: never heard of them | grant, then read again |
| anything else | the ledger did not answer | surface it; granting cannot help |

Collapsing them is how a consumer fires a doomed grant at a provider that is
already down, on a call that is already failing.

Adapters map transport failures onto the sentinel and nothing else reaches this
package:

```go
return fmt.Errorf("%w: %w", policy.ErrTenantNotProvisioned, err)
```

## Usage

Three ports, all optional except the first. A zero `Grant` is the unconfigured
case and stays silent — there is no ledger to provision against.

```go
grant := &policy.Grant{
    Provisioner: lungorAdapter,  // registers the tenant with their signup plan
    State:       accountsStore,  // remembers it landed, so a failure is retried
    Emails:      betterAuthDB,   // resolves a deliverable address by id
}

// Signup: the account is worth creating even if this fails.
grant.EnsureQuietly(ctx, tenantID, email)

// Use: the read repairs a tenant the ledger turns out not to hold.
reader := &policy.Reader{Ledger: lungorAdapter, Grant: grant}
bal, err := reader.Balance(ctx, tenantID, "credit")
```

`ErrNoLedger` is a state, not an outage: no ledger is configured, or the tenant
is anonymous. The consumer meters locally. Treating it as a failure locks out
every anonymous visitor.

## Repair

The grant at signup is best-effort by design, so an account can end up with no
subscription — refused everything, reading as a zero balance. Balance reads fix
that on their own, but only once the tenant reaches one, and never for an
operator acting on someone else's behalf.

```go
granted, err := policy.Repair(ctx, grant, tenantID)
```

Idempotent, and refuses three things before reaching the ledger — each its own
error, because consumers answer them with different statuses:

| | meaning |
|---|---|
| `ErrNoTenant` | nobody named — an unauthenticated caller, or a missing path segment |
| `ErrAnonymousTenant` | minted per trial, no durable balance to subscribe |
| `ErrNoLedger` | nothing configured to provision against |

Consumers wrap this in one handler each. The wrapping is where they legitimately
differ — route, admin gate, response shape, error-to-status — and these
decisions are where they must not.

## What stays with the consumer

Plans, units, tariffs, and what counts as a billable act. This package never
learns what is being sold — it says *when* to call and *how* to fail.

Debits are deliberately absent. `Balance` is a read; a debit's check and write
must be one operation on the ledger, so it goes straight to the SDK. Gating a
debit on a prior read leaves a window where two callers both pass a check
neither would pass together.

## Anonymous tenants

`AnonPrefix` lives here so the code that mints synthetic ids and the code that
keeps them off the ledger agree by construction. Techtuel held this constant
twice — in its auth middleware and in its quota package — kept in step by a test
whose only job was to notice they had drifted.

An anonymous id is minted for a single trial, so a durable ledger has nothing to
keep a balance against. Metering them remotely either leaks a row per visitor or
hands one balance to everyone behind an address.
