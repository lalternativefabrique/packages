# authz

One question, everywhere in the suite: may this subject perform this action
on this resource, in this context? Asked and answered in the shape of the
OpenID AuthZEN Evaluation API, so no core, agent or tool embeds a rule of its
own about a resource.

Module path: `github.com/lalternative/packages/go/authz`.

```go
claims, _ := verifier.Verify(ctx, raw)          // svcauth
subject := authz.SubjectFromClaims(claims)
d, err := pdp.Evaluate(ctx, authz.Request{
    Subject:  subject,
    Action:   authz.Action{Name: "bash", Properties: map[string]any{"command": "ls"}},
    Resource: authz.Resource{Type: "lalter:workspace", ID: "8f3…"},
    Context:  map[string]any{"workspace": "/ws"},
})
switch {
case err != nil:                              // the PDP could not answer: not a denial
case d.Allow:
case d.Reason == authz.ReasonApprovalRequired: // ask a person, then ask again
}
```

## Decision points

- `authz.OPA(fsys, "data.<product>.<policy>")` embeds OPA and evaluates the
  `.rego` files under `fsys`. The named document holds `allow` (bool) and
  may hold `reason` (string) and `context` (object). The engine is a facade
  over the product's domain: whether the subject may read, write or
  administer the resource is decided by the product and passed in
  `Request.Context`; the policies own agent-action rules and shape
  constraints, never a grant.
- `authz.HTTP(baseURL, token, client)` asks a remote PDP at
  `POST /access/v1/evaluation`. A transport failure or a non-2xx answer is
  `ErrUnavailable`, never a denial.
- `authz.Handler(pdp)` serves any PDP over that same API, for a core that
  exposes its decisions to others. Authenticating the caller is the mounting
  code's job.
- `authz.AllowAll()` and `authz.DenyAll()` for the desktop sidecar and tests.

## Wire shape

The subject goes on the wire as `type` (`user` when an owner stands behind it,
`machine` otherwise), `id` (the owner's identity id, or the client id) and
properties `owner`, `end_user`, `client_id`, `roles`, `scopes`. A policy reads
them as `input.subject.properties.roles` and the like. Resources are
`<product>:<type>` plus an id; actions are the product's published verbs.

A denial's reason travels as `context.reason`: `approval_required`,
`denied_by_policy`, `no_grant`.

## Logging

`authz.Logged(pdp, authz.Slog(logger), authz.SpanEvents())` records every
decision, as a log line and as an event on the current span.
