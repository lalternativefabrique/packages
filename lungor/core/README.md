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

## What is not here

The product decisions. Plan catalogues, credit pricing, what a plan grants —
those belong in the application, because that is where they change.

Metering (usage ledger, quota enforcement) and invoicing are the next lots to
extract; they carry a database schema with them, which is why they did not ship
in v0.1.0.

## Versioning

Tagged `lungor/core/vX.Y.Z`, matching the `go/eda/vX.Y.Z` convention in this
repository.
