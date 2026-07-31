package mollie

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// capturePayment stubs POST /payments and hands back the decoded request body,
// which is what these tests are actually about: the fields we send decide
// whether a mandate is created, reused, or silently opened on the wrong payment
// method.
func capturePayment(t *testing.T, respond string) (*Client, *map[string]any) {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, respond)
	}))
	t.Cleanup(srv.Close)
	return NewWithBaseURL("test_key", srv.URL), &got
}

// A first payment must establish a mandate ("first") and must pin the method to
// cards. The pin is not cosmetic: the proration charged on a later upgrade is
// only safe because a card answers synchronously.
func TestFirstPaymentEstablishesACardMandate(t *testing.T) {
	c, body := capturePayment(t, `{"id":"tr_1","_links":{"checkout":{"href":"https://pay.example/1"}}}`)

	id, url, err := c.FirstPayment(context.Background(), FirstPaymentInput{
		CustomerID:  "cst_1",
		AmountCents: 500,
		Currency:    "EUR",
		Description: "Techtuel",
	})
	if err != nil {
		t.Fatalf("FirstPayment: %v", err)
	}
	if id != "tr_1" || url != "https://pay.example/1" {
		t.Fatalf("got id=%q url=%q", id, url)
	}
	if got := (*body)["sequenceType"]; got != "first" {
		t.Fatalf("sequenceType = %v, want \"first\" — without it no mandate is created and nothing can be charged later", got)
	}
	if got := (*body)["method"]; got != "creditcard" {
		t.Fatalf("method = %v, want \"creditcard\": sending no method offers every method enabled on the "+
			"account, so an asynchronous mandate (SEPA, iDEAL) could be created and the upgrade path could "+
			"no longer tell a paid proration from a pending one", got)
	}
}

// A proration rides on the EXISTING mandate. Opening a checkout instead would
// create a second mandate on the same customer, and only the newest stays
// reachable while the older keeps charging.
func TestRecurringPaymentReusesTheMandate(t *testing.T) {
	c, body := capturePayment(t, `{"id":"tr_2","status":"paid"}`)

	id, status, err := c.RecurringPayment(context.Background(), RecurringPaymentInput{
		CustomerID:  "cst_1",
		AmountCents: 350,
		Currency:    "EUR",
		Description: "Techtuel — complément Max",
		Metadata:    map[string]string{"idempotency_key": "own_1:pro:max:2026-08-24"},
	})
	if err != nil {
		t.Fatalf("RecurringPayment: %v", err)
	}
	if id != "tr_2" || status != PaymentPaid {
		t.Fatalf("got id=%q status=%q, want tr_2/paid", id, status)
	}
	if got := (*body)["sequenceType"]; got != "recurring" {
		t.Fatalf("sequenceType = %v, want \"recurring\" — anything else opens a second mandate on a customer who already has one", got)
	}
	if _, hasRedirect := (*body)["redirectUrl"]; hasRedirect {
		t.Fatal("a recurring charge has no customer present: sending a redirect URL implies a checkout that must not exist")
	}
	if got := (*body)["amount"].(map[string]any)["value"]; got != "3.50" {
		t.Fatalf("amount = %v, want \"3.50\"", got)
	}
	// The idempotency key must survive the round-trip: it is how a replayed
	// webhook is recognised as the same logical charge rather than a second one.
	meta, _ := (*body)["metadata"].(map[string]any)
	if meta["idempotency_key"] != "own_1:pro:max:2026-08-24" {
		t.Fatalf("metadata = %v, want the idempotency key carried through", meta)
	}
}

// Anything that is not "paid" means the money did not move. The caller decides
// what to do; this only has to report it faithfully rather than swallow it.
func TestRecurringPaymentReportsUnpaidStatuses(t *testing.T) {
	for _, status := range []string{"failed", "canceled", "expired", "pending", "open"} {
		t.Run(status, func(t *testing.T) {
			c, _ := capturePayment(t, `{"id":"tr_3","status":"`+status+`"}`)
			_, got, err := c.RecurringPayment(context.Background(), RecurringPaymentInput{
				CustomerID: "cst_1", AmountCents: 350, Currency: "EUR",
			})
			if err != nil {
				t.Fatalf("a refused payment is an answer, not a transport error: %v", err)
			}
			if got == PaymentPaid {
				t.Fatalf("status %q reported as paid — entitlement would be granted for money that never arrived", status)
			}
		})
	}
}
