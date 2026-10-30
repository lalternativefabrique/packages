package membership_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
		Identifier: membership.IdentifierPolicy{OwnedDomain: "messag.test", Reserved: []string{"postmaster", "sylvain", "codesyl"}, Refuse: func(bare string) bool { return strings.HasPrefix(bare, "president") }},
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
		"president.x": membership.ErrReserved,
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

func clocked(t *testing.T, res membership.Resources, open func(membership.Identity) string) (*membership.Service, *membership.Memory, *time.Time) {
	t.Helper()
	store := membership.NewMemory()
	if res == nil {
		res = membership.NewNoop(store)
	}
	now := time.Now().UTC()
	p := membership.Product{
		Name:       "messag",
		Resources:  res,
		Identifier: membership.IdentifierPolicy{OwnedDomain: "messag.test"},
	}
	if open != nil {
		p.Open = func(_ context.Context, id membership.Identity) (string, error) { return open(id), nil }
	}
	svc, err := membership.New(store, p, membership.Config{BaseBackoff: time.Millisecond, MaxBackoff: 2 * time.Millisecond, MaxAttempts: 2, ReopenAfter: time.Hour, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, &now
}

func TestResolveReopensAnErasedMemberPastTheGrace(t *testing.T) {
	res := &fakeResources{}
	opened := 0
	svc, store, now := clocked(t, res, func(membership.Identity) string { opened++; return "u-" + string(rune('0'+opened)) })
	ctx := context.Background()
	ana := membership.Identity{ID: "id-1", Email: "ana@messag.test"}
	if _, err := svc.Resolve(ctx, ana); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WorkOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.RequestErase(ctx, "id-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WorkOnce(ctx); err != nil {
		t.Fatal(err)
	}
	m, err := svc.Resolve(ctx, ana)
	if !errors.Is(err, membership.ErrErased) {
		t.Fatalf("a token inside the grace must be refused: %v", err)
	}
	if at, ok := svc.ReopensAt(m); !ok || !at.Equal(m.ErasedAt.Add(time.Hour)) {
		t.Fatalf("reopens at %v (%v), erased at %v", at, ok, m.ErasedAt)
	}
	*now = now.Add(59 * time.Minute)
	if _, err := svc.Resolve(ctx, ana); !errors.Is(err, membership.ErrErased) {
		t.Fatalf("still inside the grace: %v", err)
	}
	*now = now.Add(2 * time.Minute)
	m, err = svc.Resolve(ctx, ana)
	if err != nil {
		t.Fatalf("past the grace the identity comes back: %v", err)
	}
	if m.State != membership.StateProvisioning || m.LocalID != "u-2" || m.ErasedAt != nil || m.ReadyAt != nil || m.Address != "ana@messag.test" || string(m.Resources) != "{}" || opened != 2 {
		t.Fatalf("reopened %+v opened=%d", m, opened)
	}
	if n, err := svc.WorkOnce(ctx); err != nil || n != 1 {
		t.Fatalf("reprovision: %d %v", n, err)
	}
	m, _ = store.Get(ctx, "id-1")
	if m.State != membership.StateReady || len(res.held) != 2 {
		t.Fatalf("after reprovisioning %+v held=%v", m, res.held)
	}
	if _, err := svc.Resolve(ctx, ana); err != nil {
		t.Fatalf("a reopened member is admitted: %v", err)
	}
}

func TestATombstoneReopensToo(t *testing.T) {
	svc, _, now := clocked(t, nil, nil)
	ctx := context.Background()
	if err := svc.RequestErase(ctx, "never-seen"); err != nil {
		t.Fatal(err)
	}
	id := membership.Identity{ID: "never-seen", Email: "x@messag.test"}
	if _, err := svc.Resolve(ctx, id); !errors.Is(err, membership.ErrErased) {
		t.Fatalf("tombstone ignored: %v", err)
	}
	*now = now.Add(time.Hour)
	m, err := svc.Resolve(ctx, id)
	if err != nil || m.State != membership.StateProvisioning || m.Address != "x@messag.test" {
		t.Fatalf("tombstone not reopened: %+v %v", m, err)
	}
}

func TestUnreserveLiftsAReservedIdentifier(t *testing.T) {
	var asked []string
	svc, _ := newService(t, nil, func(p *membership.Product) {
		p.Unreserve = func(_ context.Context, local, claim string) (bool, error) {
			asked = append(asked, local+"|"+claim)
			return local == "sylvain" && (claim == "ok" || claim == ""), nil
		}
	})
	ctx := context.Background()
	if err := svc.AvailableWithClaim(ctx, "sylvain", "ok"); err != nil {
		t.Fatalf("claimed: %v", err)
	}
	if err := svc.AvailableWithClaim(ctx, "postmaster", "ok"); !errors.Is(err, membership.ErrReserved) {
		t.Fatalf("other name: %v", err)
	}
	if err := svc.AvailableWithClaim(ctx, "free", "ok"); err != nil {
		t.Fatalf("free: %v", err)
	}
	m, err := svc.Resolve(ctx, membership.Identity{ID: "id-1", Email: "sylvain@messag.test"})
	if err != nil || m.Address != "sylvain@messag.test" {
		t.Fatalf("open: %+v %v", m, err)
	}
	if err := svc.AvailableWithClaim(ctx, "sylvain", "ok"); !errors.Is(err, membership.ErrTaken) {
		t.Fatalf("after open: %v", err)
	}
	want := []string{"sylvain|ok", "postmaster|ok", "sylvain|", "sylvain|ok"}
	if strings.Join(asked, ",") != strings.Join(want, ",") {
		t.Fatalf("asked %v, want %v", asked, want)
	}
}

func TestMemberWithoutIdentifierWaitsThenProvisionsTheChosenOne(t *testing.T) {
	ctx := context.Background()
	var provisioned []string
	res := resourcesFunc(func(_ context.Context, m membership.Member) (json.RawMessage, error) {
		if m.Address == "" {
			return nil, membership.ErrNeedsIdentifier
		}
		provisioned = append(provisioned, m.Address)
		return nil, nil
	})
	stuck := 0
	store := membership.NewMemory()
	svc, err := membership.New(store, membership.Product{
		Name:       "messag",
		Resources:  res,
		Identifier: membership.IdentifierPolicy{OwnedDomain: "messag.test", Reserved: []string{"postmaster"}},
	}, membership.Config{OnStuck: func(membership.Member, error) { stuck++ }})
	if err != nil {
		t.Fatal(err)
	}
	id := membership.Identity{ID: "id-1", Email: "ana@gmail.test"}
	if _, err := svc.Resolve(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ChooseIdentifier(ctx, "id-1", "ana"); !errors.Is(err, membership.ErrIdentifierNotExpected) {
		t.Fatalf("before the worker ran: %v", err)
	}
	if _, err := svc.WorkOnce(ctx); err != nil {
		t.Fatal(err)
	}
	m, err := svc.Resolve(ctx, id)
	if !errors.Is(err, membership.ErrNeedsIdentifier) || m.State != membership.StateNeedsIdentifier {
		t.Fatalf("got %+v, %v", m, err)
	}
	if stuck != 0 {
		t.Fatalf("waiting for a choice is not stuck: %d", stuck)
	}
	if _, err := svc.ChooseIdentifier(ctx, "id-1", "postmaster"); !errors.Is(err, membership.ErrReserved) {
		t.Fatalf("reserved: %v", err)
	}
	if _, err := svc.ChooseIdentifier(ctx, "id-1", "a b"); !errors.Is(err, membership.ErrInvalidIdentifier) {
		t.Fatalf("invalid: %v", err)
	}
	m, err = svc.ChooseIdentifier(ctx, "id-1", " Ana ")
	if err != nil || m.State != membership.StateProvisioning || m.Address != "ana@messag.test" {
		t.Fatalf("got %+v, %v", m, err)
	}
	if _, err := svc.ChooseIdentifier(ctx, "id-1", "ana"); err != nil {
		t.Fatalf("repeated submit: %v", err)
	}
	if _, err := svc.ChooseIdentifier(ctx, "id-1", "other"); !errors.Is(err, membership.ErrIdentifierNotExpected) {
		t.Fatalf("second name: %v", err)
	}
	if _, err := svc.WorkOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if m, err = svc.Resolve(ctx, id); err != nil || m.State != membership.StateReady {
		t.Fatalf("got %+v, %v", m, err)
	}
	if len(provisioned) != 1 || provisioned[0] != "ana@messag.test" {
		t.Fatalf("provisioned %v", provisioned)
	}
}

func TestChoosingATakenIdentifierIsRefused(t *testing.T) {
	ctx := context.Background()
	res := resourcesFunc(func(_ context.Context, m membership.Member) (json.RawMessage, error) {
		if m.Address == "" {
			return nil, membership.ErrNeedsIdentifier
		}
		return nil, nil
	})
	svc, err := membership.New(membership.NewMemory(), membership.Product{
		Name: "messag", Resources: res, Identifier: membership.IdentifierPolicy{OwnedDomain: "messag.test"},
	}, membership.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, membership.Identity{ID: "id-1", Email: "ana@messag.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(ctx, membership.Identity{ID: "id-2", Email: "ana@gmail.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.WorkOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ChooseIdentifier(ctx, "id-2", "ana"); !errors.Is(err, membership.ErrTaken) {
		t.Fatalf("got %v", err)
	}
}

type resourcesFunc func(context.Context, membership.Member) (json.RawMessage, error)

func (f resourcesFunc) Provision(ctx context.Context, m membership.Member) (json.RawMessage, error) {
	return f(ctx, m)
}
func (resourcesFunc) Erase(context.Context, membership.Member) error { return nil }
func (resourcesFunc) List(context.Context) ([]string, error)         { return nil, nil }
