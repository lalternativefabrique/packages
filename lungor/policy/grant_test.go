package policy

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type fakeProvisioner struct {
	calls int
	err   error
}

func (f *fakeProvisioner) Grant(_ context.Context, _, _ string) error {
	f.calls++
	return f.err
}

type fakeState struct {
	granted bool
	marked  int
	readErr error
	markErr error
}

func (f *fakeState) Granted(_ context.Context, _ string) (bool, error) {
	return f.granted, f.readErr
}

func (f *fakeState) MarkGranted(_ context.Context, _ string) error {
	f.marked++
	return f.markErr
}

type fakeEmails struct {
	email string
	ok    bool
	err   error
}

func (f *fakeEmails) Email(_ context.Context, _ string) (string, bool, error) {
	return f.email, f.ok, f.err
}

func TestEnsureSkipsGrantWhenAlreadyRecorded(t *testing.T) {
	prov := &fakeProvisioner{}
	g := &Grant{Provisioner: prov, State: &fakeState{granted: true}}

	granted, err := g.Ensure(context.Background(), "u1", "u@example.com")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !granted {
		t.Fatal("granted = false, want true: a recorded grant means the ledger answers")
	}
	if prov.calls != 0 {
		t.Fatalf("provisioner called %d times, want 0", prov.calls)
	}
}

func TestEnsureRecordsAfterGranting(t *testing.T) {
	prov := &fakeProvisioner{}
	state := &fakeState{}
	g := &Grant{Provisioner: prov, State: state}

	if _, err := g.Ensure(context.Background(), "u1", "u@example.com"); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if prov.calls != 1 {
		t.Fatalf("provisioner called %d times, want 1", prov.calls)
	}
	if state.marked != 1 {
		t.Fatalf("marked %d times, want 1", state.marked)
	}
}

// A grant that landed but was not recorded still reports success: the tenant
// works, only the bookkeeping is behind, and the next call re-grants — which
// idempotency absorbs.
func TestEnsureSucceedsWhenRecordingFails(t *testing.T) {
	g := &Grant{
		Provisioner: &fakeProvisioner{},
		State:       &fakeState{markErr: errors.New("db down")},
	}
	granted, err := g.Ensure(context.Background(), "u1", "u@example.com")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !granted {
		t.Fatal("granted = false, want true")
	}
}

// A state read that fails must not be taken for "not granted": re-granting on
// every request would hammer the ledger for as long as the store is down.
func TestEnsureFailsWhenStateUnreadable(t *testing.T) {
	prov := &fakeProvisioner{}
	g := &Grant{Provisioner: prov, State: &fakeState{readErr: errors.New("db down")}}

	if _, err := g.Ensure(context.Background(), "u1", "u@example.com"); err == nil {
		t.Fatal("Ensure = nil error, want the state read failure surfaced")
	}
	if prov.calls != 0 {
		t.Fatalf("provisioner called %d times, want 0", prov.calls)
	}
}

func TestEnsureResolvesEmailWhenCallerHasNone(t *testing.T) {
	g := &Grant{
		Provisioner: &fakeProvisioner{},
		Emails:      &fakeEmails{email: "resolved@example.com", ok: true},
	}
	granted, err := g.Ensure(context.Background(), "u1", "")
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !granted {
		t.Fatal("granted = false, want true")
	}
}

func TestEnsureFailsWithoutDeliverableEmail(t *testing.T) {
	cases := map[string]*Grant{
		"no resolver":    {Provisioner: &fakeProvisioner{}},
		"resolver empty": {Provisioner: &fakeProvisioner{}, Emails: &fakeEmails{ok: false}},
	}
	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			prov := g.Provisioner.(*fakeProvisioner)
			_, err := g.Ensure(context.Background(), "u1", "")
			if !errors.Is(err, ErrNoEmail) {
				t.Fatalf("err = %v, want ErrNoEmail", err)
			}
			if prov.calls != 0 {
				t.Fatal("granted without an address: Lungor refuses it, or an unreachable customer is registered")
			}
		})
	}
}

// The unconfigured case must stay silent: there is no ledger to provision
// against, and the consumer meters locally.
func TestZeroGrantIsInert(t *testing.T) {
	for name, g := range map[string]*Grant{"nil": nil, "no provisioner": {}} {
		t.Run(name, func(t *testing.T) {
			granted, err := g.Ensure(context.Background(), "u1", "u@example.com")
			if err != nil || granted {
				t.Fatalf("Ensure = (%v, %v), want (false, nil)", granted, err)
			}
			g.EnsureQuietly(context.Background(), "u1", "u@example.com")
		})
	}
}

// The open half of the rule: a signup survives a billing outage. The grant is
// left pending for the read paths and the sweep to retry.
func TestEnsureQuietlySwallowsFailure(t *testing.T) {
	g := &Grant{Provisioner: &fakeProvisioner{err: errors.New("lungor unreachable")}}
	g.EnsureQuietly(context.Background(), "u1", "u@example.com")
}

// The distinction the whole package turns on: an answer saying "never heard of
// them" is repaired, an absent answer is not.
func TestEnsureOnMissingGrantsOnlyForNotProvisioned(t *testing.T) {
	cases := []struct {
		name      string
		readErr   error
		wantGrant bool
	}{
		{"not provisioned is repaired", fmt.Errorf("%w: 404", ErrTenantNotProvisioned), true},
		{"outage is not", errors.New("connection refused"), false},
		{"success is not", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prov := &fakeProvisioner{}
			g := &Grant{Provisioner: prov, State: &fakeState{}}

			got := g.EnsureOnMissing(context.Background(), "u1", "u@example.com", tc.readErr)
			if got != tc.wantGrant {
				t.Fatalf("EnsureOnMissing = %v, want %v", got, tc.wantGrant)
			}
			want := 0
			if tc.wantGrant {
				want = 1
			}
			if prov.calls != want {
				t.Fatalf("provisioner called %d times, want %d", prov.calls, want)
			}
		})
	}
}

// A repair that cannot complete reports false rather than erroring: the caller
// is already handling a failed read and only needs to know whether a retry is
// worth it.
func TestEnsureOnMissingReportsFalseWhenGrantFails(t *testing.T) {
	g := &Grant{Provisioner: &fakeProvisioner{err: errors.New("lungor unreachable")}}

	if g.EnsureOnMissing(context.Background(), "u1", "u@example.com",
		fmt.Errorf("%w: 404", ErrTenantNotProvisioned)) {
		t.Fatal("EnsureOnMissing = true, want false: nothing was granted")
	}
}

// EnsureNow is the repair path's grant: it skips the record and calls anyway,
// where Ensure exists precisely to avoid that call.
func TestEnsureNowGrantsDespiteTheRecord(t *testing.T) {
	prov := &fakeProvisioner{}
	state := &fakeState{granted: true}
	g := &Grant{Provisioner: prov, State: state, Emails: &fakeEmails{email: "u@example.com", ok: true}}

	granted, err := g.EnsureNow(context.Background(), "u1", "")
	if err != nil {
		t.Fatalf("EnsureNow: %v", err)
	}
	if !granted {
		t.Fatal("granted = false, want true")
	}
	if prov.calls != 1 {
		t.Fatalf("provisioner called %d times, want 1", prov.calls)
	}
	if state.marked != 1 {
		t.Fatalf("marked %d times, want 1", state.marked)
	}
}
