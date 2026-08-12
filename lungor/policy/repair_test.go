package policy

import (
	"context"
	"errors"
	"testing"
)

func TestRepairGrantsWhenTheLedgerHoldsNothing(t *testing.T) {
	prov := &fakeProvisioner{}
	g := &Grant{Provisioner: prov, State: &fakeState{}, Emails: &fakeEmails{email: "u@example.com", ok: true}}

	granted, err := Repair(context.Background(), g, "tenant-1")
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !granted {
		t.Fatal("granted = false, want true")
	}
	if prov.calls != 1 {
		t.Fatalf("provisioner called %d times, want 1", prov.calls)
	}
}

// The local record says a grant once succeeded, and nothing reconciles it with
// the ledger afterwards. A tenant the provider dropped is marked granted
// forever, so a repair that trusted the record would refuse the one case an
// operator reaches for it: an account that looks provisioned and is not.
func TestRepairIgnoresTheLocalRecord(t *testing.T) {
	prov := &fakeProvisioner{}
	state := &fakeState{granted: true}
	g := &Grant{Provisioner: prov, State: state, Emails: &fakeEmails{email: "u@example.com", ok: true}}

	granted, err := Repair(context.Background(), g, "tenant-1")
	if err != nil || !granted {
		t.Fatalf("Repair = (%v, %v), want (true, nil)", granted, err)
	}
	if prov.calls != 1 {
		t.Fatalf("reached the ledger %d times, want 1", prov.calls)
	}
}

// Pressing the button twice must not cost two subscriptions. The guarantee is
// the provider's — granting a tenant it already holds is a no-op — not the
// local record's.
func TestRepairIsIdempotentOnTheLedger(t *testing.T) {
	prov := &fakeProvisioner{}
	g := &Grant{Provisioner: prov, State: &fakeState{}, Emails: &fakeEmails{email: "u@example.com", ok: true}}

	for i := range 2 {
		granted, err := Repair(context.Background(), g, "tenant-1")
		if err != nil || !granted {
			t.Fatalf("Repair #%d = (%v, %v), want (true, nil)", i+1, granted, err)
		}
	}
}

// The three refusals that must never reach the ledger. Each is told apart
// because consumers answer them with different statuses.
func TestRepairRefusesBeforeReachingTheLedger(t *testing.T) {
	cases := []struct {
		name     string
		tenantID string
		grant    *Grant
		want     error
	}{
		{"no tenant", "", &Grant{Provisioner: &fakeProvisioner{}}, ErrNoTenant},
		{"anonymous", AnonPrefix + "abc", &Grant{Provisioner: &fakeProvisioner{}}, ErrAnonymousTenant},
		{"no ledger", "tenant-1", nil, ErrNoLedger},
		{"unconfigured ledger", "tenant-1", &Grant{}, ErrNoLedger},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			granted, err := Repair(context.Background(), tc.grant, tc.tenantID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if granted {
				t.Fatal("granted = true on a refusal")
			}
			if tc.grant != nil {
				if prov, ok := tc.grant.Provisioner.(*fakeProvisioner); ok && prov.calls != 0 {
					t.Fatalf("reached the ledger %d times", prov.calls)
				}
			}
		})
	}
}

func TestRepairSurfacesAGrantFailure(t *testing.T) {
	outage := errors.New("lungor unreachable")
	g := &Grant{Provisioner: &fakeProvisioner{err: outage}, State: &fakeState{}, Emails: &fakeEmails{email: "u@example.com", ok: true}}

	if _, err := Repair(context.Background(), g, "tenant-1"); !errors.Is(err, outage) {
		t.Fatalf("err = %v, want the outage surfaced", err)
	}
}
