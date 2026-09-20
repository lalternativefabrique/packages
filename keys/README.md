# @lalternative/keys

The app-key screen every product mounts: the field that names a key, the table
of the keys someone holds, and the revocation. One implementation, so a person
who holds keys in two products reads the same screen twice.

```bash
pnpm add @lalternative/keys
```

```tsx
import { AppKeys } from "@lalternative/keys"

<AppKeys endpoint="/api/keys" />
```

## The route the component calls is yours, never urbangate's

urbangate issues the keys, and reaching it takes a machine credential — the
`<product>-provisioner` client. That credential must never leave your server
(ADR 0002 of urbangate: nothing admin-shaped is sent to a browser). So the
component talks to a route of your own, which relays.

```
browser            your server              urbangate
   |                    |                       |
   | POST /api/keys     |                       |
   | (the user session) |                       |
   |------------------->|                       |
   |                    | POST /api/machine/keys|
   |                    | Bearer <provisioner>  |
   |                    |---------------------->|
   |                    |    { client_id, key } |
   |   the key, once    |<----------------------|
   |<-------------------|
```

### What your route answers

```
GET    /api/keys      -> { keys: [{ id, label, audience, scopes, createdAt, expiresAt }] }
POST   /api/keys      -> { id, label, audience, scopes, createdAt, expiresAt, secret }
DELETE /api/keys/:id  -> any 2xx
```

`secret` is urbangate's `key` under another name: the component does not need
to know the value is a signed JWT rather than an opaque string, and naming it
`secret` keeps a product free to issue its own.

`expiresAt` is RFC3339, or `null` — which means **does not expire**, not
"unknown". A key issued before keys were signed has no lifetime of its own, and
the table says so rather than warning about it.

## The secret appears once

It exists in the creation response and nowhere else. urbangate signs the key,
returns it and forgets it; no later read returns it. The component holds it in
local state, shows it with the warning that goes with it, and drops it.

There is no "show the key again". A person who lost theirs is issued a new one.

## Log the `error` field, even when you do not show it

urbangate answers three distinct 503s, and they are three different repairs on
three different machines:

| `error` | What is wrong | Who fixes it |
|---|---|---|
| `no_vocabulary` | the product has no `-core` client in urbangate | whoever declares the clients |
| `no_signing_key` | this urbangate cannot sign | whoever deploys it |
| `revocation_not_recorded` | the revocation could not be written | it is retried, or whoever runs the core |

The component shows one message for all three, because the person reading it
did nothing wrong and can only retry. Whoever can repair needs the difference,
and only your logs carry it.

### Retrying a failed revocation is safe

urbangate records the revocation *before* deleting the client, so a 503 means
nothing was deleted — the key is still listed, and retrying deletes and records
in one go. The record is idempotent (`ON CONFLICT (jti) DO NOTHING`), and a
DELETE on a client already gone answers 2xx rather than an error. Retrying is
safe after a failure and after a success you did not see.

Never turn that 503 into "key revoked". The key still works.

## Your own transport, your own words

```tsx
<AppKeys
  transport={{ list, create, revoke }}
  copy={{ title: "Vos clés", labelPlaceholder: "production" }}
/>
```

`KeysTransport` is three functions; supply them and the HTTP client is unused.
`copy` overrides any subset of the French defaults. No CSS ships with the
package: the markup is plain, and your own styles apply.

## Before this works at all

The component and its transport are ready. What the relay reaches is not, yet:

- urbangate's machine API must be deployed. As of this writing it runs on no
  shared environment, so a relay pointing at it gets a 404.
- your product needs a `<product>-core` client (it carries the scope vocabulary)
  **and** a `<product>-provisioner` client (it carries the credential) declared
  in urbangate. A provisioner without a core answers `no_vocabulary` on every
  creation naming a scope.
- `user."identityId"` must hold the person's provider identity id — urbangate
  addresses a key's owner by it. `@lalternative/auth` 0.18.0 and later write it
  on every single sign-on, so an account fills it the next time its holder
  signs in.
