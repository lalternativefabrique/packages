# websession

Who the signed-in person is, for a product's core.

```
go get github.com/lalternative/packages/go/websession
```

```go
guard, err := websession.New(websession.Config{
    Product:   "tornad",
    Urbangate: "https://id.urbangate.dev",  // the person's own access token (ADR 0009)
    Web:       "https://tornad.dev",         // the token the web signs from its session
})
mux.Handle("/api/v1/", guard.Require(adminAPI))

func handler(w http.ResponseWriter, r *http.Request) {
    u, _ := websession.UserFrom(r.Context())
    u.IdentityID // the person at urbangate, whichever issuer signed
    u.Role       // "admin", "user" or "" for this product
}
```

Two issuers, one verifier. urbangate's access token carries the product as
audience and the roles the token hook wrote, `<product>:admin` or
`<product>:user`, from which `Role` is read. The web's token carries `role`
and `identityId` directly, the way `@lalternative/auth`'s `jwt` plugin
writes them. A core moving to ADR 0009 keeps both while sessions opened
under the web's key live, then drops `Web`.

A dev stack runs no urbangate: the web signs every token itself
(`@lalternative/auth`'s `createDevAuth`), and `Web` is its only issuer.
`ConfigFromEnv(os.Getenv)` reads the three variables every core sets —
`OIDC_AUDIENCE`, `OIDC_ISSUER_URL`, `OIDC_WEB_ISSUER_URL` — so the dev stack
only swaps which issuer it names.

A request without a token, with one neither issuer signed, or with one that
names no subject, is answered 401. A token that cannot be checked because the issuer's keys
cannot be read is answered 503 `identity_provider_unavailable` with
`Retry-After`, never 401: the person's session is fine, urbangate is not.
`Resolve` returns `websession.ErrUnavailable` for that case, for a core that
calls it by hand.

The token is read from `Authorization: Bearer`, else from the
`<product>_token` cookie `@lalternative/auth` sets, so a browser that reaches
the core directly needs no header copied by hand.

## Echo, chi

`Require` is plain `net/http` middleware, so a core mounts it as it is rather
than writing its own:

```go
api := e.Group("/api/v1", echo.WrapMiddleware(guard.Require))  // Echo
r.Use(guard.Require)                                             // chi

u, _ := websession.UserFrom(c.Request().Context())
```

A route only this product's admins may reach takes `guard.RequireRole("admin")`
in place of `Require`: 403 for anyone else. The core checks it itself — a
proxy's `adminOnly` is a courtesy to the browser, not the gate.
