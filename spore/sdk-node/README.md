# @lalternative/spore-sdk

Typed TypeScript client for the Spore API, generated from
[`spore/openapi.json`](../openapi.json) with
[orval](https://orval.dev).

## Install

Published to the public npmjs.org registry under the `@lalternative`
scope. Install normally — no registry override or token needed:

```sh
pnpm add @lalternative/spore-sdk
```

The same ESM package works with Bun:

```sh
bun add @lalternative/spore-sdk
```

## Quickstart — from zero to your first email

You need three things before `sendEmail` will accept a message:

1. an **API key** (`sk_live_…`)
2. an **identity** (your sending domain), in `verified` state
3. an **allowed From address** registered on that identity

### 1. Create an API key

Sign in at [app.sporee.fr](https://app.sporee.fr), open **API keys**,
click **Create**. The plaintext `sk_live_…` is shown **once** — copy it
into your env:

```sh
# .env in your service
SPORE_API_KEY=spore_api_key_here
```

### 2. Configure the client

Call `configureSporeClient` once at boot. After that every generated
function uses the shared axios instance.

```ts
import { configureSporeClient, getSporeAPI } from "@lalternative/spore-sdk";

configureSporeClient({
  apiKey: process.env.SPORE_API_KEY!,
  // baseURL defaults to https://api.sporee.fr — override for staging
  // or local dev (e.g. http://localhost:4110).
});

const api = getSporeAPI();
```

### 3. Register and verify a domain

```ts
const created = await api.createIdentity({ name: "example.com" });
// → publish created.records (DKIM, SPF, DMARC, bounce CNAME) on your DNS

const verified = await api.verifyIdentity(created.domainId);
// verified.status === "verified" once DKIM + SPF resolve correctly
```

You can also do this from the webapp under **Identities → Add domain**;
it gives you the DNS records to copy into your registrar.

### 4. Add an allowed sending address

`POST /emails` rejects any `from` address that is not on the identity's
active allowlist. Register the local-parts you intend to send from:

```ts
await api.addIdentityAddress(created.domainId, {
  localPart: "hello",
  label: "Marketing",
});
```

Other operations on the allowlist:

```ts
await api.disableIdentityAddress(created.domainId, "hello", { reason: "rotated" });
await api.removeIdentityAddress(created.domainId, "hello");
```

You can also manage the allowlist from the webapp at `/identities/:id`,
section **Allowed sending addresses**.

### 5. Send

```ts
await api.sendEmail(
  {
    from: "hello@example.com", // must match an active allowlist entry
    to: ["alice@example.com"],
    subject: "Hello",
    html: "<p>Hi!</p>",
  },
  { headers: { "Idempotency-Key": crypto.randomUUID() } },
);
```

Including an `Idempotency-Key` lets you safely retry the call: a replay
within 24 h returns the original 2xx response without re-sending.
Reusing the key with a different body returns 422.

## Configuration reference

`configureSporeClient(opts)` accepts:

| Option    | Required | Default                    | Notes |
|-----------|----------|----------------------------|-------|
| `apiKey`  | yes\*    | —                          | Sent as `Authorization: Bearer <apiKey>`. Accepts an `sk_live_…` managed key (recommended), an HS256 JWT signed with `JWT_SECRET`, or the static `API_KEY` value (dev only). |
| `baseURL` | no       | `https://api.sporee.fr`    | Override for staging / local dev. |
| `axios`   | no       | —                          | Inject your own `AxiosInstance` (interceptors, retry, telemetry). When set, `apiKey` and `baseURL` are ignored — wire them into your instance directly. |

\* You can omit `apiKey` only when you also pass a custom `axios`
instance that handles auth itself.

## Auth modes accepted by the server

The server reads `Authorization: Bearer <token>` and tries, in order:

1. **Three-segment string** → treated as a JWT, validated against
   `JWT_SECRET`. The `sub` claim becomes `tenant_id`.
2. **Anything else** →
   - if it starts with `sk_live_`, looked up against the managed-keys
     table (bcrypt-hashed at rest, revocation is immediate),
   - otherwise compared with the static `API_KEY` env (tenant fixed to
     `"default"`, dev only).

## Regenerate

The generated code lives under `src/generated/`. Refresh `spore/openapi.json`
from the Spore backend contract, then regenerate every client from the packages
repository root:

```sh
pnpm --filter @lalternative/spore-codegen generate
```

## Build

```sh
pnpm --filter @lalternative/spore-sdk build
```
