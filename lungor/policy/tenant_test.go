package policy

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type fakeLedger struct {
	calls int
	// errs is consumed one per call, so a read can fail then succeed after a
	// repair.
	errs []error
	bal  Balance
}

func (f *fakeLedger) Balance(_ context.Context, _, _ string) (Balance, error) {
	f.calls++
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return Balance{}, err
		}
	}
	return f.bal, nil
}

func TestAnonymousTenantsStayOffTheLedger(t *testing.T) {
	r := &Reader{Ledger: &fakeLedger{}}

	if r.LedgerFor(AnonPrefix+"abc123") != nil {
		t.Fatal("an anonymous id reached the remote ledger: it has no durable balance to keep")
	}
	if r.LedgerFor("u1") == nil {
		t.Fatal("a real tenant was kept off the ledger")
	}
}

// ErrNoLedger is a state, not an outage. A caller that treats it as a failure
// locks out every anonymous visitor.
func TestBalanceReportsNoLedgerRatherThanFailing(t *testing.T) {
	for name, r := range map[string]*Reader{
		"unconfigured": {},
		"anonymous":    {Ledger: &fakeLedger{}},
	} {
		t.Run(name, func(t *testing.T) {
			id := AnonPrefix + "abc"
			if name == "unconfigured" {
				id = "u1"
			}
			_, err := r.Balance(context.Background(), id, "credit")
			if !errors.Is(err, ErrNoLedger) {
				t.Fatalf("err = %v, want ErrNoLedger", err)
			}
		})
	}
}

// The recovery path: the read that discovers an unprovisioned tenant is also
// what repairs it, so the user gets a balance instead of an empty state.
func TestBalanceGrantsAndRereadsWhenTenantUnknown(t *testing.T) {
	ledger := &fakeLedger{
		errs: []error{fmt.Errorf("%w: 404", ErrTenantNotProvisioned)},
		bal:  Balance{Limit: 2000, Remaining: 2000},
	}
	prov := &fakeProvisioner{}
	r := &Reader{
		Ledger: ledger,
		Grant:  &Grant{Provisioner: prov, State: &fakeState{}, Emails: &fakeEmails{email: "u@example.com", ok: true}},
	}

	bal, err := r.Balance(context.Background(), "u1", "credit")
	if err != nil {
		t.Fatalf("Balance: %v", err)
	}
	if bal.Remaining != 2000 {
		t.Fatalf("Remaining = %d, want 2000 after repair", bal.Remaining)
	}
	if prov.calls != 1 {
		t.Fatalf("provisioner called %d times, want 1", prov.calls)
	}
	if ledger.calls != 2 {
		t.Fatalf("ledger read %d times, want 2 (the read that failed, then the retry)", ledger.calls)
	}
}

// The closed half of the rule: an unreachable ledger surfaces untouched. No
// grant is attempted — it could not have helped — and no balance is invented.
func TestBalancePassesOutageThroughWithoutGranting(t *testing.T) {
	outage := errors.New("connection refused")
	ledger := &fakeLedger{errs: []error{outage}}
	prov := &fakeProvisioner{}
	r := &Reader{Ledger: ledger, Grant: &Grant{Provisioner: prov, State: &fakeState{}}}

	if _, err := r.Balance(context.Background(), "u1", "credit"); !errors.Is(err, outage) {
		t.Fatalf("err = %v, want the outage surfaced", err)
	}
	if prov.calls != 0 {
		t.Fatal("granted during an outage: a doomed call added to a call already failing")
	}
	if ledger.calls != 1 {
		t.Fatalf("ledger read %d times, want 1: no retry without a repair", ledger.calls)
	}
}

// Without a Grant there is nothing to repair with, so the read error stands.
func TestBalanceWithoutGrantSurfacesNotProvisioned(t *testing.T) {
	r := &Reader{Ledger: &fakeLedger{errs: []error{fmt.Errorf("%w: 404", ErrTenantNotProvisioned)}}}

	if _, err := r.Balance(context.Background(), "u1", "credit"); !errors.Is(err, ErrTenantNotProvisioned) {
		t.Fatalf("err = %v, want ErrTenantNotProvisioned", err)
	}
}

func TestIsAnon(t *testing.T) {
	if !IsAnon(AnonPrefix + "abc123") {
		t.Fatal("IsAnon = false for a prefixed id")
	}
	if IsAnon("u1") {
		t.Fatal("IsAnon = true for a real tenant id")
	}
}
