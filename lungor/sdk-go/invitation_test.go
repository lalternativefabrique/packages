package sdk

import (
	"errors"
	"net/http"
	"testing"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

func TestClaimInvitation_SendsTheTokenAndUser(t *testing.T) {
	code, end := "free", "2026-09-05T10:00:00Z"
	srv, rec := server(t, http.StatusOK, wire.FinanceClaimResp{
		PlanCode: &code, PeriodEnd: &end,
	})
	c := New(srv.URL, "k")

	out, err := c.ClaimInvitation(ctx(), ClaimInput{
		Token: "tok_abc", ExternalUserID: "usr_42", Country: "FR",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.path != "/api/v1/invitations/claim" {
		t.Fatalf("path = %q", rec.path)
	}
	if rec.body["token"] != "tok_abc" ||
		rec.body["external_user_id"] != "usr_42" ||
		rec.body["country"] != "FR" {
		t.Fatalf("body = %+v", rec.body)
	}
	if out.PlanCode != "free" {
		t.Fatalf("result = %+v", out)
	}
	if out.PeriodEnd.IsZero() {
		t.Fatal("period end was not parsed")
	}
}

func TestClaimInvitation_OmitsAnUnsetCountry(t *testing.T) {
	srv, rec := server(t, http.StatusOK, wire.FinanceClaimResp{})
	c := New(srv.URL, "k")

	if _, err := c.ClaimInvitation(ctx(), ClaimInput{
		Token: "tok_abc", ExternalUserID: "usr_42",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := rec.body["country"]; present {
		t.Fatalf("country was sent: %+v", rec.body)
	}
}

// The claim is single-use and scoped to one app, so each refusal says something
// different to the invitee. Folding them together would leave someone holding a
// real but lapsed link unable to tell it from a wrong address.
func TestClaimInvitation_DistinguishesTheRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		want   error
	}{
		{"unknown token", http.StatusNotFound, ErrNotFound},
		{"already claimed", http.StatusConflict, ErrConflict},
		{"expired link", http.StatusGone, ErrInvitationExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := server(t, tc.status, wire.FinanceClaimResp{})
			c := New(srv.URL, "k")

			_, err := c.ClaimInvitation(ctx(), ClaimInput{
				Token: "tok_abc", ExternalUserID: "usr_42",
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestClaimInvitation_RefusesIncompleteInput(t *testing.T) {
	srv, _ := server(t, http.StatusOK, wire.FinanceClaimResp{})
	c := New(srv.URL, "k")

	for _, in := range []ClaimInput{
		{ExternalUserID: "usr_42"},
		{Token: "tok_abc"},
	} {
		if _, err := c.ClaimInvitation(ctx(), in); !errors.Is(err, ErrBadRequest) {
			t.Fatalf("err = %v, want ErrBadRequest for %+v", err, in)
		}
	}
}

func TestClaimInvitation_UnconfiguredNeverCalls(t *testing.T) {
	if _, err := New("", "").ClaimInvitation(ctx(), ClaimInput{
		Token: "tok_abc", ExternalUserID: "usr_42",
	}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}
