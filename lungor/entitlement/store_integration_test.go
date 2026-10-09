//go:build integration

package entitlement

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPGStoreRoundTrip(t *testing.T) {
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
	store := PG(pool)
	subject := "it-" + time.Now().Format(time.RFC3339Nano)

	if _, ok, err := store.Get(ctx, subject); err != nil || ok {
		t.Fatalf("missing row: ok=%v err=%v", ok, err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	for _, plan := range []string{"free", "pro"} {
		if err := store.Put(ctx, subject, Snapshot{Plan: plan, Limits: map[string]int64{"domain": 5}, RefreshedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	s, ok, err := store.Get(ctx, subject)
	if err != nil || !ok || s.Plan != "pro" || s.Limits["domain"] != 5 || !s.RefreshedAt.Equal(at) {
		t.Fatalf("got %+v ok=%v err=%v", s, ok, err)
	}
}
