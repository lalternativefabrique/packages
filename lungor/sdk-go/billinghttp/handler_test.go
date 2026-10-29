package billinghttp_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/lalternative/packages/lungor/sdk-go"
	"github.com/lalternative/packages/lungor/sdk-go/billinghttp"
)

type lungorCall struct {
	path string
	body map[string]any
}

func setup(t *testing.T, status int, reply string) (http.Handler, *[]lungorCall) {
	h, calls, _ := setupWatching(t, status, reply)
	return h, calls
}

func setupWatching(t *testing.T, status int, reply string) (http.Handler, *[]lungorCall, *[]string) {
	t.Helper()
	var calls []lungorCall
	var changed []string
	lungor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, lungorCall{path: r.URL.Path + "?" + r.URL.RawQuery, body: body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(lungor.Close)
	h := billinghttp.New(billinghttp.Config{
		Client: sdk.New(lungor.URL, "k"),
		UserID: func(r *http.Request) (string, bool) {
			id := r.Header.Get("X-User")
			return id, id != ""
		},
		ReturnURL: "https://app/billing",
		OnChanged: func(_ *http.Request, userID string) { changed = append(changed, userID) },
	})
	return h, &calls, &changed
}

func do(h http.Handler, method, path, body, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != "" {
		req.Header.Set("X-User", user)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAnUnauthenticatedCallReachesNothing(t *testing.T) {
	h, calls := setup(t, 200, `{}`)
	if rec := do(h, http.MethodPost, "/cancel", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if len(*calls) != 0 {
		t.Fatalf("Lungor was called %d times", len(*calls))
	}
}

func TestChangePlanQuoteCarriesTheFiguresAndTheCard(t *testing.T) {
	h, calls := setup(t, http.StatusPaymentRequired,
		`{"kind":"up","consent_required":true,"consent_amount_cents":579,"consent_recurring_cents":1200,
		  "payment_method":{"kind":"card","brand":"visa","last4":"3201"}}`)

	rec := do(h, http.MethodPost, "/change-plan", `{"code":"max"}`, "u1")

	var got billinghttp.ChangePlanResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || !got.ConsentRequired || got.ConsentAmountCents != 579 || got.ConsentRecurringCents != 1200 {
		t.Fatalf("status %d, quote %+v", rec.Code, got)
	}
	if got.Card == nil || got.Card.Brand != "visa" || got.Card.Last4 != "3201" {
		t.Fatalf("card = %+v", got.Card)
	}
	if c := (*calls)[0]; c.body["external_user_id"] != "u1" || c.body["plan_code"] != "max" || c.body["agreed"] != nil {
		t.Fatalf("relayed %v, want u1 → max without agreement", c.body)
	}
}

func TestChangePlanRelaysTheAgreedAmount(t *testing.T) {
	h, calls := setup(t, 200, `{"kind":"up","applied_now":true,"prorated_cents":579}`)

	do(h, http.MethodPost, "/change-plan", `{"code":"max","agreed":true,"agreed_amount_cents":579}`, "u1")

	if c := (*calls)[0]; c.body["agreed"] != true || c.body["agreed_amount_cents"] != float64(579) {
		t.Fatalf("relayed %v, want the agreement to 579", c.body)
	}
}

func TestCancelStopsAtThePeriodEndNeverNow(t *testing.T) {
	h, calls := setup(t, 200, `{"status":"active","effective_at":"2026-11-23T00:00:00Z"}`)

	rec := do(h, http.MethodPost, "/cancel", "", "u1")

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "2026-11-23") {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	if c := (*calls)[0]; c.body["at_period_end"] != true {
		t.Fatalf("relayed %v, want at_period_end true", c.body)
	}
}

func TestReplaceCardSendsTheUserBackToTheBillingPage(t *testing.T) {
	h, calls := setup(t, 200, `{"redirect_url":"https://mollie/card"}`)

	rec := do(h, http.MethodPost, "/payment-method", "", "u1")

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "https://mollie/card") {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	if c := (*calls)[0]; c.body["return_url"] != "https://app/billing" {
		t.Fatalf("relayed %v", c.body)
	}
}

func TestNoSubscriptionIsANotFoundNotAnOutage(t *testing.T) {
	h, _ := setup(t, http.StatusNotFound, `{"message":"no active subscription for this user"}`)
	if rec := do(h, http.MethodGet, "/payment-method", "", "u1"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAnAppliedChangeIsAnnouncedButAQuoteIsNot(t *testing.T) {
	h, _, changed := setupWatching(t, http.StatusPaymentRequired, `{"consent_required":true,"consent_amount_cents":579}`)
	do(h, http.MethodPost, "/change-plan", `{"code":"max"}`, "u1")
	if len(*changed) != 0 {
		t.Fatalf("a quote announced a change: %v", *changed)
	}

	h, _, changed = setupWatching(t, 200, `{"status":"active"}`)
	do(h, http.MethodPost, "/cancel", "", "u1")
	if len(*changed) != 1 || (*changed)[0] != "u1" {
		t.Fatalf("changed = %v, want u1 once", *changed)
	}
}

func TestARefusalIsTellableFromAnOutage(t *testing.T) {
	h, _ := setup(t, http.StatusBadRequest, `{"message":"charge proration: card declined"}`)
	rec := do(h, http.MethodPost, "/change-plan", `{"code":"max","agreed":true,"agreed_amount_cents":579}`, "u1")
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), `"refused"`) {
		t.Fatalf("status %d body %s, want 422 refused", rec.Code, rec.Body)
	}

	h, _ = setup(t, http.StatusInternalServerError, `{}`)
	rec = do(h, http.MethodPost, "/resume", "", "u1")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), `"unavailable"`) {
		t.Fatalf("status %d body %s, want 502 unavailable", rec.Code, rec.Body)
	}
}
