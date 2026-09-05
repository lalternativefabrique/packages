package sdk

import (
	"errors"
	"net/http"
	"testing"
)

func TestCheckoutMethods_AsksForOnePlanAndReadsTheList(t *testing.T) {
	srv, rec := server(t, 200, map[string]any{"methods": []map[string]string{
		{"id": "card", "label": "Carte bancaire"},
		{"id": "bank_transfer", "label": "Virement bancaire"},
	}})
	c := New(srv.URL, "k")

	got, err := c.CheckoutMethods(ctx(), "plan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.method != http.MethodGet || rec.path != "/api/v1/finance/checkout/methods?plan_id=plan-1" {
		t.Fatalf("%s %s", rec.method, rec.path)
	}
	// Order is Lungor's: it is the order to offer them in.
	if len(got) != 2 || got[0].ID != "card" || got[1].ID != "bank_transfer" {
		t.Fatalf("methods = %+v", got)
	}
	if got[0].Label != "Carte bancaire" {
		t.Fatalf("label = %q", got[0].Label)
	}
}

// A plan that charges nothing has nothing to pay with, and says so with an
// empty list rather than an error.
func TestCheckoutMethods_EmptyListIsNotAnError(t *testing.T) {
	srv, _ := server(t, 200, map[string]any{"methods": []map[string]string{}})
	c := New(srv.URL, "k")

	got, err := c.CheckoutMethods(ctx(), "free-plan")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("methods = %+v, want none", got)
	}
}

// A method with no id could be selected and then sent back as an empty
// payment_method, which Lungor reads as "no preselection" — the payer would
// pick one thing and get the provider's full list.
func TestCheckoutMethods_DropsAMethodWithNoID(t *testing.T) {
	srv, _ := server(t, 200, map[string]any{"methods": []map[string]string{
		{"id": "card", "label": "Carte bancaire"},
		{"label": "Sans identifiant"},
	}})
	c := New(srv.URL, "k")

	got, err := c.CheckoutMethods(ctx(), "plan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].ID != "card" {
		t.Fatalf("methods = %+v, want card only", got)
	}
}

func TestCheckoutMethods_RefusesAnEmptyPlanID(t *testing.T) {
	srv, _ := server(t, 200, nil)
	c := New(srv.URL, "k")

	if _, err := c.CheckoutMethods(ctx(), ""); !errors.Is(err, ErrBadRequest) {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
}

func TestCheckoutMethods_RefusesAnUnconfiguredClient(t *testing.T) {
	if _, err := New("", "").CheckoutMethods(ctx(), "plan-1"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}

// The preselection must reach the wire, or the payer's choice is silently lost
// and the provider shows its own list instead.
func TestCheckout_CarriesThePreselectedMethod(t *testing.T) {
	srv, rec := server(t, 200, Checkout{RedirectURL: "https://pay/1", SessionID: "s1"})
	c := New(srv.URL, "k")

	if _, err := c.Checkout(ctx(), CheckoutInput{
		PriceID: "price_pro", ExternalUserID: "user-1",
		SuccessURL: "https://app/ok", PaymentMethod: "bank_transfer",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.body["payment_method"] != "bank_transfer" {
		t.Fatalf("payment_method = %v", rec.body["payment_method"])
	}
}

// Omitted, the field must be absent rather than empty: Lungor tells the two
// apart, and an empty string would be read as a method it does not know.
func TestCheckout_OmitsTheMethodWhenUnset(t *testing.T) {
	srv, rec := server(t, 200, Checkout{RedirectURL: "https://pay/1", SessionID: "s1"})
	c := New(srv.URL, "k")

	if _, err := c.Checkout(ctx(), CheckoutInput{
		PriceID: "price_pro", ExternalUserID: "user-1", SuccessURL: "https://app/ok",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := rec.body["payment_method"]; present {
		t.Fatalf("payment_method must be absent, got %v", rec.body["payment_method"])
	}
}
