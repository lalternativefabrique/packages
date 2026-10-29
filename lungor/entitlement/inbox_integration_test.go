//go:build integration

package entitlement

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGInboxLifecycle(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, Migration); err != nil {
		t.Fatal(err)
	}
	inbox := PGInbox(pool)
	prefix := "it-" + time.Now().Format(time.RFC3339Nano) + "-"
	now := time.Now().UTC().Truncate(time.Microsecond)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM entitlement_inbox WHERE source_event_id LIKE $1`, prefix+"%")
	})

	ids := []string{prefix + "a", prefix + "b", prefix + "c"}
	for _, id := range ids {
		e := InboxEntry{SourceEventID: id, DeliveryID: "d-" + id, Type: "subscription.activated", Subject: "u1", Payload: []byte(`{"external_user_id":"u1"}`), ReceivedAt: now}
		if ok, err := inbox.Insert(ctx, e); err != nil || !ok {
			t.Fatalf("insert %s: ok=%v err=%v", id, ok, err)
		}
		if ok, err := inbox.Insert(ctx, e); err != nil || ok {
			t.Fatalf("duplicate %s: ok=%v err=%v", id, ok, err)
		}
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed = map[string]int{}
	)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := inbox.Claim(ctx, now, time.Minute, 2)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, e := range got {
				claimed[e.SourceEventID]++
			}
		}()
	}
	wg.Wait()
	for _, id := range ids {
		if claimed[id] != 1 {
			t.Fatalf("claims = %v", claimed)
		}
	}
	if again, err := inbox.Claim(ctx, now, time.Minute, 10); err != nil || len(again) != 0 {
		t.Fatalf("leased rows claimed again: %v %v", again, err)
	}

	if err := inbox.Done(ctx, ids[0], now); err != nil {
		t.Fatal(err)
	}
	if err := inbox.Retry(ctx, ids[1], 1, "boom", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := inbox.Fail(ctx, ids[2], 3, "gave up", now); err != nil {
		t.Fatal(err)
	}
	got, err := inbox.Claim(ctx, now.Add(2*time.Second), time.Minute, 10)
	if err != nil || len(got) != 1 || got[0].SourceEventID != ids[1] || got[0].Attempts != 1 || string(got[0].Payload) != `{"external_user_id": "u1"}` {
		t.Fatalf("after retry: %+v %v", got, err)
	}
	assertRow(t, pool, ids[0], InboxDone, 0)
	assertRow(t, pool, ids[2], InboxFailed, 3)

	if n, err := inbox.Replay(ctx, now.Add(time.Hour), now); err != nil || n != 0 {
		t.Fatalf("replay before since: %d %v", n, err)
	}
	if n, err := inbox.Replay(ctx, now, now); err != nil || n != 1 {
		t.Fatalf("replay: %d %v", n, err)
	}
	assertRow(t, pool, ids[2], InboxPending, 0)
	if got, err := inbox.Claim(ctx, now, time.Minute, 10); err != nil || len(got) != 1 || got[0].SourceEventID != ids[2] {
		t.Fatalf("replayed claim: %+v %v", got, err)
	}
}

func assertRow(t *testing.T, pool *pgxpool.Pool, id, status string, attempts int) {
	t.Helper()
	var (
		gotStatus   string
		gotAttempts int
	)
	if err := pool.QueryRow(context.Background(),
		`SELECT status, attempts FROM entitlement_inbox WHERE source_event_id = $1`, id).Scan(&gotStatus, &gotAttempts); err != nil {
		t.Fatal(err)
	}
	if gotStatus != status || gotAttempts != attempts {
		t.Fatalf("%s: status=%s attempts=%d, want %s/%d", id, gotStatus, gotAttempts, status, attempts)
	}
}
