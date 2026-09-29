package membership_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lalternative/packages/go/membership"
)

type fakeResources struct {
	mu        sync.Mutex
	provision func(membership.Member) (json.RawMessage, error)
	erased    []string
	held      []string
}

func (f *fakeResources) Provision(_ context.Context, m membership.Member) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.provision == nil {
		f.held = append(f.held, m.Key())
		return json.RawMessage(`{"principal":"p-` + m.Key() + `"}`), nil
	}
	return f.provision(m)
}

func (f *fakeResources) Erase(_ context.Context, m membership.Member) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.erased = append(f.erased, m.Key())
	return nil
}

func (f *fakeResources) List(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.held...), nil
}

func newService(t *testing.T, res membership.Resources, opts ...func(*membership.Product)) (*membership.Service, *membership.Memory) {
	t.Helper()
	store := membership.NewMemory()
	if res == nil {
		res = membership.NewNoop(store)
	}
	p := membership.Product{
		Name:       "messag",
		Resources:  res,
		Identifier: membership.IdentifierPolicy{OwnedDomain: "messag.test", Reserved: []string{"sylvain", "codesyl"}},
	}
	for _, o := range opts {
		o(&p)
	}
	svc, err := membership.New(store, p, membership.Config{BaseBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond, MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	return svc, store
}

func TestResolveOpensTheMemberOnceThenReturnsIt(t *testing.T) {
	svc, _ := newService(t, nil)
	ctx := context.Background()
	first, err := svc.Resolve(ctx, membership.Identity{ID: "id-1", Email: "Ana@Messag.Test"})
	if err != nil {
		t.Fatal(err)
	}
	if first.State != membership.StateProvisioning || first.Address != "ana@messag.test" || first.LocalID != "id-1" {
		t.Fatalf("opened %+v", first)
	}
	if _, err := svc.WorkOnce(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := svc.Resolve(ctx, membership.Identity{ID: "id-1", Email: "other@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if second.State != membership.StateReady || second.Address != "ana@messag.test" {
		t.Fatalf("second resolve %+v, want ready under the first address", second)
	}
}

func TestResolveKeepsTheProductLocalID(t *testing.T) {
	svc, _ := newService(t, nil, func(p *membership.Product) {
		p.Open = func(_ context.Context, id membership.Identity) (string, error) { return "legacy-" + id.ID, nil }
	})
	m, err := svc.Resolve(context.Background(), membership.Identity{ID: "id-9", Email: "bob@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if m.LocalID != "legacy-id-9" || m.Address != "" {
		t.Fatalf("%+v", m)
	}
}

func TestReservedOrTakenIdentifierOpensInConflict(t *testing.T) {
	svc, _ := newService(t, nil)
	ctx := context.Background()
	if _, err := svc.Resolve(ctx, membership.Identity{ID: "id-1", Email: "ana@messag.test"}); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"id-2": "postmaster@messag.test",
		"id-3": "Code.Syl@messag.test",
		"id-4": "bad name@messag.test",
	}
	for id, email := range cases {
		_, err := svc.Resolve(ctx, membership.Identity{ID: id, Email: email})
		if !errors.Is(err, membership.ErrConflict) {
			t.Errorf("%s: got %v, want ErrConflict", email, err)
		}
	}
	// An address someone else already holds core-side is a conflict too, never an adoption.
	svc2, store := newService(t, nil)
	_, _ = store.Insert(ctx, membership.Member{IdentityID: "old", LocalID: "old", Address: "ana@messag.test", State: membership.StateReady}, nil)
	_, err := svc2.Resolve(ctx, membership.Identity{ID: "new", Email: "ana@messag.test"})
	if !errors.Is(err, membership.ErrConflict) {
		t.Fatalf("taken address: got %v", err)
	}
	m, _ := store.Get(ctx, "new")
	if m.Address != "" || m.State != membership.StateConflict {
		t.Fatalf("conflicting member %+v must not hold the address", m)
	}
}

func TestAvailable(t *testing.T) {
	svc, store := newService(t, nil)
	ctx := context.Background()
	_, _ = store.Insert(ctx, membership.Member{IdentityID: "x", LocalID: "x", Address: "taken@messag.test", State: membership.StateErased}, nil)
	cases := map[string]error{
		"free":        nil,
		"Free":        nil,
		"taken":       membership.ErrTaken,
		"postmaster":  membership.ErrReserved,
		"post.master": membership.ErrReserved,
		"code_syl":    membership.ErrReserved,
		"..bad":       membership.ErrInvalidIdentifier,
		"":            membership.ErrInvalidIdentifier,
	}
	for local, want := range cases {
		if got := svc.Available(ctx, local); !errors.Is(got, want) && got != want {
			t.Errorf("Available(%q) = %v, want %v", local, got, want)
		}
	}
}

func TestWorkerProvisionsThenErases(t *testing.T) {
	res := &fakeResources{}
	svc, store := newService(t, res)
	ctx := context.Background()
	if _, err := svc.Resolve(ctx, membership.Identity{ID: "id-1", Email: "ana@messag.test"}); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.WorkOnce(ctx); err != nil || n != 1 {
		t.Fatalf("work: %d %v", n, err)
	}
	m, _ := store.Get(ctx, "id-1")
	if m.State != membership.StateReady || string(m.Resources) != `{"principal":"p-ana@messag.test"}` || m.ReadyAt == nil {
		t.Fatalf("after provisioning %+v", m)
	}
	if n, _ := svc.WorkOnce(ctx); n != 0 {
		t.Fatalf("a ready member was claimed again")
	}

	purged := 0
	svc2, _ := membership.New(store, membership.Product{Name: "messag", Resources: res, Purge: func(context.Context, membership.Member) error { purged++; return nil }}, membership.Config{})
	if err := svc2.RequestErase(ctx, "id-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc2.Resolve(ctx, membership.Identity{ID: "id-1"}); !errors.Is(err, membership.ErrErased) {
		t.Fatalf("erasing member admitted: %v", err)
	}
	if _, err := svc2.WorkOnce(ctx); err != nil {
		t.Fatal(err)
	}
	m, _ = store.Get(ctx, "id-1")
	if m.State != membership.StateErased || m.ErasedAt == nil || m.Address != "ana@messag.test" || purged != 1 || len(res.erased) != 1 {
		t.Fatalf("after erasure %+v purged=%d erased=%v", m, purged, res.erased)
	}
	if err := svc2.RequestErase(ctx, "id-1"); err != nil {
		t.Fatalf("erase twice: %v", err)
	}
	if got := svc.Available(ctx, "ana"); !errors.Is(got, membership.ErrTaken) {
		t.Fatalf("an erased address must stay taken, got %v", got)
	}
}

func TestEraseOfAnUnknownIdentityLeavesATombstone(t *testing.T) {
	svc, _ := newService(t, nil)
	ctx := context.Background()
	if err := svc.RequestErase(ctx, "never-seen"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, membership.Identity{ID: "never-seen", Email: "x@messag.test"}); !errors.Is(err, membership.ErrErased) {
		t.Fatalf("tombstone ignored: %v", err)
	}
}

func TestWorkerRetriesAndReportsStuck(t *testing.T) {
	calls := 0
	res := &fakeResources{provision: func(membership.Member) (json.RawMessage, error) {
		calls++
		if calls < 3 {
			return nil, errors.New("stalwart down")
		}
		return json.RawMessage(`{"ok":true}`), nil
	}}
	store := membership.NewMemory()
	stuck := 0
	svc, _ := membership.New(store, membership.Product{Name: "messag", Resources: res}, membership.Config{
		BaseBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxAttempts: 2,
		OnStuck: func(membership.Member, error) { stuck++ },
	})
	ctx := context.Background()
	_, _ = svc.Resolve(ctx, membership.Identity{ID: "id-1"})
	for i := 0; i < 3; i++ {
		time.Sleep(2 * time.Millisecond)
		if _, err := svc.WorkOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	m, _ := store.Get(ctx, "id-1")
	if m.State != membership.StateReady || calls != 3 || stuck != 1 {
		t.Fatalf("state=%s calls=%d stuck=%d last=%q", m.State, calls, stuck, m.LastError)
	}
}

func TestProvisionConflictParksTheMember(t *testing.T) {
	res := &fakeResources{provision: func(membership.Member) (json.RawMessage, error) {
		return nil, errors.New("account exists outside members: " + membership.ErrConflict.Error())
	}}
	res.provision = func(membership.Member) (json.RawMessage, error) {
		return nil, membership.ErrConflict
	}
	svc, store := newService(t, res)
	ctx := context.Background()
	_, _ = svc.Resolve(ctx, membership.Identity{ID: "id-1", Email: "ana@messag.test"})
	if _, err := svc.WorkOnce(ctx); err != nil {
		t.Fatal(err)
	}
	m, _ := store.Get(ctx, "id-1")
	if m.State != membership.StateConflict {
		t.Fatalf("%+v", m)
	}
	if _, err := svc.Resolve(ctx, membership.Identity{ID: "id-1"}); !errors.Is(err, membership.ErrConflict) {
		t.Fatalf("conflicting member admitted: %v", err)
	}
	if n, _ := svc.WorkOnce(ctx); n != 0 {
		t.Fatal("a conflicting member is not retried")
	}
}

type fakeIdentities []string

func (f fakeIdentities) WithRole(context.Context, string) ([]string, error) { return f, nil }

func TestReconcileReportsEveryDrift(t *testing.T) {
	res := &fakeResources{held: []string{"ghost@messag.test"}}
	svc, store := newService(t, res, func(p *membership.Product) { p.Identities = fakeIdentities{"id-1", "id-2", "id-lost"} })
	ctx := context.Background()
	_, _ = svc.Resolve(ctx, membership.Identity{ID: "id-1", Email: "ana@messag.test"})
	_, _ = svc.Resolve(ctx, membership.Identity{ID: "id-2", Email: "bob@messag.test"})
	_, _ = svc.Resolve(ctx, membership.Identity{ID: "id-gone", Email: "gone@messag.test"})
	_, _ = svc.Resolve(ctx, membership.Identity{ID: "id-bad", Email: "postmaster@messag.test"})
	if _, err := svc.WorkOnce(ctx); err != nil {
		t.Fatal(err)
	}
	res.mu.Lock()
	res.held = []string{"ghost@messag.test", "ana@messag.test", "gone@messag.test"}
	res.mu.Unlock()
	_ = store.Fail(ctx, "id-2", "", time.Now())

	r, err := svc.Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := membership.Report{
		IdentitiesWithoutMember: []string{"id-lost"},
		MembersWithoutIdentity:  []string{"id-gone"},
		ResourcesWithoutMember:  []string{"ghost@messag.test"},
		MembersWithoutResource:  []string{"id-2"},
		Conflicts:               []string{"id-bad: postmaster@messag.test: identifier reserved"},
	}
	if got, exp := toJSON(r), toJSON(want); got != exp {
		t.Fatalf("report\n got %s\nwant %s", got, exp)
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
