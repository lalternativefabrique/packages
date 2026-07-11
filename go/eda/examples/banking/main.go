// Runnable example: a tiny "Account" aggregate that exercises every
// modern layer of the boilerplate.
//
// Run:
//
//	go run ./examples/banking
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/google/uuid"

	"github.com/lalternative/packages/go/eda/pkg/cqrs"
	"github.com/lalternative/packages/go/eda/pkg/db"
	"github.com/lalternative/packages/go/eda/pkg/ddd"
	"github.com/lalternative/packages/go/eda/pkg/di"
	"github.com/lalternative/packages/go/eda/pkg/logger"
	"github.com/lalternative/packages/go/eda/pkg/obs"
)

// ----------------------------------------------------------------------------
// Domain: Account aggregate
// ----------------------------------------------------------------------------

type accountID = string

type AccountOpened struct {
	Owner string `json:"owner"`
}

func (AccountOpened) EventKind() string { return "account.opened" }

type MoneyDeposited struct {
	Amount int64 `json:"amount"`
}

func (MoneyDeposited) EventKind() string { return "account.deposited" }

type MoneyWithdrawn struct {
	Amount int64 `json:"amount"`
}

func (MoneyWithdrawn) EventKind() string { return "account.withdrawn" }

type Account struct {
	ddd.BaseAggregateRoot[accountID]
	owner   string
	balance int64
	open    bool
}

func NewAccount(id accountID) *Account {
	a := &Account{}
	a.Init(id, "Account", ddd.SystemClock{})
	return a
}

func (a *Account) Apply(env ddd.EventEnvelope[accountID]) error {
	switch p := env.Payload.(type) {
	case AccountOpened:
		a.owner = p.Owner
		a.open = true
	case MoneyDeposited:
		a.balance += p.Amount
	case MoneyWithdrawn:
		a.balance -= p.Amount
	default:
		return fmt.Errorf("%w: %T", ddd.ErrUnknownEvent, env.Payload)
	}
	return nil
}

func (a *Account) Open(owner string) error {
	if a.open {
		return fmt.Errorf("account already open")
	}
	return ddd.Raise[accountID, *Account](a, &a.BaseAggregateRoot, AccountOpened{Owner: owner}, a.Apply)
}

func (a *Account) Deposit(amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("%w: deposit must be positive", cqrs.ErrValidation)
	}
	return ddd.Raise[accountID, *Account](a, &a.BaseAggregateRoot, MoneyDeposited{Amount: amount}, a.Apply)
}

func (a *Account) Withdraw(amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("%w: withdrawal must be positive", cqrs.ErrValidation)
	}
	if a.balance < amount {
		return fmt.Errorf("%w: insufficient funds (balance=%d, asked=%d)", cqrs.ErrValidation, a.balance, amount)
	}
	return ddd.Raise[accountID, *Account](a, &a.BaseAggregateRoot, MoneyWithdrawn{Amount: amount}, a.Apply)
}

// ----------------------------------------------------------------------------
// Repository (event-sourced) on top of the in-memory store
// ----------------------------------------------------------------------------

type AccountRepo struct {
	store *db.InMemoryStore[accountID]
}

func NewAccountRepo(store *db.InMemoryStore[accountID]) *AccountRepo {
	return &AccountRepo{store: store}
}

func (r *AccountRepo) Load(ctx context.Context, id accountID) (*Account, error) {
	history, err := r.store.Load(ctx, id)
	if err != nil {
		return nil, err
	}
	a := NewAccount(id)
	if err := ddd.LoadFromHistory[accountID, *Account](a, &a.BaseAggregateRoot, history); err != nil {
		return nil, err
	}
	return a, nil
}

func (r *AccountRepo) Save(ctx context.Context, a *Account) error {
	pending := a.Uncommitted()
	if len(pending) == 0 {
		return nil
	}
	expected := a.Version() - len(pending)
	if err := r.store.Save(ctx, a.ID(), expected, pending); err != nil {
		return err
	}
	a.MarkCommitted()
	return nil
}

// ----------------------------------------------------------------------------
// Commands & queries
// ----------------------------------------------------------------------------

type OpenAccountCmd struct {
	ID    accountID
	Owner string
}
type OpenAccountRes struct{ ID accountID }

type DepositCmd struct {
	ID     accountID
	Amount int64
}
type DepositRes struct{ NewBalance int64 }

type GetBalanceQry struct{ ID accountID }
type GetBalanceRes struct{ Balance int64 }

// Handlers.

type openAccountHandler struct {
	repo *AccountRepo
	log  logger.Logger
}

func (h *openAccountHandler) Handle(ctx context.Context, cmd OpenAccountCmd) (OpenAccountRes, error) {
	id := cmd.ID
	if id == "" {
		id = uuid.NewString()
	}
	a := NewAccount(id)
	if err := a.Open(cmd.Owner); err != nil {
		return OpenAccountRes{}, err
	}
	if err := h.repo.Save(ctx, a); err != nil {
		return OpenAccountRes{}, err
	}
	h.log.Info("account opened", logger.String("id", id), logger.String("owner", cmd.Owner))
	return OpenAccountRes{ID: id}, nil
}

type depositHandler struct {
	repo *AccountRepo
}

func (h *depositHandler) Handle(ctx context.Context, cmd DepositCmd) (DepositRes, error) {
	a, err := h.repo.Load(ctx, cmd.ID)
	if err != nil {
		return DepositRes{}, fmt.Errorf("%w: %s", cqrs.ErrNotFound, cmd.ID)
	}
	if err := a.Deposit(cmd.Amount); err != nil {
		return DepositRes{}, err
	}
	if err := h.repo.Save(ctx, a); err != nil {
		return DepositRes{}, err
	}
	return DepositRes{NewBalance: a.balance}, nil
}

// Read-model projection: a tenant-agnostic balance index updated from the
// event stream.

type BalanceProjection struct {
	mu       sync.RWMutex
	balances map[accountID]int64
}

func NewBalanceProjection() *BalanceProjection {
	return &BalanceProjection{balances: make(map[accountID]int64)}
}

func (p *BalanceProjection) Apply(_ context.Context, env ddd.EventEnvelope[accountID]) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch e := env.Payload.(type) {
	case AccountOpened:
		if _, ok := p.balances[env.AggregateID]; !ok {
			p.balances[env.AggregateID] = 0
		}
	case MoneyDeposited:
		p.balances[env.AggregateID] += e.Amount
	case MoneyWithdrawn:
		p.balances[env.AggregateID] -= e.Amount
	}
	return nil
}

func (p *BalanceProjection) Balance(id accountID) int64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.balances[id]
}

type getBalanceHandler struct {
	proj *BalanceProjection
}

func (h *getBalanceHandler) Handle(_ context.Context, q GetBalanceQry) (GetBalanceRes, error) {
	return GetBalanceRes{Balance: h.proj.Balance(q.ID)}, nil
}

// ----------------------------------------------------------------------------
// Wiring with the typed DI container
// ----------------------------------------------------------------------------

func buildRegistry() *di.Registry {
	r := di.New()

	di.Provide[logger.Logger](r, func(_ *di.Resolver) (logger.Logger, error) {
		return logger.NewJSONSlogLogger(slog.LevelInfo), nil
	})

	di.Provide(r, func(_ *di.Resolver) (*db.InMemoryStore[accountID], error) {
		return db.NewInMemoryStore[accountID](), nil
	})

	di.Provide(r, func(rv *di.Resolver) (*AccountRepo, error) {
		store, err := di.From[*db.InMemoryStore[accountID]](rv)
		if err != nil {
			return nil, err
		}
		return NewAccountRepo(store), nil
	})

	di.Provide(r, func(_ *di.Resolver) (*BalanceProjection, error) {
		return NewBalanceProjection(), nil
	})

	di.Provide(r, func(rv *di.Resolver) (*cqrs.CommandRouter, error) {
		repo := di.MustFrom[*AccountRepo](rv)
		log := di.MustFrom[logger.Logger](rv)
		router := cqrs.NewCommandRouter()

		// Wire OpenAccount with logging + recovery middleware.
		openMW := cqrs.Chain(
			cqrs.RecoveryMiddleware[OpenAccountCmd, OpenAccountRes](),
			obs.LoggingMiddleware[OpenAccountCmd, OpenAccountRes](log),
		)
		openHandler := cqrs.TypedCommandHandlerFunc[OpenAccountCmd, OpenAccountRes](
			openMW((&openAccountHandler{repo: repo, log: log}).Handle),
		)
		cqrs.RegisterCommandHandler[OpenAccountCmd, OpenAccountRes](router, openHandler)

		depositMW := cqrs.Chain(
			cqrs.RecoveryMiddleware[DepositCmd, DepositRes](),
			obs.LoggingMiddleware[DepositCmd, DepositRes](log),
		)
		depositHandlerFn := cqrs.TypedCommandHandlerFunc[DepositCmd, DepositRes](
			depositMW((&depositHandler{repo: repo}).Handle),
		)
		cqrs.RegisterCommandHandler[DepositCmd, DepositRes](router, depositHandlerFn)

		return router, nil
	})

	di.Provide(r, func(rv *di.Resolver) (*cqrs.QueryRouter, error) {
		proj := di.MustFrom[*BalanceProjection](rv)
		router := cqrs.NewQueryRouter()
		cqrs.RegisterQueryHandler[GetBalanceQry, GetBalanceRes](router,
			&getBalanceHandler{proj: proj},
		)
		return router, nil
	})

	return r
}

// ----------------------------------------------------------------------------
// main
// ----------------------------------------------------------------------------

func main() {
	ctx := context.Background()
	reg := buildRegistry()
	if err := reg.Start(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}
	defer reg.Stop(ctx)

	store := di.MustResolve[*db.InMemoryStore[accountID]](reg)
	proj := di.MustResolve[*BalanceProjection](reg)
	cmds := di.MustResolve[*cqrs.CommandRouter](reg)
	queries := di.MustResolve[*cqrs.QueryRouter](reg)

	// Wire the projection to the store's event stream.
	if err := store.Subscribe(ctx, proj.Apply); err != nil {
		fmt.Fprintln(os.Stderr, "subscribe:", err)
		os.Exit(1)
	}

	// 1) Open an account.
	opened, err := cqrs.Execute[OpenAccountCmd, OpenAccountRes](ctx, cmds, OpenAccountCmd{Owner: "Alice"})
	must(err)

	// 2) Deposit twice.
	_, err = cqrs.Execute[DepositCmd, DepositRes](ctx, cmds, DepositCmd{ID: opened.ID, Amount: 100})
	must(err)
	_, err = cqrs.Execute[DepositCmd, DepositRes](ctx, cmds, DepositCmd{ID: opened.ID, Amount: 250})
	must(err)

	// 3) Query the read-side projection.
	bal, err := cqrs.Ask[GetBalanceQry, GetBalanceRes](ctx, queries, GetBalanceQry{ID: opened.ID})
	must(err)

	fmt.Printf("account=%s balance=%d (expected 350)\n", opened.ID, bal.Balance)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
