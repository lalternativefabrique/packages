package sdk

import (
	"errors"
	"net/http"
	"testing"

	"github.com/lalternative/packages/lungor/sdk-go/internal/wire"
)

func TestRegisterCustomer_SendsTheIdentityAndReportsTheRow(t *testing.T) {
	id, created, at := "cus-1", true, "2026-08-11T10:00:00Z"
	srv, rec := server(t, http.StatusCreated, wire.FinanceAppRegisterCustomerResponse{
		CustomerId: &id, Created: &created, CreatedAt: &at,
	})
	c := New(srv.URL, "k")

	out, err := c.RegisterCustomer(ctx(), RegisterCustomerInput{
		Email:          "bob@example.fr",
		Name:           "Bob",
		ExternalUserID: "org_42",
		Country:        "FR",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.path != "/api/v1/customers" {
		t.Fatalf("path = %q", rec.path)
	}
	if rec.body["email"] != "bob@example.fr" ||
		rec.body["name"] != "Bob" ||
		rec.body["external_user_id"] != "org_42" ||
		rec.body["country"] != "FR" {
		t.Fatalf("body = %+v", rec.body)
	}
	if out.CustomerID != "cus-1" || !out.Created {
		t.Fatalf("result = %+v", out)
	}
	if out.CreatedAt.IsZero() {
		t.Fatal("created at was not parsed")
	}
}

// Optional fields must not travel as empty strings: Lungor's upsert fills
// blanks rather than overwriting, and "" is a value, not an absence — sending
// it would erase a name the customer already had.
func TestRegisterCustomer_OmitsUnsetOptionalFields(t *testing.T) {
	srv, rec := server(t, http.StatusCreated, wire.FinanceAppRegisterCustomerResponse{})
	c := New(srv.URL, "k")

	if _, err := c.RegisterCustomer(ctx(), RegisterCustomerInput{
		Email: "bob@example.fr",
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, field := range []string{"name", "external_user_id", "country", "silent"} {
		if _, present := rec.body[field]; present {
			t.Fatalf("%s was sent for an unset field: %+v", field, rec.body)
		}
	}
}

// The flag is the whole reason a back-fill is safe to run: without it reaching
// Lungor, importing a user base announces every one of them as a new signup.
func TestRegisterCustomer_SendsSilentWhenSet(t *testing.T) {
	srv, rec := server(t, http.StatusCreated, wire.FinanceAppRegisterCustomerResponse{})
	c := New(srv.URL, "k")

	if _, err := c.RegisterCustomer(ctx(), RegisterCustomerInput{
		Email:  "bob@example.fr",
		Silent: true,
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.body["silent"] != true {
		t.Fatalf("silent = %v, want true: %+v", rec.body["silent"], rec.body)
	}
}

// A known email comes back created=false. An import re-run after a partial
// failure needs that to report what it actually added.
func TestRegisterCustomer_ReportsAKnownEmailAsNotCreated(t *testing.T) {
	id, created := "cus-1", false
	srv, _ := server(t, http.StatusCreated, wire.FinanceAppRegisterCustomerResponse{
		CustomerId: &id, Created: &created,
	})
	c := New(srv.URL, "k")

	out, err := c.RegisterCustomer(ctx(), RegisterCustomerInput{Email: "bob@example.fr"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Created {
		t.Fatal("created = true for an email Lungor already knew")
	}
}

// Caught before the request: the email is what the record is keyed on, so a
// blank one is a caller bug, not something to let Lungor refuse over the wire.
func TestRegisterCustomer_RefusesABlankEmailWithoutCalling(t *testing.T) {
	srv, rec := server(t, http.StatusCreated, wire.FinanceAppRegisterCustomerResponse{})
	c := New(srv.URL, "k")

	_, err := c.RegisterCustomer(ctx(), RegisterCustomerInput{})
	if !errors.Is(err, ErrBadRequest) {
		t.Fatalf("err = %v, want ErrBadRequest", err)
	}
	if rec.path != "" {
		t.Fatalf("a request was sent for a blank email: %q", rec.path)
	}
}

func TestRegisterCustomer_RequiresConfiguration(t *testing.T) {
	c := New("", "")
	if _, err := c.RegisterCustomer(ctx(), RegisterCustomerInput{Email: "bob@example.fr"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
}
