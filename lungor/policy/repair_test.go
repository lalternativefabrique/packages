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

// Pressing the button twice must not cost two calls, nor two subscriptions.
func TestRepairIsIdempotent(t *testing.T) {
	prov := &fakeProvisioner{}
	g := &Grant{Provisioner: prov, State: &fakeState{granted: true}}

	granted, err := Repair(context.Background(), g, "tenant-1")
	if err != nil || !granted {
		t.Fatalf("Repair = (%v, %v), want (true, nil)", granted, err)
	}
	if prov.calls != 0 {
		t.Fatalf("called the ledger %d times for a tenant already granted", prov.calls)
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
