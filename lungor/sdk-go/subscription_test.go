package sdk

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

func TestGrant_SendsTheUserPlanAndEmail(t *testing.T) {
	subID, code, end := "sub-1", "collab", "2026-09-05T10:00:00Z"
	srv, rec := server(t, http.StatusCreated, wire.FinanceAppGrantResponse{
		SubscriptionId: &subID, PlanCode: &code, PeriodEnd: &end,
	})
	c := New(srv.URL, "k")

	out, err := c.Grant(ctx(), GrantInput{
		ExternalUserID: "org_42", PlanCode: "collab", Email: "bob@example.fr",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.path != "/api/v1/subscriptions/grant" {
		t.Fatalf("path = %q", rec.path)
	}
	if rec.body["external_user_id"] != "org_42" ||
		rec.body["plan_code"] != "collab" ||
		rec.body["email"] != "bob@example.fr" {
		t.Fatalf("body = %+v", rec.body)
	}
	if out.SubscriptionID != "sub-1" || out.PlanCode != "collab" {
		t.Fatalf("result = %+v", out)
	}
	// The renewal date is what tells the caller when the allowance comes back.
	if out.PeriodEnd.IsZero() {
		t.Fatal("period end was not parsed")
	}
}

// An omitted country must not travel as an empty string: Lungor would store
// "" as a country rather than the absence the caller meant.
func TestGrant_OmitsAnUnsetCountry(t *testing.T) {
	srv, rec := server(t, http.StatusCreated, wire.FinanceAppGrantResponse{})
	c := New(srv.URL, "k")

	if _, err := c.Grant(ctx(), GrantInput{
		ExternalUserID: "org_42", PlanCode: "collab", Email: "bob@example.fr",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := rec.body["country"]; present {
		t.Fatalf("country was sent: %+v", rec.body)
	}
}

// Already subscribed is a customer state, not an outage: the caller must be
// able to tell it from Lungor being down and route it to ChangePlan instead.
func TestGrant_ReportsAConflictDistinctly(t *testing.T) {
	srv, _ := server(t, http.StatusConflict, map[string]string{
		"message": "user already has an active subscription",
	})
	c := New(srv.URL, "k")

	_, err := c.Grant(ctx(), GrantInput{
		ExternalUserID: "org_42", PlanCode: "collab", Email: "bob@example.fr",
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
}

// Refused locally rather than sent: a request Lungor is certain to reject is
// one round trip nobody needs, and the error names which field is missing.
func TestGrant_RefusesIncompleteInputWithoutCalling(t *testing.T) {
	srv, rec := server(t, http.StatusCreated, wire.FinanceAppGrantResponse{})
	c := New(srv.URL, "k")

	if _, err := c.Grant(ctx(), GrantInput{ExternalUserID: "org_42"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
	if rec.path != "" {
		t.Fatalf("an incomplete grant was sent anyway: %q", rec.path)
	}
}

func TestChangePlan_SendsTheUserAndPlanCode(t *testing.T) {
	kind, applied := "upgrade", true
	prorated := 450
	srv, rec := server(t, 200, wire.FinanceAppChangePlanResponse{
		Kind: &kind, AppliedNow: &applied, ProratedCents: &prorated,
	})
	c := New(srv.URL, "k")

	out, err := c.ChangePlan(ctx(), ChangePlanInput{
		ExternalUserID: "user-1", PlanCode: "pro", Direction: DirectionUp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.path != "/api/v1/subscriptions/change-plan" {
		t.Fatalf("path = %q", rec.path)
	}
	if rec.body["external_user_id"] != "user-1" || rec.body["plan_code"] != "pro" {
		t.Fatalf("body = %+v", rec.body)
	}
	if rec.body["direction"] != "up" {
		t.Fatalf("direction = %v, want up", rec.body["direction"])
	}
	if !out.AppliedNow || out.Kind != "upgrade" || out.ProratedCents != 450 {
		t.Fatalf("result = %+v", out)
	}
}

// DirectionAny must not send the field at all: an empty string would be a value
// Lungor rejects rather than the absence the caller meant.
func TestChangePlan_OmitsAnUnsetDirection(t *testing.T) {
	srv, rec := server(t, 200, wire.FinanceAppChangePlanResponse{})
	c := New(srv.URL, "k")

	_, _ = c.ChangePlan(ctx(), ChangePlanInput{ExternalUserID: "u1", PlanCode: "pro"})

	if _, sent := rec.body["direction"]; sent {
		t.Fatalf("direction was sent as %q, want it omitted", rec.body["direction"])
	}
}

// 402 is a legitimate answer, not a failure: the move is allowed and the
// figures to show the customer come back with it.
func TestChangePlan_ConsentRequiredCarriesTheFigures(t *testing.T) {
	required := true
	amount, recurring := 1200, 1700
	srv, _ := server(t, http.StatusPaymentRequired, wire.FinanceAppChangePlanResponse{
		ConsentRequired: &required, ConsentAmountCents: &amount, ConsentRecurringCents: &recurring,
	})
	c := New(srv.URL, "k")

	out, err := c.ChangePlan(ctx(), ChangePlanInput{ExternalUserID: "u1", PlanCode: "max"})

	if !errors.Is(err, ErrConsentRequired) {
		t.Fatalf("err = %v, want ErrConsentRequired", err)
	}
	if !out.ConsentRequired || out.ConsentAmount != 1200 || out.ConsentRecurring != 1700 {
		t.Fatalf("result = %+v, want the figures to display", out)
	}
}

func TestChangePlan_SendsTheAgreedAmount(t *testing.T) {
	srv, rec := server(t, 200, wire.FinanceAppChangePlanResponse{})
	c := New(srv.URL, "k")

	_, _ = c.ChangePlan(ctx(), ChangePlanInput{
		ExternalUserID: "u1", PlanCode: "max",
		Agreed: true, AgreedAmountCents: 1200,
	})

	if rec.body["agreed"] != true {
		t.Fatalf("agreed = %v, want true", rec.body["agreed"])
	}
	if rec.body["agreed_amount_cents"] != float64(1200) {
		t.Fatalf("agreed_amount_cents = %v, want 1200", rec.body["agreed_amount_cents"])
	}
}

func TestChangePlan_ParsesTheEffectiveDate(t *testing.T) {
	when := "2026-09-01T00:00:00Z"
	srv, _ := server(t, 200, wire.FinanceAppChangePlanResponse{EffectiveAt: &when})
	c := New(srv.URL, "k")

	out, err := c.ChangePlan(ctx(), ChangePlanInput{ExternalUserID: "u1", PlanCode: "solo"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.EffectiveAt.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("effective at = %v", out.EffectiveAt)
	}
}

func TestChangePlan_RefusesIncompleteInput(t *testing.T) {
	srv, rec := server(t, 200, wire.FinanceAppChangePlanResponse{})
	c := New(srv.URL, "k")

	if _, err := c.ChangePlan(ctx(), ChangePlanInput{PlanCode: "pro"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("missing user: err = %v", err)
	}
	if _, err := c.ChangePlan(ctx(), ChangePlanInput{ExternalUserID: "u1"}); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("missing plan: err = %v", err)
	}
	if rec.method != "" {
		t.Fatal("no request should have been sent")
	}
}

func TestCancel_DefaultsAreExplicit(t *testing.T) {
	status, when := "canceled", "2026-09-01T00:00:00Z"
	srv, rec := server(t, 200, wire.FinanceAppCancelResponse{Status: &status, EffectiveAt: &when})
	c := New(srv.URL, "k")

	out, err := c.Cancel(ctx(), "user-1", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.path != "/api/v1/subscriptions/cancel" {
		t.Fatalf("path = %q", rec.path)
	}
	if rec.body["at_period_end"] != true {
		t.Fatalf("at_period_end = %v, want true", rec.body["at_period_end"])
	}
	if out.Status != "canceled" || out.EffectiveAt.IsZero() {
		t.Fatalf("result = %+v", out)
	}
}

// false must reach the wire: relying on omitempty would silently turn an
// immediate cancellation into an at-period-end one.
func TestCancel_SendsAnExplicitFalse(t *testing.T) {
	srv, rec := server(t, 200, wire.FinanceAppCancelResponse{})
	c := New(srv.URL, "k")

	_, _ = c.Cancel(ctx(), "user-1", false)

	if rec.body["at_period_end"] != false {
		t.Fatalf("at_period_end = %v, want an explicit false", rec.body["at_period_end"])
	}
}

func TestWithdrawPendingPlan_ReportsWhatWasWithdrawn(t *testing.T) {
	withdrawn, code := true, "solo"
	srv, rec := server(t, 200, wire.FinanceAppWithdrawPendingResponse{Withdrawn: &withdrawn, PlanCode: &code})
	c := New(srv.URL, "k")

	got, plan, err := c.WithdrawPendingPlan(ctx(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rec.path != "/api/v1/subscriptions/withdraw-pending-plan" {
		t.Fatalf("path = %q", rec.path)
	}
	if !got || plan != "solo" {
		t.Fatalf("withdrawn=%v plan=%q", got, plan)
	}
}

// Nothing scheduled is not an error: a customer clicking twice must not be told
// off for a promise that is already gone.
func TestWithdrawPendingPlan_NothingScheduledIsNotAnError(t *testing.T) {
	withdrawn := false
	srv, _ := server(t, 200, wire.FinanceAppWithdrawPendingResponse{Withdrawn: &withdrawn})
	c := New(srv.URL, "k")

	got, _, err := c.WithdrawPendingPlan(ctx(), "user-1")

	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got {
		t.Fatal("withdrawn must be false")
	}
}

// A user with no subscription is a distinct outcome from a broken call: the
// caller shows "nothing to change", not an error.
func TestSubscriptionOps_MapNotFound(t *testing.T) {
	srv, _ := server(t, http.StatusNotFound, nil)
	c := New(srv.URL, "k")

	if _, err := c.Cancel(ctx(), "stranger", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancel: err = %v, want ErrNotFound", err)
	}
	if _, _, err := c.WithdrawPendingPlan(ctx(), "stranger"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("withdraw: err = %v, want ErrNotFound", err)
	}
	if _, err := c.ChangePlan(ctx(), ChangePlanInput{ExternalUserID: "s", PlanCode: "pro"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("change-plan: err = %v, want ErrNotFound", err)
	}
}

func TestSubscriptionOps_RefuseWhenUnconfigured(t *testing.T) {
	c := New("", "k")

	if _, err := c.ChangePlan(ctx(), ChangePlanInput{ExternalUserID: "u", PlanCode: "p"}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("change-plan: %v", err)
	}
	if _, err := c.Cancel(ctx(), "u", true); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("cancel: %v", err)
	}
	if _, _, err := c.WithdrawPendingPlan(ctx(), "u"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("withdraw: %v", err)
	}
}
