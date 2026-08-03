package sdk

import (
	"errors"
	"net/http"
	"testing"
)

const catalogueBody = `{"plans":[
	{"id":"p-starter","code":"starter","name":"Starter","amount":900,"currency":"EUR","interval":"month","interval_count":1,"rank":10},
	{"id":"p-pro","code":"pro","name":"Pro","amount":2900,"currency":"EUR","interval":"month","interval_count":1,"rank":100}
]}`

func TestListPlans_ReadsTheCatalogue(t *testing.T) {
	srv, rec := server(t, http.StatusOK, nil)
	defer srv.Close()
	srv.Config.Handler = jsonHandler(catalogueBody, rec)

	plans, err := New(srv.URL, "key").ListPlans(ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("got %d plans, want 2", len(plans))
	}
	pro, ok := plans.ByCode("pro")
	if !ok {
		t.Fatal("catalogue has no pro plan")
	}
	// The id is what a checkout is opened against. Losing it here is what sent
	// every consumer back to carrying the id in its own configuration.
	if pro.ID != "p-pro" {
		t.Fatalf("pro id = %q, want p-pro", pro.ID)
	}
	if pro.Amount != 2900 || pro.Currency != "EUR" {
		t.Fatalf("pro price = %d %s, want 2900 EUR", pro.Amount, pro.Currency)
	}
}

// The public route carries no credential — a pricing page is read before
// anyone signs up.
func TestListPublicPlans_NeedsNoKey(t *testing.T) {
	srv, rec := server(t, http.StatusOK, nil)
	defer srv.Close()
	srv.Config.Handler = jsonHandler(catalogueBody, rec)

	plans, err := New(srv.URL, "").ListPublicPlans(ctx(), "app-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("got %d plans, want 2", len(plans))
	}
}

func TestListPublicPlans_RefusesAnEmptyAppID(t *testing.T) {
	_, err := New("https://lungor.test", "").ListPublicPlans(ctx(), "")
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("got %v, want ErrBadRequest", err)
	}
}

func TestListPlans_UnconfiguredIsNotAnEmptyCatalogue(t *testing.T) {
	_, err := New("", "").ListPlans(ctx())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("got %v, want ErrNotConfigured", err)
	}
}

// An app with nothing on sale answers an empty catalogue, not a nil one: a
// caller ranging over the result must not have to nil-check first.
func TestListPlans_EmptyCatalogueIsUsable(t *testing.T) {
	srv, rec := server(t, http.StatusOK, nil)
	defer srv.Close()
	srv.Config.Handler = jsonHandler(`{"plans":[]}`, rec)

	plans, err := New(srv.URL, "key").ListPlans(ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if plans == nil {
		t.Fatal("nil catalogue: ranging over it should be safe")
	}
	if _, ok := plans.ByCode("pro"); ok {
		t.Fatal("found a plan in an empty catalogue")
	}
}

// A rejected key is an operator mistake, never "this app sells nothing":
// reading it as an empty catalogue would take every plan off sale at once.
func TestListPlans_UnauthorizedIsNotAnEmptyCatalogue(t *testing.T) {
	srv, _ := server(t, http.StatusUnauthorized, map[string]string{"error": "bad key"})
	defer srv.Close()

	plans, err := New(srv.URL, "key").ListPlans(ctx())
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("got %v, want ErrUnauthorized", err)
	}
	if len(plans) != 0 {
		t.Fatal("returned plans alongside an error")
	}
}

func jsonHandler(body string, rec *recorded) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.RequestURI()
		rec.method = r.Method
		rec.auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
}
