# busevents

The contract of the suite's shared bus: which subjects each product publishes
under `events.<product>.>`, and the payload of each. Producers and consumers
import the same types.

```
go get github.com/lalternative/packages/go/busevents
```

| Subject | Publisher | Type |
|---|---|---|
| `events.<product>.account.signed_up` | every product, once the address is proven | `SignedUp` |
| `events.<product>.account.activated` | every product, first value reached, once per person | `Activated` |
| `events.lungor.subscription.activated` | Lungor, first payment or return after churn | `Subscription` |
| `events.lungor.subscription.renewed` | Lungor, recurring payment succeeded | `Subscription` |
| `events.lungor.subscription.past_due` | Lungor, recurring payment failed | `Subscription` |
| `events.lungor.subscription.canceled` | Lungor, access stops at `effective_at` | `Subscription` |
| `events.skalpai.analytics.daily` | Skalpai, unique visitors per product per UTC day | `DailyVisitors` |
| `events.urbangate.account.deletion_requested` | urbangate (its ADR 0006) | `DeletionRequested` |

Every payload is flat JSON with `event_id` and `occurred_at` at the top level,
and no e-mail or name: every consumer of the bus can read it.

## Publishing

```go
body, msgID, err := busevents.Encode(busevents.Activated{
    Meta:     busevents.Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()},
    PersonID: user.ID,
    Trigger:  "synthesis_completed",
})
_, err = js.Publish(ctx, busevents.Subject("synthiz", busevents.AccountActivated), body, jetstream.WithMsgID(msgID))
```

Publish from the outbox, in the transaction that made the fact true, so the
event and the state cannot disagree. `EventID` must be the fact's id, not the
publish's: a republish sends the same one.

## Consuming

```go
e, err := busevents.Decode[busevents.Subscription](msg.Data)
if err != nil {
    return consumer.Permanent(err)
}
product := e.AppSlug
```

A product's own subjects name it in their prefix, which its NATS account
guarantees; read the product from the subject (`busevents.ProductOf`), not
from the payload. Lungor bills every product, so its payload carries
`app_slug`.
