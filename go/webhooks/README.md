# webhooks

Outgoing HTTP webhooks for product APIs: subscriber-managed endpoints, HMAC-signed
delivery, retries with backoff, and an event-sourced audit of every attempt.

Extracted from Spore, which runs it in production.

## What it gives you

- **Endpoint management** — CRUD + secret rotation, event-sourced, with Postgres
  and NATS KV read models.
- **Signed delivery** — HMAC-SHA256, replay-resistant, one signing implementation
  shared by every product.
- **SSRF protection** — DNS is resolved before connecting and private, loopback,
  link-local and cloud-metadata addresses are refused. Redirects are bounded and
  restricted to http(s).
- **Retry classification** — 2xx succeeds; 4xx (except 408/429) is permanent;
  408/429, 5xx and network errors retry with backoff.

## Using it

```go
svc, err := webhooks.NewService(webhooks.ServiceDeps{
    NC:      nc,
    Pool:    pool,
    Brand:   "Lungor",
    Catalog: events.Catalog{"subscription.renewed", "subscription.canceled"},
    Source: dispatcher.Source{
        StreamName:    "FINANCE_EVENTS",
        SubjectFilter: "finance.>",
        PublicType: func(upstream string) string {
            switch upstream {
            case "subscription-renewed":
                return "subscription.renewed"
            }
            return ""
        },
    },
})
svc.StartBackground(ctx)
svc.RegisterRoutes(v1)
```

`Brand` names the outgoing headers, `Catalog` declares what subscribers may
register for, and `Source` says which upstream stream to fan out. Omit `Source`
to manage endpoints without publishing anything yet.

Requires the `webhook_endpoints` table (see `migrations/`).

## What subscribers receive

```http
POST /your/hook
Lungor-Signature: v1=<hex>
Lungor-Timestamp: 1700000000
Lungor-Event: subscription.renewed
Lungor-Delivery-ID: <uuid>
Lungor-Source-Event-ID: <uuid>

{"type": "...", "id": "...", "createdAt": "...", "data": {...}}
```

The header prefix follows `Brand`; everything else is identical across products.

### Verifying a signature

Compute `HMAC-SHA256(secret, "<timestamp>.<raw body>")` and compare, in constant
time, against the hex digest after `v1=`. Sign the **raw** body, before any JSON
parsing — re-serializing changes the bytes and breaks the comparison. Reject
timestamps far from your clock to bound replay.

## Delivery guarantees

At-least-once. Delivery ids are derived from `(endpointId, sourceEventId)`, so a
replay reuses the id rather than fanning out a duplicate — **subscribers must
still be idempotent**, keyed on `<Brand>-Delivery-ID`.

## Scoping

Endpoints are scoped by `TenantID`, which the package treats as an **opaque
key** and never interprets. Spore stores its tenant id there; a service scoping
by application stores that instead.
