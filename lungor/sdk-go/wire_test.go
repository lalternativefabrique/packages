package sdk

import (
	"testing"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

// The generated types carry pointers everywhere, because swag emits OpenAPI 2.0
// and 2.0 has no `required`. Those pointers must stop at this boundary: a
// *bool for Entitled would make "not entitled" and "no answer" the same value
// at the call site, where one must degrade and the other must not.
func TestEntitlementFrom_NilVerdictIsNotEntitled(t *testing.T) {
	got := entitlementFrom(wire.FinanceEntitlementResponse{})

	if got.Entitled {
		t.Fatal("a response with no verdict must grant nothing")
	}
	if got.Status != "" || got.Balances != nil {
		t.Fatalf("zero wire type = %+v, want the zero entitlement", got)
	}
}

func TestEntitlementFrom_CopiesEveryField(t *testing.T) {
	entitled, status := true, "active"
	balances := map[string]int64{"credit": 2840}

	got := entitlementFrom(wire.FinanceEntitlementResponse{
		Entitled: &entitled, Status: &status, Balances: &balances,
	})

	if !got.Entitled || got.Status != "active" {
		t.Fatalf("entitlement = %+v", got)
	}
	if v, ok := got.Balance("credit"); !ok || v != 2840 {
		t.Fatalf("credit = (%d, %v), want (2840, true)", v, ok)
	}
}

func TestCheckoutFrom_CopiesEveryField(t *testing.T) {
	session, sub, redirect := "s1", "sub1", "https://pay.example/s/1"

	got := checkoutFrom(wire.FinanceCheckoutResponse{
		SessionId: &session, SubscriptionId: &sub, RedirectUrl: &redirect,
	})

	if got.SessionID != "s1" || got.SubscriptionID != "sub1" || got.RedirectURL != redirect {
		t.Fatalf("checkout = %+v", got)
	}
}

func TestCheckoutFrom_EmptyWireIsTheZeroCheckout(t *testing.T) {
	if got := checkoutFrom(wire.FinanceCheckoutResponse{}); got != (Checkout{}) {
		t.Fatalf("checkout = %+v, want zero", got)
	}
}
