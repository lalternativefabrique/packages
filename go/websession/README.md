# websession

Who the signed-in person is, for a product's core.

```
go get github.com/lalternative/packages/go/websession
```

```go
guard, err := websession.New(websession.Config{
    Product:   "tornad",
    Urbangate: "https://id.urbangate.dev",  // the person's own access token (ADR 0009)
})
mux.Handle("/api/v1/", guard.Require(adminAPI))

func handler(w http.ResponseWriter, r *http.Request) {
    u, _ := websession.UserFrom(r.Context())
    u.IdentityID // the person at urbangate, whichever issuer signed
    u.Role       // "admin", "user" or "" for this product
}
```

One issuer at a time. Only a token urbangate issued to the product's own
client, `<product>-admin`, makes a session; a token of any other client with
the product as audience is refused (`ErrNotASession`), whatever roles it
carries. urbangate's access token carries the product as
audience and the roles the token hook wrote, `<product>:admin` or
`<product>:user`, from which `Role` is read.

A dev stack runs no urbangate: the web signs every token itself
(`@lalternative/auth`'s `createDevAuth`), and `Web` is its only issuer. That
token carries `role` and `identityId` directly. `createDevAuth` signs with a
key derived from the product's name, so anyone can mint one: `New` refuses a
`Config` naming `Web` beside `Urbangate` (`ErrWebBesideUrbangate`), because
that is a dev variable carried into a real deployment.
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

## An app the person connected (urbangate ADR 0012)

An app such as nakoda reaches a product with a token the person granted it
on urbangate's consent screen: a few scopes and the resources they checked.
That token never makes a `User`. The routes an app may call take
`RequireDelegated`, and the handler reads the `Grant`:

```go
mux.Handle("/api/v1/connected/", guard.RequireDelegated("lungor:read")(h))

gr, _ := websession.GrantFrom(r.Context())
gr.Subject                                        // the person
gr.ClientID                                       // the app
gr.Allows("lungor:app:crm", "lungor:tenant:acme") // the resource, then its parents
```

`Allows` takes the resource's id followed by its ancestors' as the product
reads them now, so a tenant granted whole covers the apps created since, and
a resource the person no longer holds is never reached.

A product joins by declaring what it shares and mounting the two routes
urbangate and the apps call, outside `Require`:

```go
mux.Handle("/connect/v1/", guard.Connect(websession.ConnectConfig{
    Scopes: []websession.Scope{{Name: "lungor:read", Label: "Lire ta facturation"}},
    Resources: func(ctx context.Context, identityID string) ([]websession.Resource, error) {
        // what this person holds and may share, ids "<product>:<type>:<id>"
    },
}))
```

- `GET /connect/v1/grantable?subject=<identity>` answers urbangate's
  `urbangate-connect` client only, while it draws the consent screen.
- `GET /connect/v1/resources` answers an app's grant with the resources it
  reaches, each with its `url`, so the app can match them with its own data.

Then set `metadata.connect_url` on the product's `-admin` client in
urbangate: the base URL urbangate reaches the core at.
