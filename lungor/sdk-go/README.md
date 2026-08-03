# lungor/sdk-go

Go client for the Lungor billing API.

```bash
go get github.com/lalternative/packages/lungor/sdk-go
```

## Why it exists

Lungor speaks HTTP and published no client, so every consumer wrote its own.
Synthiz shipped a hand-rolled `LungorClient`; Techtuel would have shipped a
second one. Each re-derived the same auth header, the same status handling, and
its own idea of what "entitled" means — from reading the server's source.

## Where the types come from

`types.gen.go` is generated from `openapi/lungor.json` — Lungor's own swagger,
the one `swag` produces from its handler annotations. Requests are built as
those types and responses decoded into them, so a field added or renamed in the
API is a **compile error here**, not a value silently never read.

The generated pointers stop at that boundary. swag emits OpenAPI 2.0, which has
no `required`, so every generated field is a `*T`; `*bool` for `Entitled` would
make "not entitled" and "no answer" the same value at the call site, where one
must degrade and the other must not. `entitlementFrom` converts, reading a nil
verdict as **not** entitled — the direction that grants nothing.

### Updating after an API change

```bash
# 1. lungor is private, so nothing fetches this for you
cp ../../../lungor/apps/core/docs/contract/openapi3.json openapi/lungor.json

# 2. regenerate, then reconcile whatever the new shape breaks
go generate ./...
go test ./...
```

Lungor's CI fails when its API drifts from the contract it publishes
(`go-contract-up-to-date`), which is what makes step 1 a reminder rather than
something to remember. It is a manual step on purpose: automating it would mean
a write token for this repo living in Lungor's pipeline, and this stack
deliberately removed its private-repo credentials.

## The split with `lungor/core`

Two packages, two jobs, and keeping them apart is the point:

| | holds | example |
|---|---|---|
| `lungor/core` | the **rules** — pure functions over values you already hold | `billing.Entitled`, `Prorate`, `Tier.SmallerThan` |
| `lungor/sdk-go` | the **wire** — who is entitled right now, per the service that owns that fact | `client.Entitlement(ctx, userID)` |

Ask Lungor *who has what*. Ask `lungor/core` *what that means*.

## Reading entitlement

```go
client := sdk.New(os.Getenv("LUNGOR_BASE_URL"), os.Getenv("LUNGOR_APP_KEY"))

ent, err := client.Entitlement(ctx, userID)
switch {
case errors.Is(err, sdk.ErrUnauthorized):
    // Operator mistake: a bad app key. NOT "this customer has not paid" —
    // reading it that way cuts off every paying user at once.
case errors.Is(err, sdk.ErrUnavailable):
    // Transient. Degrade; do not fail the user's request.
case err != nil:
    // ErrNotConfigured / ErrBadRequest — a wiring or caller bug.
}

if ent.Entitled { /* serve the paid tier */ }
```

Read `Entitled`, never `Status`. The mapping from a provider status to access is
Lungor's rule — `past_due` entitles until the paid period lapses, `canceled`
does too — and re-deriving it per consumer is how two products end up
disagreeing about whether a customer is cut off.

A user Lungor has never seen is not an error: it resolves to
`StatusNoSubscription` with `Entitled: false`, which is the right answer for
everyone who has not paid.

## On a hot path: cache it

Entitlement is read on rate-limit and quota checks. An HTTP round trip per
request is not affordable.

```go
ent := sdk.NewCache(client, time.Minute)

e, err := ent.Entitlement(ctx, userID)   // one upstream call per user per minute

ent.Invalidate(userID)  // right after checkout returns, or on a webhook
```

Failures are never cached — caching `ErrUnavailable` would turn a blip into a
full TTL of certain failure. Balances are never cached either: they move on
every metered operation, so requesting units bypasses the cache and goes to
Lungor.

## Balances

```go
ent, _ := client.Entitlement(ctx, userID, "credit")

if left, known := ent.Balance("credit"); known && left < 100 {
    // warn the customer
}
```

A missing unit is **unknown**, not zero. Zero means spent; conflating them
refuses work the customer is allowed to do.

## Changing and cancelling

```go
out, err := client.ChangePlan(ctx, sdk.ChangePlanInput{
    ExternalUserID: userID,
    PlanCode:       "max",
    Direction:      sdk.DirectionUp,   // refuse the move if it is not an upgrade
})
if errors.Is(err, sdk.ErrConsentRequired) {
    // Nothing changed. Show out.ConsentAmount / out.ConsentRecurring, then
    // resend with Agreed:true and AgreedAmountCents set to what was displayed.
}

client.Cancel(ctx, userID, true)          // at period end — keeps what was paid for
client.WithdrawPendingPlan(ctx, userID)   // drop a scheduled downgrade
```

An upgrade applies immediately and is prorated; a downgrade takes effect at the
next renewal (`out.EffectiveAt`). Cancelling at period end does **not** revoke
access — `Entitlement` keeps reporting the user as entitled until that date,
which is what they bought.

`ErrNotFound` means the user has no subscription. That is an ordinary outcome,
not a failure: show "nothing to change" rather than an error.

## Checkout

```go
client := sdk.New(baseURL, appKey, sdk.WithCheckoutIdentity(tenantID, appID))

out, err := client.Checkout(ctx, sdk.CheckoutInput{
    PriceID:        "price_pro",
    ExternalUserID: userID,
    Email:          email,
    Country:        "FR",       // VAT follows the customer's country (EU OSS)
    SuccessURL:     "https://app.example/billing/ok",
})
// redirect the browser to out.RedirectURL
```

The amount is never sent. Lungor prices the tier, so the page shown and the
amount charged cannot disagree.

`WithCheckoutIdentity` is required here and only here: Lungor verifies the app
key **and** that the app id matches it. A caller that only reads entitlement
needs neither id.

## Webhooks

Polling entitlement tells you what is true now; a webhook tells you the moment
it changed. Lungor delivers five types, and they are constants here so a typo
is a compile error rather than an endpoint that silently never fires:

`EventSubscriptionActivated`, `EventSubscriptionRenewed`,
`EventSubscriptionCanceled`, `EventSubscriptionPastDue`,
`EventEntitlementChanged`.

### Registering a destination

```go
ep, err := client.CreateWebhookEndpoint(ctx, sdk.CreateWebhookEndpointInput{
    URL:        "https://app.example/hooks/lungor",
    EventTypes: []string{sdk.EventSubscriptionActivated, sdk.EventSubscriptionPastDue},
})
// ep.Secret is shown ONCE. Store it now — reading the endpoint back never
// discloses it again, and recovering means RotateWebhookSecret.
```

`ListWebhookEndpoints`, `GetWebhookEndpoint`, `UpdateWebhookEndpoint`,
`DeleteWebhookEndpoint` and `RotateWebhookSecret` complete the set. To stop
delivery reversibly, update with `Disabled: &yes` rather than deleting: the
registration and its secret survive.

The URL must be publicly reachable. Lungor refuses private and link-local
addresses at dispatch time, so a `localhost` endpoint registers happily and then
never delivers.

### Receiving one

Verify before reading the body for anything. An endpoint URL is public, so
whatever arrives is attacker-controlled until the signature says otherwise —
acting on an unverified body is how a forged request grants a paid tier.

```go
func handler(w http.ResponseWriter, r *http.Request) {
    body, _ := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))

    d, err := sdk.VerifyWebhook(secret, r.Header, body)
    if err != nil {
        http.Error(w, "bad signature", http.StatusBadRequest)
        return
    }

    if seen(d.SourceEventID) {   // at-least-once: the same fact can arrive twice
        w.WriteHeader(http.StatusOK)
        return
    }

    switch d.Type {
    case sdk.EventSubscriptionActivated:
        // json.Unmarshal(d.Payload, &event)
    }

    w.WriteHeader(http.StatusOK)
}
```

Deduplicate on `SourceEventID`, never on `ID`: the delivery id changes between
retries, the source event id identifies the underlying fact and does not.

Answer 2xx once the delivery is durably recorded. Anything else is a failure
Lungor retries with backoff, so answering 500 on a duplicate turns a harmless
repeat into a delivery that never completes.

Signatures older than `DefaultTolerance` (5 min) are rejected, which is what
stops a captured delivery from being replayed forever. `VerifyWebhookAt` takes
the clock and the window explicitly, for tests and for unusual skew.

## Notes

- `externalUserID` is your own user id, opaque to Lungor. Nothing needs
  registering first.
- The app key identifies the **app**, never a user, and scopes every answer to
  it.
- Pass the API root without the version segment
  (`https://billing.example`) — paths are appended here.
- Default timeout is 5s (`WithTimeout` to change).
