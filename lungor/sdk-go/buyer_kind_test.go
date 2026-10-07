package sdk

import (
	"errors"
	"net/http"
	"testing"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

func TestCheckout_CarriesTheBuyerKind(t *testing.T) {
	srv, rec := server(t, 200, Checkout{RedirectURL: "https://pay/1", SessionID: "s1"})
	c := New(srv.URL, "k")

	if _, err := c.Checkout(ctx(), CheckoutInput{
		PriceID: "price_pro", ExternalUserID: "user-1",
		SuccessURL: "https://app/ok", BuyerKind: BuyerBusiness,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.body["buyer_kind"] != "business" {
		t.Fatalf("buyer_kind = %v", rec.body["buyer_kind"])
	}
}

// Omitted, Lungor reads the buyer as a consumer; an empty string would be an
// unknown kind and a 400.
func TestCheckout_OmitsTheBuyerKindWhenUnset(t *testing.T) {
	srv, rec := server(t, 200, Checkout{RedirectURL: "https://pay/1", SessionID: "s1"})
	c := New(srv.URL, "k")

	if _, err := c.Checkout(ctx(), CheckoutInput{
		PriceID: "price_pro", ExternalUserID: "user-1", SuccessURL: "https://app/ok",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := rec.body["buyer_kind"]; present {
		t.Fatalf("buyer_kind must be absent, got %v", rec.body["buyer_kind"])
	}
}

func TestCheckout_BusinessRefusalIsItsOwnError(t *testing.T) {
	srv, _ := server(t, http.StatusUnprocessableEntity, map[string]string{
		"error": "business_buyers_not_accepted", "message": "this offer is for consumers only",
	})
	c := New(srv.URL, "k")

	_, err := c.Checkout(ctx(), CheckoutInput{
		PriceID: "price_pro", ExternalUserID: "user-1", BuyerKind: BuyerBusiness,
	})
	if !errors.Is(err, ErrBusinessBuyersNotAccepted) {
		t.Fatalf("err = %v, want ErrBusinessBuyersNotAccepted", err)
	}
	if !errors.Is(err, ErrUnprocessable) {
		t.Fatal("a refused buyer kind is still a 422 to a caller branching on the broad error")
	}
}

// Another 422 on the same route — the consent a consumer must give under a
// waiver policy — keeps its generic mapping.
func TestCheckout_OtherUnprocessableAnswersStayGeneric(t *testing.T) {
	srv, _ := server(t, http.StatusUnprocessableEntity, map[string]string{
		"error": "consent_required", "message": "withdrawal_waiver consent is required",
	})
	c := New(srv.URL, "k")

	_, err := c.Checkout(ctx(), CheckoutInput{PriceID: "price_pro", ExternalUserID: "user-1"})
	if !errors.Is(err, ErrUnprocessable) || errors.Is(err, ErrBusinessBuyersNotAccepted) {
		t.Fatalf("err = %v, want plain ErrUnprocessable", err)
	}
}

func TestCheckoutSession_ReadsTheBuyerKind(t *testing.T) {
	srv, _ := server(t, 200, map[string]any{
		"session_id": "sess-1", "status": "completed", "paid": true, "buyer_kind": "business",
	})
	c := New(srv.URL, "k")

	got, err := c.CheckoutSession(ctx(), "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BuyerKind != BuyerBusiness {
		t.Fatalf("buyer_kind = %q", got.BuyerKind)
	}
}

func TestCheckoutSessionFrom_NoBuyerKindIsEmpty(t *testing.T) {
	if got := checkoutSessionFrom(wire.FinanceCheckoutSessionResponse{}); got.BuyerKind != "" {
		t.Fatalf("buyer_kind = %q, want empty", got.BuyerKind)
	}
}
