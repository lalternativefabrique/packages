//go:build integration

package entitlement

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lalternative/packages/go/eda/pkg/outbox"
	outboxpgx "github.com/lalternative/packages/go/eda/pkg/outbox/pgx"

	sdk "github.com/lalternative/packages/lungor/sdk-go"
)

type lungorStub struct {
	mu       sync.Mutex
	consumed map[string]int64
	usages   []sdk.Usage
	failWith error
	keys     map[string]struct{}
}

func newLungorStub() *lungorStub {
	return &lungorStub{consumed: map[string]int64{}, keys: map[string]struct{}{}}
}

func (l *lungorStub) Entitlement(context.Context, string, ...string) (sdk.Entitlement, error) {
	return sdk.Entitlement{Entitled: true, Status: "active", PlanCode: "pro"}, nil
}
func (l *lungorStub) ListPlans(context.Context) (sdk.Plans, error) { return sdk.Plans{}, nil }
func (l *lungorStub) Balance(_ context.Context, subject, unit string) (sdk.Balance, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return sdk.Balance{Unit: unit, Limit: 5, Consumed: l.consumed[subject+unit], Periodic: true}, nil
}
func (l *lungorStub) Consume(_ context.Context, u sdk.Usage) (sdk.Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failWith != nil {
		return sdk.Decision{}, l.failWith
	}
	l.usages = append(l.usages, u)
	if _, seen := l.keys[u.IdempotencyKey]; !seen {
		l.keys[u.IdempotencyKey] = struct{}{}
		l.consumed[u.ExternalUserID+u.Unit] += u.Quantity
	}
	return sdk.Decision{Allowed: true, Recorded: true}, nil
}
func (l *lungorStub) Release(_ context.Context, u sdk.Usage) (sdk.Decision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.keys[u.IdempotencyKey] = struct{}{}
	l.consumed[u.ExternalUserID+u.Unit] -= u.Quantity
	return sdk.Decision{Allowed: true, Recorded: true}, nil
}
func (l *lungorStub) Recorded(context.Context, time.Time, string, int) (sdk.RecordedPage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	page := sdk.RecordedPage{}
	for k := range l.keys {
		page.Keys = append(page.Keys, sdk.RecordedKey{IdempotencyKey: k})
	}
	return page, nil
}
func (l *lungorStub) forget(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.keys, key)
}

func outboxEnv(t *testing.T, limit int64) (*pgxpool.Pool, *Gate, *lungorStub, string) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, OutboxMigration); err != nil {
		t.Fatal(err)
	}
	stub := newLungorStub()
	store := NewMemoryStore()
	subject := fmt.Sprintf("it-%d", time.Now().UnixNano())
	if err := store.Put(ctx, subject, Snapshot{Plan: "pro", Limits: map[string]int64{"email": limit}, RefreshedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return pool, New(Config{Ledger: stub, Store: store, Units: []string{"email"}, Refresh: time.Hour}), stub, subject
}

func inTx(t *testing.T, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	t.Helper()
	return pgx.BeginFunc(context.Background(), pool, fn)
}

func counter(t *testing.T, pool *pgxpool.Pool, subject string) int64 {
	t.Helper()
	var used int64
	err := pool.QueryRow(context.Background(),
		`SELECT used FROM lungor_usage_counters WHERE subject=$1 AND unit='email'`, subject).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return used
}

func queued(t *testing.T, pool *pgxpool.Pool, subject, status string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM lungor_outbox WHERE payload->>'subject'=$1 AND status=$2`, subject, status).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestConsumeTx_DecidesLocallyAndCommitsWithTheAction(t *testing.T) {
	pool, g, _, subject := outboxEnv(t, 3)
	ctx := context.Background()

	if err := inTx(t, pool, func(tx pgx.Tx) error { return g.ConsumeTx(ctx, tx, subject, "email", 2, subject+"-a") }); err != nil {
		t.Fatal(err)
	}
	err := inTx(t, pool, func(tx pgx.Tx) error { return g.ConsumeTx(ctx, tx, subject, "email", 2, subject+"-b") })
	var limit *LimitError
	if !errors.As(err, &limit) || limit.Used != 2 || limit.Limit != 3 {
		t.Fatalf("over the cap: %v", err)
	}
	actionFailed := errors.New("send failed")
	if err := inTx(t, pool, func(tx pgx.Tx) error {
		if err := g.ConsumeTx(ctx, tx, subject, "email", 1, subject+"-c"); err != nil {
			return err
		}
		return actionFailed
	}); !errors.Is(err, actionFailed) {
		t.Fatal(err)
	}
	if used, n := counter(t, pool, subject), queued(t, pool, subject, "pending"); used != 2 || n != 1 {
		t.Fatalf("after a rolled-back action: used=%d queued=%d", used, n)
	}
	if err := inTx(t, pool, func(tx pgx.Tx) error { return g.ConsumeTx(ctx, tx, subject, "email", 2, subject+"-a") }); err != nil {
		t.Fatal(err)
	}
	if used := counter(t, pool, subject); used != 2 {
		t.Fatalf("replayed key counted again: used=%d", used)
	}
	if err := inTx(t, pool, func(tx pgx.Tx) error { return g.RecordTx(ctx, tx, subject, "email", 5, subject+"-d") }); err != nil {
		t.Fatalf("record past the cap: %v", err)
	}
}

func TestConsumeTx_PostgresSerialisesConcurrentDebits(t *testing.T) {
	pool, g, _, subject := outboxEnv(t, 5)
	ctx := context.Background()
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		passed  int
		failure error
	)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := inTx(t, pool, func(tx pgx.Tx) error {
				return g.ConsumeTx(ctx, tx, subject, "email", 1, fmt.Sprintf("%s-%d", subject, i))
			})
			mu.Lock()
			defer mu.Unlock()
			var limit *LimitError
			switch {
			case err == nil:
				passed++
			case !errors.As(err, &limit):
				failure = err
			}
		}()
	}
	wg.Wait()
	if failure != nil {
		t.Fatal(failure)
	}
	if passed != 5 || counter(t, pool, subject) != 5 {
		t.Fatalf("passed=%d used=%d, want 5", passed, counter(t, pool, subject))
	}
}

func TestRelay_DeliversSettledDebitsAndRecalibrates(t *testing.T) {
	pool, g, stub, subject := outboxEnv(t, 5)
	ctx := context.Background()
	for _, key := range []string{"a", "b"} {
		if err := inTx(t, pool, func(tx pgx.Tx) error { return g.ConsumeTx(ctx, tx, subject, "email", 1, subject+"-"+key) }); err != nil {
			t.Fatal(err)
		}
	}
	stub.mu.Lock()
	stub.consumed[subject+"email"] = 3
	stub.mu.Unlock()

	relay := testRelay(g, pool)
	if _, err := relay.RelayOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if n := queued(t, pool, subject, "sent"); n != 2 {
		t.Fatalf("sent=%d", n)
	}
	stub.mu.Lock()
	for _, u := range stub.usages {
		if !u.Settled || u.OccurredAt.IsZero() {
			t.Fatalf("usage not settled with its date: %+v", u)
		}
	}
	stub.mu.Unlock()
	if used := counter(t, pool, subject); used != 5 {
		t.Fatalf("recalibrated used=%d, want Lungor's 5", used)
	}
}

func TestRelay_ParksARefusalAndRetriesAnOutage(t *testing.T) {
	pool, g, stub, subject := outboxEnv(t, 5)
	ctx := context.Background()
	if err := inTx(t, pool, func(tx pgx.Tx) error { return g.ConsumeTx(ctx, tx, subject, "email", 1, subject+"-a") }); err != nil {
		t.Fatal(err)
	}
	relay := testRelay(g, pool)

	stub.failWith = sdk.ErrUnavailable
	if _, err := relay.RelayOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if n := queued(t, pool, subject, "pending"); n != 1 {
		t.Fatalf("outage: pending=%d", n)
	}

	stub.failWith = sdk.ErrUnprocessable
	if _, err := pool.Exec(ctx, `UPDATE lungor_outbox SET next_attempt_at = NULL WHERE payload->>'subject'=$1`, subject); err != nil {
		t.Fatal(err)
	}
	if _, err := relay.RelayOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if n := queued(t, pool, subject, statusRejected); n != 1 {
		t.Fatalf("refusal: rejected=%d", n)
	}
}

func TestResync_QueuesAgainWhatLungorLost(t *testing.T) {
	pool, g, stub, subject := outboxEnv(t, 5)
	ctx := context.Background()
	if err := inTx(t, pool, func(tx pgx.Tx) error { return g.ConsumeTx(ctx, tx, subject, "email", 1, subject+"-a") }); err != nil {
		t.Fatal(err)
	}
	if _, err := testRelay(g, pool).RelayOnce(ctx); err != nil {
		t.Fatal(err)
	}
	stub.forget(subject + "-a")

	n, err := g.Resync(ctx, pool, time.Now().Add(-time.Hour))
	if err != nil || n < 1 {
		t.Fatalf("resync queued %d (%v)", n, err)
	}
	if p := queued(t, pool, subject, "pending"); p != 1 {
		t.Fatalf("pending after resync=%d", p)
	}
}

func TestConsumeTx_ACounterPastItsPeriodStartsAgain(t *testing.T) {
	pool, g, _, subject := outboxEnv(t, 3)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `
		INSERT INTO lungor_usage_counters (subject, unit, used, period_end) VALUES ($1, 'email', 3, NOW() - INTERVAL '1 minute')`, subject); err != nil {
		t.Fatal(err)
	}
	if err := inTx(t, pool, func(tx pgx.Tx) error { return g.ConsumeTx(ctx, tx, subject, "email", 1, subject+"-new-period") }); err != nil {
		t.Fatalf("first debit of the new period refused: %v", err)
	}
	if used := counter(t, pool, subject); used != 1 {
		t.Fatalf("used=%d, want 1", used)
	}
}

func testRelay(g *Gate, pool *pgxpool.Pool) *outbox.LeaseRelay {
	store := rejectingStore{LeaseStore: outboxpgx.NewLeaseStore(pool, outboxpgx.WithTable(OutboxTable)), pool: pool}
	return outbox.NewLeaseRelay(store, outbox.RawPublisherFunc(func(ctx context.Context, topic string, payload []byte) error {
		return g.deliver(ctx, pool, topic, payload)
	}), outbox.LeaseRelayConfig{BatchSize: 50})
}
