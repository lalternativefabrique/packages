//go:build integration

package pgx_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/lalternative/packages/go/membership"
	mpgx "github.com/lalternative/packages/go/membership/pgx"
)

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is unset")
	}
	p, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	ctx := context.Background()
	if err := mpgx.EnsureSchema(ctx, p); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `DELETE FROM members`); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStoreLifecycle(t *testing.T) {
	ctx := context.Background()
	store := mpgx.New(pool(t))

	if _, err := store.Get(ctx, "id-1"); !errors.Is(err, membership.ErrNotMember) {
		t.Fatalf("empty store: %v", err)
	}
	m, err := store.Insert(ctx, membership.Member{IdentityID: "id-1", LocalID: "id-1", Address: "ana@messag.test", State: membership.StateProvisioning}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m.State != membership.StateProvisioning || m.Address != "ana@messag.test" || string(m.Resources) != "{}" {
		t.Fatalf("inserted %+v", m)
	}
	again, err := store.Insert(ctx, membership.Member{IdentityID: "id-1", LocalID: "other", State: membership.StateErased}, nil)
	if err != nil || again.LocalID != "id-1" || again.State != membership.StateProvisioning {
		t.Fatalf("second insert must return the existing member: %+v %v", again, err)
	}
	if taken, _ := store.AddressTaken(ctx, "ana@messag.test"); !taken {
		t.Fatal("address not taken")
	}
	if taken, _ := store.AddressTaken(ctx, "free@messag.test"); taken {
		t.Fatal("free address taken")
	}

	now := time.Now().UTC()
	due, err := store.ClaimDue(ctx, now, time.Minute, 10)
	if err != nil || len(due) != 1 || due[0].IdentityID != "id-1" {
		t.Fatalf("claim: %+v %v", due, err)
	}
	if due, _ := store.ClaimDue(ctx, now, time.Minute, 10); len(due) != 0 {
		t.Fatal("a leased member was claimed twice")
	}
	if err := store.Fail(ctx, "id-1", "stalwart down", now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	m, _ = store.Get(ctx, "id-1")
	if m.Attempts != 1 || m.LastError != "stalwart down" {
		t.Fatalf("after fail %+v", m)
	}
	if due, _ := store.ClaimDue(ctx, now, time.Minute, 10); len(due) != 1 {
		t.Fatal("a failed member due again was not claimed")
	}

	ok, err := store.Transition(ctx, "id-1", membership.StateReady, membership.StateErasing, membership.Patch{}, nil)
	if err != nil || ok {
		t.Fatalf("transition from a wrong state: ok=%v err=%v", ok, err)
	}
	ok, err = store.Transition(ctx, "id-1", membership.StateProvisioning, membership.StateReady, membership.Patch{Resources: json.RawMessage(`{"principal":"p1"}`)}, nil)
	if err != nil || !ok {
		t.Fatalf("to ready: ok=%v err=%v", ok, err)
	}
	m, _ = store.Get(ctx, "id-1")
	if m.State != membership.StateReady || m.ReadyAt == nil || m.Attempts != 0 || m.LastError != "" || string(m.Resources) != `{"principal": "p1"}` {
		t.Fatalf("ready %+v", m)
	}
	if due, _ := store.ClaimDue(ctx, now.Add(time.Hour), time.Minute, 10); len(due) != 0 {
		t.Fatal("a ready member was claimed")
	}
	ok, _ = store.Transition(ctx, "id-1", membership.StateReady, membership.StateErasing, membership.Patch{NextAttemptAt: now}, nil)
	if !ok {
		t.Fatal("to erasing")
	}
	ok, _ = store.Transition(ctx, "id-1", membership.StateErasing, membership.StateErased, membership.Patch{Resources: json.RawMessage(`{}`)}, nil)
	if !ok {
		t.Fatal("to erased")
	}
	m, _ = store.Get(ctx, "id-1")
	if m.State != membership.StateErased || m.ErasedAt == nil || m.Address != "ana@messag.test" {
		t.Fatalf("erased %+v", m)
	}
	list, err := store.List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %v %v", list, err)
	}
	if err := store.Fail(ctx, "missing", "x", now); !errors.Is(err, membership.ErrNotMember) {
		t.Fatalf("fail on a missing member: %v", err)
	}
}

func TestHooksRunInTheMemberTransaction(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	if _, err := p.Exec(ctx, `CREATE TABLE IF NOT EXISTS product_users (id TEXT PRIMARY KEY); DELETE FROM product_users`); err != nil {
		t.Fatal(err)
	}
	store := mpgx.New(p)
	open := func(ctx context.Context, m *membership.Member) error {
		tx, ok := mpgx.Tx(ctx)
		if !ok {
			t.Fatal("no transaction in the hook's context")
		}
		_, err := tx.Exec(ctx, `INSERT INTO product_users (id) VALUES ($1)`, "u-"+m.IdentityID)
		if err != nil {
			return err
		}
		m.LocalID = "u-" + m.IdentityID
		return nil
	}

	m, err := store.Insert(ctx, membership.Member{IdentityID: "id-1", LocalID: "id-1", State: membership.StateProvisioning}, open)
	if err != nil || m.LocalID != "u-id-1" {
		t.Fatalf("insert with hook: %+v %v", m, err)
	}
	var users int
	_ = p.QueryRow(ctx, `SELECT count(*) FROM product_users`).Scan(&users)
	if users != 1 {
		t.Fatalf("product row not committed with the member: %d", users)
	}

	_, err = store.Insert(ctx, membership.Member{IdentityID: "id-2", LocalID: "id-2", State: membership.StateProvisioning}, func(ctx context.Context, m *membership.Member) error {
		tx, _ := mpgx.Tx(ctx)
		_, _ = tx.Exec(ctx, `INSERT INTO product_users (id) VALUES ('u-id-2')`)
		return errors.New("product refused")
	})
	if err == nil {
		t.Fatal("a failing hook must fail the insert")
	}
	if _, err := store.Get(ctx, "id-2"); !errors.Is(err, membership.ErrNotMember) {
		t.Fatalf("member committed despite the hook failure: %v", err)
	}
	_ = p.QueryRow(ctx, `SELECT count(*) FROM product_users`).Scan(&users)
	if users != 1 {
		t.Fatalf("product row committed despite the hook failure: %d", users)
	}

	ok, err := store.Transition(ctx, "id-1", membership.StateProvisioning, membership.StateErased, membership.Patch{}, func(ctx context.Context, m *membership.Member) error {
		tx, _ := mpgx.Tx(ctx)
		_, err := tx.Exec(ctx, `DELETE FROM product_users WHERE id = $1`, m.LocalID)
		return err
	})
	if err != nil || !ok {
		t.Fatalf("transition with hook: ok=%v err=%v", ok, err)
	}
	_ = p.QueryRow(ctx, `SELECT count(*) FROM product_users`).Scan(&users)
	if users != 0 {
		t.Fatalf("purge not committed with the tombstone: %d", users)
	}
}
