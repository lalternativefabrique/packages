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
(`ListPlans`) hides. A unit the plan does not list has a limit of **0**:
declare every gated unit on every plan. An allocation `>= 1_000_000_000`
(Lungor's `Unlimited`) is uncapped.

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
api.GET("/me/plan", func(c echo.Context) error { /* gate.Limits(ctx, subject) */ })
e.POST("/webhooks/lungor", gate.Webhook(os.Getenv("LUNGOR_WEBHOOK_SECRET")))
```

Non-HTTP callers use `gate.Allow(ctx, subject, unit, n)`. After changing a
subject's plan yourself (admin `Grant`, `ChangePlan`), call
`gate.Invalidate(ctx, subject)` to re-read it and replace its snapshot.

Refusals reach the client as
`402 {"code":"plan_limit","unit","limit","used","plan"}`; a metered spend the
ledger could not record as `503 {"code":"ledger_unavailable"}`.

## Reads

Memory → `Store` → Lungor, one ledger read per subject at a time
(singleflight), written through to the store. In steady state no request
reaches Lungor. A snapshot older than `Refresh` is still served and re-read in
the background. The webhook re-reads a subject on `entitlement.changed` and
`subscription.*`, so a plan change applies at once; a delivery naming no
subject only clears the memory cache.

A subject Lungor has no subscription for is provisioned through
`lungor/policy`'s `Grant.Ensure`, then re-read. A subject without an active
subscription, and an anonymous one (`policy.IsAnon`), gets the `Fallback`
plan.

## Failure rules

| situation | capacity / boolean | metered `Spend` |
|---|---|---|
| Lungor up | live plan | `Consume` decides |
| Lungor down, snapshot known | last snapshot | 503 `ledger_unavailable` |
| Lungor down, no snapshot, catalogue read earlier | `Fallback` plan limits | 503 |
| Lungor down, never reached since boot | every limit 0 (denied) | 503 |
| `Store` read fails | error (500) | — |
| handler errors or answers 5xx after `Spend` | — | `Release` attempted |

Never more than the fallback plan is granted on a guess. Holdings are never
removed: the gate only refuses creation.

`Release` is refused by Lungor for metered units (consumption already served),
so on today's ledger the refund after a failed handler only lands for capacity
units; the failure is logged.
