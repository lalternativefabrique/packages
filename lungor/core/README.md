# lungor/core

Subscription billing primitives: tier arithmetic, billing periods, and a Mollie
client. Pure Go, no external dependencies, no database.

Extracted from Techtuel, where all three have been running in production since
mid-2026.

```bash
go get github.com/lalternative/packages/lungor/core@lungor/core/v0.1.0
```

## What is here

**`billing`** — the algebra of paid tiers, with no I/O at all.

```go
tier := billing.Tier{Name: "pro", PriceCents: 500, Rank: 2000, Billable: true}
change := billing.Classify(from, to)          // upgrade / downgrade / none
sub := billing.Subscription{OwnerID: "...", Plan: "pro", ...}
ok := billing.Entitled(sub, now)              // is the subscription live?
cents := billing.Prorate(window, 500, 1200, now, billing.DefaultProrationFloorCents)
```

`Subscription` deliberately carries `ProviderCustomerID` / `ProviderScheduleID`
rather than Mollie-specific names: the tier logic does not know who takes the
payment.

**`billingperiod`** — period arithmetic anchored on a renewal date, plus a
dunning schedule for failed payments.

```go
w := billingperiod.Activate(now, billingperiod.Monthly)
w = billingperiod.WindowContaining(anchor, billingperiod.Monthly, now)
next := billingperiod.NextDunningAction(state, schedule, now)
```

Anchored windows matter: a subscriber who signed up on the 17th has a period
running the 17th to the 17th, not a calendar month. Quota and invoicing both
have to agree on that boundary.

**`mollie`** — a thin HTTP client over the Mollie API (customers, mandates,
subscriptions, payments). `net/http` only.

**`metering`** — an append-only usage ledger with an atomic quota cap.

```go
m := metering.New(metering.Config{AppID: appID, TenantID: tenantID,
    Units: units, Ledger: ledger, Periods: periods})

dec, err := m.ConsumeQuota(ctx, userID, alloc, qty, idempotencyKey)
used, err := m.ConsumedThisPeriod(ctx, userID, "credit")
```

Nothing stores a running total: consumption is a SUM over the ledger, which is
what makes a debit safe to retry under its idempotency key. `PeriodResolver` is
the seam where anchored windows plug in — pair it with `billingperiod` so quota
and invoicing agree on where a period ends.

**`invoicing`** — invoice numbering, PDF rendering and Factur-X output, with
French sequential-numbering rules built in.

Both need tables. `migrations/` carries the schema they expect; it is plain SQL,
apply it with whatever tool the consuming application already uses.

## What is not here

The product decisions. Plan catalogues, credit pricing, what a plan grants —
those belong in the application, because that is where they change.

Subscription persistence (`pgsubscriptions`), the payment reconciler and the
anchored-period reader are the next lot. They are close to generic already, but
they travel with a `Plan` type that carries product-specific fields, so
extracting them means reworking that boundary first.

## Versioning

Tagged `lungor/core/vX.Y.Z`, matching the `go/eda/vX.Y.Z` convention in this
repository.
