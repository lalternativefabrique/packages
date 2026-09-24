# svcauth

Verifies the bearer tokens a service receives against the identity providers
it trusts. One verifier, several issuers: Ory Hydra for the suite (RS256), a
Better Auth web app (EdDSA), or any other publisher of a JWKS.

Module path: `github.com/lalternative/packages/go/svcauth`.

```go
v, err := svcauth.New([]svcauth.Issuer{
    svcauth.Hydra("https://id.vvaves.dev", "tornade"),
})
mux.Handle("POST /speak", svcauth.Require(v)(handler))

// or, when a route accepts more than one credential:
if raw, ok := svcauth.BearerToken(r); ok {
    claims, err := v.Verify(r.Context(), raw)
    // claims.Subject, claims.ClientID, claims.Owner, claims.HasScope("tornade:speak"), claims.HasRole("tornade:admin")
}
```

A token is accepted when its `iss` names a configured issuer, its signature
checks against that issuer's JWKS, it has not expired, and its `aud` meets one
of the audiences declared for the issuer. An issuer declared without audiences
accepts every token it signed. Only RS256 and EdDSA are honoured: `none` and
HMAC over a public key are refused.

`Owner` is the identity behind a personal key: a `client_credentials` token
has its client id as `sub`, and urbangate's token hook adds the person who
created the client as an `owner` claim. It is empty for the suite's own
service accounts and for tokens of a signed-in person.

Keys are fetched on first use and cached ten minutes. An unknown `kid`
triggers one refresh, then a thirty-second cooldown, so a forged token cannot
turn every request into a fetch. When the issuer cannot be reached, the cached
keys keep serving.

A token whose issuer's keys cannot be read — never fetched, or the `kid` is
not among the cached ones — is `ErrUnavailable`, and `Require` answers it 503
with `Retry-After`, never 401: a client told its valid credential is invalid
starts rotating secrets during an outage. An expired token is refused as
invalid without reaching the issuer. A failed fetch is retried after the same
thirty-second cooldown. A caller verifying by hand does the same:

```go
switch _, err := v.Verify(ctx, raw); {
case errors.Is(err, svcauth.ErrUnavailable): // 503, retry later
case err != nil:                             // 401
}
```

## Obtaining a token

The calling service side of the same exchange:

```go
cc := svcauth.HydraClientCredentials("https://id.vvaves.dev", clientID, clientSecret,
    []string{"tornade"}, []string{"tornade:speak"})
req, _ := http.NewRequestWithContext(ctx, http.MethodPost, tornadeURL+"/speak", body)
if err := cc.Authorize(req); err != nil { /* the issuer refused or is unreachable */ }
```

`Token` fetches on first use and reuses the token until thirty seconds before
it expires; `Authorize` sets the `Authorization: Bearer` header.

When the call is made by a library that takes an `*http.Client` rather than
handing you the request, `HTTPClient` wires the same credentials into one:

```go
fetcher := rendersvc.NewClient(vvavesURL, cc.HTTPClient())
```

`Transport` is the same thing over an existing round tripper, for a client that
already has one:

```go
client := &http.Client{Transport: svcauth.Transport(cc, base)}
```

Each request is cloned before its header is set, so the caller's own request is
never modified. A token the issuer refuses fails the request rather than
sending it without one.
