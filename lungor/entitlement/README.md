# lungor/entitlement

Plan-entitlement enforcement for every L'Alter app, as Echo v4 middlewares.

```bash
go get github.com/lalternative/packages/lungor/entitlement
```

## The model

| where | says |
|---|---|
| `tenants/lungor.yaml` (Lungor) | who gets what: plans, units, allocations |
| routes | which route requires what: `Require`, `RequireRoom`, `Spend` |
| handlers | nothing — they never read a plan |

This module never knows what is sold. A subject's plan comes from
`Entitlement`, its limits from the subject's own allocations: one `Balance`
read per gated unit (its `Limit`, or `Unlimited`), at most 4 in parallel.
That works for `visibility: staff` plans, which the public catalogue
(`ListPlans`) hides. A unit the plan does not list has a limit of **0** by
default (`Config.MissingUnit = MissingUnitDenied`): declare every gated unit
on every plan. `MissingUnitUnlimited` leaves unlisted units uncapped instead,
so only the units a plan names are enforced. A "missing" unit is one Lungor
answers `ErrNotFound` for, or with a balance that is neither periodic, nor
capacity, nor unlimited and has no ceiling. An allocation
`>= 1_000_000_000` (Lungor's `Unlimited`) is uncapped.

Gated units are the `Counters` keys, the units passed to `Require`,
`RequireRoom*` and `Spend`, and `Config.Units`. A unit checked only through
`gate.Allow` must be listed in `Units`.

The `Fallback` plan (no subscription, anonymous subject, or Lungor down with
no snapshot) still takes its limits from `ListPlans`, kept in memory.

## Wiring

```go
gate := entitlement.New(entitlement.Config{
    Ledger:   lungorClient,              // *sdk.Client satisfies entitlement.Ledger
    Store:    entitlement.PG(pool),      // apply entitlement.Migration first
    Grant:    &policy.Grant{Provisioner: lungorAdapter, Emails: users},
    Fallback: "free",
    Subject:  func(c echo.Context) (string, error) { return auth.UserID(c) },
    Counters: map[string]entitlement.Counter{"domain": domains.CountOwned},
    Refresh:  24 * time.Hour,
})

e.HTTPErrorHandler = entitlement.ErrorHandler(e.DefaultHTTPErrorHandler)
e.Use(middleware.RequestID()) // Spend keys its debit on the request id

api.POST("/signatures", h.CreateSignature, gate.Require("custom_signature"))
api.POST("/domains", h.CreateDomain, gate.RequireRoom("domain", 1))
api.POST("/files", h.Upload, gate.RequireRoomFrom("drive_gb", sizeInGB))
api.POST("/ai/write", h.Write, gate.Spend("ai_write", 1))
api.POST("/emails", h.Send, gate.SpendWith("email", 1, entitlement.SpendOpts{Key: messageID}))
api.GET("/me/plan", func(c echo.Context) error { /* gate.Limits(ctx, subject) */ })
e.POST("/webhooks/lungor", gate.Webhook(os.Getenv("LUNGOR_WEBHOOK_SECRET")))
```

Non-HTTP callers use `gate.Allow(ctx, subject, unit, n)` for capacity and
boolean units, and the metered API below. After changing a subject's plan
yourself (admin `Grant`, `ChangePlan`), call `gate.Invalidate(ctx, subject)`
to re-read it and replace its snapshot.

Refusals reach the client as
`402 {"code":"plan_limit","unit","limit","used","plan"}`; a metered spend the
ledger could not record as `503 {"code":"ledger_unavailable"}`.

The webhook runs an optional hook on every verified delivery, before the
subject is re-read; an error answers 500 so Lungor retries:

```go
e.POST("/webhooks/lungor", gate.Webhook(secret, entitlement.OnDelivery(
    func(ctx context.Context, d sdk.Delivery) error { return billing.Apply(ctx, d) },
)))
```

## Metered units outside HTTP

A job runner, a NATS consumer or an inbound mail path meters without an Echo
context:

```go
err := gate.Admit(ctx, userID, "synthesis", 1)        // before the work starts
err := gate.Consume(ctx, userID, "synthesis", 1, "synthesis:"+id) // once it is done
err := gate.Record(ctx, tenantID, "email", 1, "inbound:"+msgID)  // never refuses
```

| call | reads | refuses | on outage |
|---|---|---|---|
| `Admit` | live `Balance.Remaining >= n`, cached `Config.AdmitTTL` (default 0: every call reads) | `*LimitError{Unit, Limit, Used: Consumed, Plan}` | `ErrLedgerUnavailable`, or admits under `AdmitOnOutage: Allow` |
| `Consume` | `Consume` with the caller's idempotency key | `*LimitError` | `ErrLedgerUnavailable` |
| `Record` | `Consume` with the caller's idempotency key | never: a refusal is logged, the act already happened | `ErrLedgerUnavailable` |

`Admit` records nothing: pair it with `Consume` or `Record`. Derive the
idempotency key from the act (a message id, a job id), never from a clock. A
`Consume`, `Record` or `Spend` drops the subject's cached balance for that
unit, `Invalidate` drops all of them. `Spend` and `Consume` always deny on an
outage, whatever `AdmitOnOutage` says: a debit that was not recorded is a
unit nobody is billed for.

## Grace

Lungor's `Entitlement.Entitled` is the verdict. By default a subject whose
subscription is not entitled, whatever its `Status`, gets the `Fallback`
plan. `Config.EntitledStatuses` (default `{"active", "trialing"}`) names the
statuses that keep a subject on **its plan's limits** even when
`Entitled == false`:

```go
EntitledStatuses: []string{"active", "trialing", "past_due", "canceled"},
```

`past_due` is a grace period the customer can still fix; `canceled` is a
cancellation that still runs until the paid period ends. Listing them means
the app keeps serving the plan while Lungor reports the subscription as not
entitled; a subscription Lungor has ended (`unpaid`, `paused`, or no
subscription at all) is never in the list and falls back. The limits still
come from the subject's own balances, so an allowance Lungor has already
withdrawn is withdrawn here too.

## Dev mode

A `Config` without a `Ledger` builds a gate that passes every check and meters
nothing; `New` logs a warning so the mode cannot go unnoticed. `Store` then
defaults to memory. `entitlement.AllowAll()` is the same ledger, explicit.

A local free plan, for a standalone deployment or the anonymous engine, is
`entitlement.StaticLedger(plan, limits)`: every subject is on `plan`, each
unit is metered in memory per subject and deduplicated on the idempotency
key, nothing survives a restart.

```go
gate := entitlement.New(entitlement.Config{
    Ledger: entitlement.StaticLedger("free", map[string]int64{"synthesis": 30, "article": entitlement.Unlimited}),
    Store:  entitlement.NewMemoryStore(),
    Units:  []string{"synthesis", "article"},
})
```

## Bypass

`Config.Bypass` names the subjects every check passes for — platform admins,
who must not lock themselves out of the service nor appear on an invoice:

```go
Bypass: func(ctx context.Context, subject string) (bool, error) { return admins.Is(ctx, subject) },
```

A bypassed subject is never read from Lungor nor metered; an error from
`Bypass` fails the check. Handlers that gate something themselves read it
through `gate.Bypassed(c)`.

## Error shaping

`ErrorHandler` takes options. The body always carries `code: plan_limit`:

```go
e.HTTPErrorHandler = entitlement.ErrorHandler(e.DefaultHTTPErrorHandler,
    entitlement.WithLimitStatus(http.StatusUnprocessableEntity), // default 402
    entitlement.WithMessage(func(e *entitlement.LimitError) string {
        return fmt.Sprintf("plan %s allows %d %s", e.Plan, e.Limit, e.Unit)
    }), // adds "message"
)
```

## Anonymous

A subject `policy.IsAnon` reports (prefix `anon:`) is a cookie, not a
customer: Lungor never hears of it. By default it gets the `Fallback` plan
from the catalogue and its debits still go to `Ledger`, which is Lungor's to
refuse. `Config.AnonLedger` routes those subjects to another ledger instead,
for every read and every debit: typically `StaticLedger` with the anonymous
allowance, or an app's own engine. Plan and limits then come from that
ledger's `Entitlement` and `Balance`.

## Reads

Memory → `Store` → Lungor, one ledger read per subject at a time
(singleflight), written through to the store. In steady state no request
reaches Lungor. A snapshot older than `Refresh` is still served and re-read in
the background. The webhook re-reads a subject on `entitlement.changed` and
`subscription.*`, so a plan change applies at once; a delivery naming no
subject only clears the memory cache. The subject is read from
`ExternalUserID` or `external_user_id`, flat or under `data`.

A subject Lungor has no subscription for is provisioned through
`lungor/policy`'s `Grant.Ensure`, then re-read. A subject without an active
subscription, and an anonymous one (`policy.IsAnon`), gets the `Fallback`
plan.

## Failure rules

| situation | capacity / boolean | metered `Spend` / `Consume` / `Record` | `Admit` |
|---|---|---|---|
| Lungor up | live plan | `Consume` decides | live balance |
| Lungor down, snapshot known | last snapshot | 503 `ledger_unavailable` | `AdmitOnOutage`: Deny (default) or Allow |
| Lungor down, no snapshot, catalogue read earlier | `Fallback` plan limits | 503 | same |
| Lungor down, never reached since boot | every limit 0 (denied) | 503 | same |
| `Store` read fails | error (500) | — | — |
| handler errors or answers 5xx after `Spend` | — | `Release` attempted | — |
| `Bypass` subject, or no `Ledger` | allowed | allowed, not metered | allowed |

Never more than the fallback plan is granted on a guess. Holdings are never
removed: the gate only refuses creation.

`Release` is refused by Lungor for metered units (consumption already served),
so on today's ledger the refund after a failed handler only lands for capacity
units; the failure is logged.
