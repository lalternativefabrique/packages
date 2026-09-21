# appkeys

The product side of urbangate's customer app keys: the relay a product's front
calls, and the guard its API puts on the hot path.

```
go get github.com/lalternative/packages/go/appkeys
```

## Three lines

```go
keys, err := appkeys.New(appkeys.Config{
    Product:   "tornad",
    Urbangate: "https://id.urbangate.dev",
    Provisioner: svcauth.HydraClientCredentials(
        "https://id.urbangate.dev", clientID, clientSecret,
        []string{"urbangate"}, []string{"urbangate:keys:issue"},
    ),
    OwnerOf:       identityIDOfSession, // user.identityId, not the local id
    DefaultScopes: []string{"search"},
})

go keys.Run(ctx)

mux.Handle("/api/keys", http.StripPrefix("/api/keys", keys.Relay()))
mux.Handle("/api/keys/", http.StripPrefix("/api/keys", keys.Relay()))
mux.Handle("/search", keys.Require("tornad:search")(searchHandler))
```

`Relay()` is what `<AppKeys endpoint="/api/keys"/>` from `@lalternative/keys`
calls; the two speak the contract that package's README documents. Nothing else
is needed on either side.

## What each piece is for

**`Relay()`** forwards the front's three calls to urbangate's machine API,
authenticated with the product's `<product>-provisioner` credential. That
credential is used in this process and reaches no browser — it is the whole
reason the front talks to the product rather than to urbangate.

**`Require(scopes...)`** verifies a key offline: the product's prefix, the
signature against urbangate's key JWKS, the audience, the revocation list, then
the scopes. `ClaimsFrom(ctx)` reads `Owner`, `ClientID` and `Scopes`.

**`Run(ctx)`** loads the revocation list and refreshes it. **`Ready()`** says
whether keys can be honoured — put it behind `/readyz`, not `/healthz`: a
process that is alive and cannot check revocations should leave the rotation,
not restart.

## The staleness rule

`Require` refuses with **503** while the revocation list has never loaded, and
again once it is older than `MaxStale` (five minutes by default).

A revoked key must never be honoured because a poll failed. Refusing to answer
and refusing the caller are different things, and only the first is true here —
which is why it is a 503 and never a 401. Telling a customer their valid key is
invalid sends them rotating a key that was fine.

An urbangate outage therefore does not affect requests already authenticated by
key, as long as the list keeps refreshing: only issuing and revoking stop.

## Log the `error` field

urbangate answers three distinct 503s, and the relay passes each one through
under its own name:

| `error` | What is wrong | Where it is fixed |
|---|---|---|
| `no_vocabulary` | the product has no `-core` client in urbangate | whoever declares the clients |
| `no_signing_key` | this urbangate cannot sign | whoever deploys it |
| `revocation_not_recorded` | the revocation could not be written | retried, or whoever runs the core |

The front shows one message for all three, because the person reading it did
nothing wrong and can only retry. Whoever can repair needs the difference, and
only the product's logs carry it.

**A failed revocation deleted nothing.** urbangate records before it deletes, so
a 503 leaves the key listed and working. Retrying is safe — the record is
idempotent and a delete on a client already gone answers 2xx. Never report it as
"key revoked".

## What a product needs before this works

- `<product>-core` **and** `<product>-provisioner` clients declared in
  urbangate. The core carries the scope vocabulary; a provisioner without one
  answers `no_vocabulary` on every creation naming a scope.
- `user.identityId` populated with the provider identity id.
  `@lalternative/auth` 0.18.0 and later write it on every single sign-on, so an
  account fills it the next time its holder signs in.
- urbangate's machine API deployed and reachable.
