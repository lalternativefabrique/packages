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

## Notes

- `externalUserID` is your own user id, opaque to Lungor. Nothing needs
  registering first.
- The app key identifies the **app**, never a user, and scopes every answer to
  it.
- Pass the API root without the version segment
  (`https://billing.example`) — paths are appended here.
- Default timeout is 5s (`WithTimeout` to change).
