package sdk

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// The point of the endpoint: learning what an offer holds without spending it.
// Claiming to find out would burn the invitation of a visitor who then closes
// the tab.
func TestLookupInvitation_ReadsWithoutClaiming(t *testing.T) {
	srv, rec := server(t, 200, map[string]string{
		"email":      "invitee@example.com",
		"plan_code":  "collab",
		"status":     "claimable",
		"expires_at": "2026-09-01T00:00:00Z",
	})
	c := New(srv.URL, "k")

	inv, err := c.LookupInvitation(ctx(), "tok_abc")
	if err != nil {
		t.Fatalf("LookupInvitation: %v", err)
	}
	if inv.Email != "invitee@example.com" || inv.PlanCode != "collab" {
		t.Fatalf("invitation = %+v, want the invited address and tier", inv)
	}
	if !inv.Claimable() {
		t.Fatalf("status = %q, want claimable", inv.Status)
	}
	if rec.method != http.MethodGet {
		t.Fatalf("method = %s, want GET — a read must not consume", rec.method)
	}
	if !strings.Contains(rec.path, "tok_abc") {
		t.Fatalf("path = %q, want the token in it", rec.path)
	}
	if inv.ExpiresAt.IsZero() {
		t.Fatal("expires_at was reported and not parsed")
	}
}

// A lapsed or spent offer is a normal answer, not a failure: the app shows a
// different thing for each, and both say the invitation was real.
func TestLookupInvitation_ReportsASettledStatus(t *testing.T) {
	for _, status := range []string{"claimed", "expired"} {
		t.Run(status, func(t *testing.T) {
			srv, _ := server(t, 200, map[string]string{
				"email": "invitee@example.com", "plan_code": "collab", "status": status,
			})
			inv, err := New(srv.URL, "k").LookupInvitation(ctx(), "tok_abc")
			if err != nil {
				t.Fatalf("LookupInvitation: %v", err)
			}
			if string(inv.Status) != status {
				t.Fatalf("status = %q, want %q", inv.Status, status)
			}
			if inv.Claimable() {
				t.Fatalf("%s reads as claimable", status)
			}
		})
	}
}

// An answer with no status must not read as claimable: the generated pointers
// make "not said" indistinguishable from "empty", and offering a tier on an
// answer Lungor never gave is the wrong direction to guess in.
func TestLookupInvitation_AnAbsentStatusIsNotClaimable(t *testing.T) {
	srv, _ := server(t, 200, map[string]string{"email": "invitee@example.com"})

	inv, err := New(srv.URL, "k").LookupInvitation(ctx(), "tok_abc")
	if err != nil {
		t.Fatalf("LookupInvitation: %v", err)
	}
	if inv.Claimable() {
		t.Fatal("an answer with no status read as claimable")
	}
}

// A token belonging to another app answers exactly as an unknown one, so the
// caller cannot tell them apart either.
func TestLookupInvitation_UnknownTokenIsNotFound(t *testing.T) {
	srv, _ := server(t, 404, map[string]string{"message": "invitation not found"})

	if _, err := New(srv.URL, "k").LookupInvitation(ctx(), "tok_abc"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestLookupInvitation_RefusesAnEmptyToken(t *testing.T) {
	srv, rec := server(t, 200, nil)

	if _, err := New(srv.URL, "k").LookupInvitation(ctx(), ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
	if rec.method != "" {
		t.Fatal("called Lungor with no token")
	}
}

func TestLookupInvitation_RequiresConfiguration(t *testing.T) {
	if _, err := New("", "").LookupInvitation(ctx(), "tok_abc"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}
