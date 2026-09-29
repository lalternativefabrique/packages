# membership

The account lifecycle every product core shares (urbangate ADR 0013). One
`members` table keyed by the person's identity id, a member opened by the
first token urbangate signed, erased by `account.deletion_requested`, and
reconciled against urbangate. The product's own tables stay as they are: the
package tells the product when to write them, in the same transaction.

```
go get github.com/lalternative/packages/go/membership
```

## What a product plugs in

```go
svc, err := membership.New(mpgx.New(pool), membership.Product{
    Name:      "messag",
    Resources: stalwartResources,                       // or membership.NewNoop(store)
    Identifier: membership.IdentifierPolicy{            // only with an owned mail domain
        OwnedDomain: "messag.eco",
        Reserved:    []string{"codesyl", "sylvain"},   // on top of StandardReserved
    },
    Open: func(ctx context.Context, id membership.Identity) (string, error) {
        tx, _ := mpgx.Tx(ctx)                            // same transaction as the member
        // upsert the product's own user row, answer the id its tables use
        return localID, nil
    },
    Purge: func(ctx context.Context, m membership.Member) error {
        tx, _ := mpgx.Tx(ctx)                            // same transaction as the tombstone
        _, err := tx.Exec(ctx, `DELETE FROM notes WHERE user_id = $1`, m.LocalID)
        return err
    },
    Identities: urbangateClient,                         // reconciliation only
}, membership.Config{OnStuck: alert})
```

`Resources` is the only required field:

```go
type Resources interface {
    Provision(ctx context.Context, m Member) (json.RawMessage, error) // idempotent; ErrConflict, never adoption
    Erase(ctx context.Context, m Member) error                        // idempotent
    List(ctx context.Context) ([]string, error)                       // keys, for reconciliation
}
```

`Provision` creates the resource under `m.Key()`, the address when the product
owns a domain and the local id otherwise, a name the core recorded before the
call. A resource that already exists without this member having made it is
`ErrConflict`: the member is parked, an operator is alerted, nothing is reset.

## The lifecycle

```
first token ──► provisioning ──worker──► ready ──deletion_requested──► erasing ──worker──► erased
                     │                                                   ▲
                     └──ErrConflict / refused identifier──► conflict ────┘
```

| Step | Who | What |
|---|---|---|
| `svc.Resolve(ctx, identity)` | the request middleware, once the token is verified | the member; inserts one in `provisioning` on first sight (running `Open` in the same transaction); `ErrErased` for a tombstone, `ErrConflict` for a parked member |
| `svc.RunWorker(ctx, poll)` | one goroutine per core | claims due members, calls `Provision` or `Erase` + `Purge`, retries with backoff, `OnStuck` past `MaxAttempts` |
| `svc.DeletionHandler()` | `consumer.Run` on the suite bus | moves the member to `erasing`; a tombstone for an identity never met |
| `svc.Reconcile(ctx)` | a daily job | identities with the role at urbangate vs `members` vs `Resources.List`; a `Report`, no correction |

Every transition is one `UPDATE … WHERE state = <expected>`, so two workers
cannot both move the same member. The `members` row is the work item: no
separate queue, and a crash between the insert and the provisioning leaves a
row the next worker pass picks up.

## Owned domain

When the product owns a mail domain, the local part of the login address is
also the product's resource. The web asks before `signUp`:

```
GET /api/v1/machine/identifiers/{local}   204 free · 400 invalid or reserved · 409 taken
```

`svc.IdentifierHandler()` serves it from `members` and the reserved list;
urbangate is never consulted. An erased member keeps its address, so an
address is never handed out twice. An identity that reaches `Resolve` with an
address the product refuses opens in `conflict`, visible to reconciliation.

`svc.MeHandler(identity)` serves `GET /me/membership` (`{state, localId,
address}`) and `DELETE /me` (202: the member goes to `erasing` at once, the
urbangate event that follows is idempotent on it).

## Stores

`pgx.New(pool)` for production, `pgx.Schema` to ship in the product's
migrations, `pgx.EnsureSchema` for a dev stack. `membership.NewMemory()` for
tests, including the product's own.
