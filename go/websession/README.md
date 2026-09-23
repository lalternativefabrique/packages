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

A request without a token, with one neither issuer signed, or with one that
names no subject, is answered 401.
