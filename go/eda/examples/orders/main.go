// End-to-end runnable example tying every layer of the boilerplate
// together with no NATS dependency:
//
//   - aggregate Order (event-sourced) saved through pkg/db.InMemoryStore
//   - outbox.Relay drains pending records and publishes to the typed bus
//   - projection.Manager keeps an inventory read model up to date
//   - processmanager.Engine reacts to events to emit a shipping decision
//
// Run:
//
//	go run ./examples/orders
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/lalternative/packages/go/eda/pkg/cqrs"
	"github.com/lalternative/packages/go/eda/pkg/db"
	"github.com/lalternative/packages/go/eda/pkg/ddd"
	"github.com/lalternative/packages/go/eda/pkg/di"
	"github.com/lalternative/packages/go/eda/pkg/logger"
	"github.com/lalternative/packages/go/eda/pkg/outbox"
	"github.com/lalternative/packages/go/eda/pkg/processmanager"
	"github.com/lalternative/packages/go/eda/pkg/projection"
)

// ----------------------------------------------------------------------------
// Domain: Order aggregate
// ----------------------------------------------------------------------------

type orderID = string

type OrderPlaced struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

func (OrderPlaced) EventKind() string { return "order.placed" }

type OrderPaid struct{}

func (OrderPaid) EventKind() string { return "order.paid" }

type Order struct {
	ddd.BaseAggregateRoot[orderID]
	sku      string
	quantity int
	placed   bool
	paid     bool
}

func NewOrder(id orderID) *Order {
	o := &Order{}
	o.Init(id, "Order", ddd.SystemClock{})
	return o
}

func (o *Order) Apply(env ddd.EventEnvelope[orderID]) error {
	switch p := env.Payload.(type) {
	case OrderPlaced:
		o.sku, o.quantity, o.placed = p.SKU, p.Quantity, true
	case OrderPaid:
		o.paid = true
	default:
		return fmt.Errorf("%w: %T", ddd.ErrUnknownEvent, env.Payload)
	}
	return nil
}

func (o *Order) Place(sku string, qty int) error {
	if o.placed {
		return fmt.Errorf("order already placed")
	}
	return ddd.Raise[orderID, *Order](o, &o.BaseAggregateRoot,
		OrderPlaced{SKU: sku, Quantity: qty}, o.Apply,
		ddd.WithCorrelation[orderID](o.ID(), ""),
	)
}

func (o *Order) MarkPaid() error {
	if !o.placed || o.paid {
		return fmt.Errorf("invalid state for payment")
	}
	return ddd.Raise[orderID, *Order](o, &o.BaseAggregateRoot,
		OrderPaid{}, o.Apply,
		ddd.WithCorrelation[orderID](o.ID(), ""),
	)
}

// ----------------------------------------------------------------------------
// Repository (event store + outbox in one Save)
// ----------------------------------------------------------------------------

type OrderRepo struct {
	store *db.InMemoryStore[orderID]
	ob    *outbox.InMemoryStore[orderID]
}

func NewOrderRepo(store *db.InMemoryStore[orderID], ob *outbox.InMemoryStore[orderID]) *OrderRepo {
	return &OrderRepo{store: store, ob: ob}
}

// Save commits the aggregate events AND enqueues outbox records "in the
// same transaction" — here the in-memory stores stand in for a real DB
// transaction. With a SQL backend, both writes share the same *sql.Tx.
func (r *OrderRepo) Save(ctx context.Context, o *Order) error {
	pending := o.Uncommitted()
	if len(pending) == 0 {
		return nil
	}
	expected := o.Version() - len(pending)
	if err := r.store.Save(ctx, o.ID(), expected, pending); err != nil {
		return err
	}
	for _, env := range pending {
		if _, err := r.ob.Enqueue(ctx, env); err != nil {
			return err
		}
	}
	o.MarkCommitted()
	return nil
}

func (r *OrderRepo) Load(ctx context.Context, id orderID) (*Order, error) {
	history, err := r.store.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	o := NewOrder(id)
	if err := ddd.LoadFromHistory[orderID, *Order](o, &o.BaseAggregateRoot, history); err != nil {
		return nil, err
	}
	return o, nil
}

// ----------------------------------------------------------------------------
// Read model: inventory projection
// ----------------------------------------------------------------------------

type InventoryProjection struct {
	bySKU map[string]int
}

func NewInventoryProjection() *InventoryProjection {
	return &InventoryProjection{bySKU: make(map[string]int)}
}

func (p *InventoryProjection) Name() string { return "inventory" }

func (p *InventoryProjection) Apply(_ context.Context, env ddd.EventEnvelope[orderID]) error {
	if e, ok := env.Payload.(OrderPlaced); ok {
		p.bySKU[e.SKU] += e.Quantity
	}
	return nil
}

// inventorySource adapts InMemoryStore to projection.EventSource.
type inventorySource struct {
	store *db.InMemoryStore[orderID]
	known map[orderID]struct{}
}

func newInventorySource(store *db.InMemoryStore[orderID]) *inventorySource {
	return &inventorySource{store: store, known: make(map[orderID]struct{})}
}

func (s *inventorySource) AllAggregateIDs(_ context.Context) ([]orderID, error) {
	out := make([]orderID, 0, len(s.known))
	for id := range s.known {
		out = append(out, id)
	}
	return out, nil
}

func (s *inventorySource) LoadFromVersion(ctx context.Context, id orderID, from int) ([]ddd.EventEnvelope[orderID], error) {
	return s.store.LoadFromVersion(ctx, id, from)
}

func (s *inventorySource) Subscribe(ctx context.Context, h func(context.Context, ddd.EventEnvelope[orderID]) error) error {
	if err := s.store.Subscribe(ctx, func(ctx context.Context, env ddd.EventEnvelope[orderID]) error {
		s.known[env.AggregateID] = struct{}{}
		return h(ctx, env)
	}); err != nil {
		return err
	}
	// Block until ctx is canceled so the Manager's goroutine stays alive.
	<-ctx.Done()
	return ctx.Err()
}

// ----------------------------------------------------------------------------
// Process manager: shipping
// ----------------------------------------------------------------------------

type shipState struct {
	OrderID string
	Placed  bool
	Paid    bool
}

func shippingDef(shipped *int32) processmanager.Definition[orderID, shipState] {
	return processmanager.Definition[orderID, shipState]{
		Name: "shipping",
		InstanceID: func(env ddd.EventEnvelope[orderID]) (string, error) {
			return env.AggregateID, nil
		},
		Initial: func() shipState { return shipState{} },
		Handlers: map[string]processmanager.Handler[orderID, shipState]{
			"order.placed": func(_ context.Context, in processmanager.HandlerInput[orderID, shipState]) (processmanager.HandlerOutput[shipState], error) {
				st := in.State
				st.OrderID = in.Envelope.AggregateID
				st.Placed = true
				return processmanager.HandlerOutput[shipState]{State: st}, nil
			},
			"order.paid": func(_ context.Context, in processmanager.HandlerInput[orderID, shipState]) (processmanager.HandlerOutput[shipState], error) {
				st := in.State
				st.Paid = true
				effects := []processmanager.Effect{
					processmanager.EffectFunc(func(_ context.Context) error {
						atomic.AddInt32(shipped, 1)
						return nil
					}),
				}
				return processmanager.HandlerOutput[shipState]{State: st, Effects: effects, Done: true}, nil
			},
		},
	}
}

// ----------------------------------------------------------------------------
// Wiring
// ----------------------------------------------------------------------------

type Wiring struct {
	Store   *db.InMemoryStore[orderID]
	Outbox  *outbox.InMemoryStore[orderID]
	Repo    *OrderRepo
	Bus     *cqrs.TypedEventBus[orderID]
	Inv     *InventoryProjection
	InvMgr  *projection.Manager[orderID]
	Engine  *processmanager.Engine[orderID, shipState]
	Shipped *int32
	Log     logger.Logger
}

func build() *Wiring {
	r := di.New()

	di.Provide[logger.Logger](r, func(_ *di.Resolver) (logger.Logger, error) {
		return logger.NewJSONSlogLogger(slog.LevelInfo), nil
	})
	di.Provide(r, func(_ *di.Resolver) (*db.InMemoryStore[orderID], error) {
		return db.NewInMemoryStore[orderID](), nil
	})
	di.Provide(r, func(_ *di.Resolver) (*outbox.InMemoryStore[orderID], error) {
		return outbox.NewInMemoryStore[orderID](nil), nil
	})
	di.Provide(r, func(rv *di.Resolver) (*OrderRepo, error) {
		return NewOrderRepo(
			di.MustFrom[*db.InMemoryStore[orderID]](rv),
			di.MustFrom[*outbox.InMemoryStore[orderID]](rv),
		), nil
	})
	di.Provide(r, func(_ *di.Resolver) (*cqrs.TypedEventBus[orderID], error) {
		return cqrs.NewTypedEventBus[orderID](cqrs.TypedEventBusConfig{Workers: 2, QueueSize: 64}), nil
	})

	wiring := &Wiring{Shipped: new(int32)}
	wiring.Log = di.MustResolve[logger.Logger](r)
	wiring.Store = di.MustResolve[*db.InMemoryStore[orderID]](r)
	wiring.Outbox = di.MustResolve[*outbox.InMemoryStore[orderID]](r)
	wiring.Repo = di.MustResolve[*OrderRepo](r)
	wiring.Bus = di.MustResolve[*cqrs.TypedEventBus[orderID]](r)

	wiring.Inv = NewInventoryProjection()
	src := newInventorySource(wiring.Store)
	wiring.InvMgr = projection.NewManager[orderID](src, projection.NewInMemoryCheckpointStore[orderID](), wiring.Inv, projection.ManagerConfig{})

	pmStore := processmanager.NewInMemoryStateStore[shipState]()
	eng, err := processmanager.New[orderID, shipState](shippingDef(wiring.Shipped), pmStore)
	if err != nil {
		panic(err)
	}
	wiring.Engine = eng

	return wiring
}

// ----------------------------------------------------------------------------
// main
// ----------------------------------------------------------------------------

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := build()
	defer w.Bus.Close()

	// 1) Subscribe the inventory projection to live events. RunLive
	// registers the handler synchronously and then blocks until ctx ends.
	liveDone := make(chan struct{})
	go func() {
		defer close(liveDone)
		if err := w.InvMgr.RunLive(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "projection live:", err)
		}
	}()
	// Give the goroutine a moment to register the Subscribe handler.
	time.Sleep(20 * time.Millisecond)

	// 2) Wire the typed event bus -> process manager.
	for _, kind := range []string{"order.placed", "order.paid"} {
		w.Bus.Subscribe(kind, cqrs.TypedEventHandlerFunc[orderID](func(ctx context.Context, env ddd.EventEnvelope[orderID]) error {
			return w.Engine.Handle(ctx, env)
		}))
	}

	// 3) Outbox relay -> publishes to the typed event bus.
	relay := outbox.NewRelay[orderID](w.Outbox, outbox.PublisherFunc[orderID](func(ctx context.Context, env ddd.EventEnvelope[orderID]) error {
		return w.Bus.Publish(ctx, env)
	}), outbox.RelayConfig{BatchSize: 50, PollInterval: 20 * time.Millisecond})
	go func() {
		if err := relay.Run(ctx); err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "relay:", err)
		}
	}()

	// 4) Drive a couple of orders.
	for _, x := range []struct {
		sku string
		qty int
	}{{"SKU-A", 2}, {"SKU-B", 5}, {"SKU-A", 1}} {
		id := uuid.NewString()
		o := NewOrder(id)
		must(o.Place(x.sku, x.qty))
		must(w.Repo.Save(ctx, o))

		// Mark paid right after.
		o2, err := w.Repo.Load(ctx, id)
		must(err)
		must(o2.MarkPaid())
		must(w.Repo.Save(ctx, o2))
	}

	// 5) Wait until the relay drained and projection caught up.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(w.Shipped) == 3 && w.Inv.bySKU["SKU-A"] == 3 && w.Inv.bySKU["SKU-B"] == 5 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	fmt.Printf("shipped=%d (expected 3)\n", atomic.LoadInt32(w.Shipped))
	fmt.Printf("inventory: SKU-A=%d (expected 3), SKU-B=%d (expected 5)\n",
		w.Inv.bySKU["SKU-A"], w.Inv.bySKU["SKU-B"])

	// 6) Show outbox final state.
	dispatched := 0
	for _, r := range w.Outbox.Snapshot() {
		if r.Status == outbox.StatusDispatched {
			dispatched++
		}
	}
	fmt.Printf("outbox dispatched=%d (expected 6)\n", dispatched)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
