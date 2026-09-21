# ADR 0001: The product relays and verifies, urbangate issues

## Status

Accepted — 2026-09-21

## Context

urbangate's ADR 0007 settles the issuing of a customer's app key: a signed JWT,
valid a year, handed over once, verified offline against a published key and
bounded by a revocation list. The machine API that issues it exists, the front
that shows it exists (`@lalternative/keys`), and the offline verification
exists (`svcauth`).

What sat between them was written by nobody and would have been written by
everybody: the route a product's front calls, the translation to the machine
API, the prefix strip, the signature check against the right JWKS, the
revocation poll. Five products already carry their own local key table — spore,
lalter, lungor, synthiz, vvaves — which is the outcome ADR 0004 names as the
thing to remove rather than to repeat.

## Decision

**urbangate issues; the product relays and verifies.** Neither half moves.

urbangate holds the tenants, so it is the only party that can say who a key
belongs to and what it may carry. A product holds no customer credential in any
form — there is nothing to leak from its database, and nothing to keep in sync.

The relay lives in the product because the machine credential must not reach a
browser (urbangate ADR 0002). The verification lives in the product because a
check on the hot path may not depend on a network call to another service.

This module is that seam, once, for every product.

### The revocation list is a hard dependency, and staleness is refusal

`Require` answers 503 while the list has never loaded, and again once it is
older than `MaxStale`. A poll that silently stopped succeeding would otherwise
bring every revoked key back to life, which is the one failure a key system may
not have.

503 rather than 401 is deliberate: the caller's key may be perfectly valid, and
declaring it invalid sends a customer rotating a credential that was fine.
Refusing to answer and refusing the caller are different answers.

### Errors keep urbangate's own name

The three 503s urbangate can answer are three repairs in three places. The relay
passes each through under its own name so the product's logs say which one, even
though the front shows a single message to a person who can only retry.

## Consequences

- A product integrates customer keys with `New`, `Relay`, `Require` and the
  React component. No table, no migration, no hashing.
- An urbangate outage stops issuing and revoking. Requests already
  authenticated by key keep working while the list stays fresh.
- A revocation reaches a product within `MaxStale`, not instantly. That is the
  price of offline verification, and it is the price every comparable system
  pays.
- Each product still declares its own `-core` and `-provisioner` clients in
  urbangate, and each must populate `user.identityId`. The module cannot
  substitute for either.
- The five local implementations are removed product by product, each with its
  own migration window; that is not this module's job.
