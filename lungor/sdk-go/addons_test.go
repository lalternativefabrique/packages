package sdk

import (
	"errors"
	"net/http"
	"testing"
)

func TestAddons_ReadsTheWrappedList(t *testing.T) {
	srv, rec := server(t, http.StatusOK, map[string]any{"addons": []map[string]any{{
		"code": "seat", "unit": "seat", "quantity": 3, "amount_cents": 500, "interval": "month", "interval_count": 1,
	}}})
	got, err := New(srv.URL, "k").Addons(ctx(), "u1")
	if err != nil {
		t.Fatalf("Addons: %v", err)
	}
	if rec.path != "/api/v1/addons?external_user_id=u1" {
		t.Fatalf("path = %q", rec.path)
	}
	if len(got) != 1 || got[0].Code != "seat" || got[0].Quantity != 3 || got[0].AmountCents != 500 || got[0].IntervalCount != 1 {
		t.Fatalf("addons = %+v", got)
	}
}

func TestSetAddonQuantity_SendsTheQuantity(t *testing.T) {
	srv, rec := server(t, http.StatusOK, map[string]any{"code": "seat", "quantity": 4})
	q, err := New(srv.URL, "k").SetAddonQuantity(ctx(), "u1", "seat", 4)
	if err != nil || q != 4 {
		t.Fatalf("q=%d err=%v", q, err)
	}
	if rec.method != http.MethodPut || rec.path != "/api/v1/addons/seat/quantity" ||
		rec.body["external_user_id"] != "u1" || rec.body["quantity"] != float64(4) {
		t.Fatalf("request = %+v", rec)
	}
}

func TestSetAddonQuantity_TypedErrors(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   error
	}{
		{http.StatusUnprocessableEntity, "quantity", ErrAddonQuantity},
		{http.StatusConflict, "plan_required", ErrAddonPlanRequired},
		{http.StatusNotFound, "addon", ErrAddonNotFound},
		{http.StatusPaymentRequired, "payment_required", ErrPaymentRequired},
	}
	for _, c := range cases {
		srv, _ := server(t, c.status, map[string]any{"error": c.code, "checkout_url": "https://pay.example/x"})
		_, err := New(srv.URL, "k").SetAddonQuantity(ctx(), "u1", "seat", 1)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.code, err, c.want)
		}
		var pr *PaymentRequiredError
		if c.status == http.StatusPaymentRequired && (!errors.As(err, &pr) || pr.CheckoutURL != "https://pay.example/x") {
			t.Errorf("payment required error carries no checkout url: %v", err)
		}
	}
}
