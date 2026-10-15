//go:build integration

package pgstore_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lalternative/packages/go/connect"
	"github.com/lalternative/packages/go/connect/pgstore"
)

func store(t *testing.T) (*pgstore.Store, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is unset")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pgstore.EnsureSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM connect_grant; DELETE FROM connect_grant_pending`); err != nil {
		t.Fatal(err)
	}
	sealer, err := connect.NewAESGCM("MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err != nil {
		t.Fatal(err)
	}
	return pgstore.New(pool, sealer), pool
}

func TestAPendingStateIsTakenOnce(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	p, _ := connect.NewPending("ada", "partage", time.Now())
	if err := s.AddPending(ctx, p); err != nil {
		t.Fatal(err)
	}
	got, err := s.TakePending(ctx, p.State)
	if err != nil || got.Verifier != p.Verifier || got.Owner != "ada" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.TakePending(ctx, p.State); !errors.Is(err, connect.ErrUnknownState) {
		t.Fatalf("taken twice: %v", err)
	}
}

func TestAGrantIsSealedForItsOwner(t *testing.T) {
	s, pool := store(t)
	ctx := context.Background()
	c := connect.Connection{Owner: "ada", Product: "partage", Status: connect.StatusActive, RefreshToken: "r1", Scopes: []string{"partage:read"}, ConnectedAt: time.Now()}
	if err := s.Save(ctx, c); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT refresh_token FROM connect_grant WHERE owner = 'ada'`).Scan(&raw); err != nil || string(raw) == "r1" {
		t.Fatalf("stored in clear: %v", err)
	}
	got, err := s.Connection(ctx, "ada", "partage")
	if err != nil || got.RefreshToken != "r1" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE connect_grant SET owner = 'bob'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Connection(ctx, "bob", "partage"); err == nil {
		t.Fatal("a token moved to another owner opened")
	}
}

func TestRotationsAreSerialized(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	if err := s.Save(ctx, connect.Connection{Owner: "ada", Product: "partage", Status: connect.StatusActive, RefreshToken: "r0", ConnectedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Rotate(ctx, "ada", "partage", func(c connect.Connection) (connect.Connection, error) {
				mu.Lock()
				seen[c.RefreshToken]++
				c.RefreshToken = c.RefreshToken + "+"
				mu.Unlock()
				return c, nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for tok, n := range seen {
		if n != 1 {
			t.Fatalf("%s rotated %d times", tok, n)
		}
	}
	got, _ := s.Connection(ctx, "ada", "partage")
	if got.RefreshToken != "r0+++++" {
		t.Fatalf("refresh = %s", got.RefreshToken)
	}
}

func TestPurgeErasesTheOwnersGrants(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	for _, owner := range []string{"ada", "bob"} {
		if err := s.Save(ctx, connect.Connection{Owner: owner, Product: "partage", Status: connect.StatusActive, RefreshToken: "r", ConnectedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Purge(ctx, "ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Connection(ctx, "ada", "partage"); !errors.Is(err, connect.ErrNotConnected) {
		t.Fatalf("ada: %v", err)
	}
	if _, err := s.Connection(ctx, "bob", "partage"); err != nil {
		t.Fatalf("bob: %v", err)
	}
}
