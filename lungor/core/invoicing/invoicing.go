// Package invoicing is the CONTRACT for issuing and reading invoices. It is
// deliberately shaped as if it were the remote Lungor billing API, because that
// is what it will become: today [Client] is served in-process by the local
// implementation, and the day billing moves to Lungor the same interface is
// served by an HTTP client against `/tenants/{tenant_id}/invoices`. Callers are
// written once, against this package, and never change.
//
// That is why the types below mirror Lungor's transport DTOs field-for-field
// and json-tag-for-json-tag (see finance/billing_admin_api.go there) rather
// than being modelled on what Techtuel happens to need today. A field Techtuel
// does not use yet (Livemode, AppID, SubscriptionID) is still carried, because
// dropping it here would make the eventual swap a migration instead of a
// one-line change of implementation.
//
// Money is ALWAYS integer minor units (cents). Never float.
package invoicing

import (
	"context"
	"errors"
)

// ErrInvoiceNotFound is returned when no invoice matches the lookup. It mirrors
// a 404 from the remote API.
var ErrInvoiceNotFound = errors.New("invoicing: invoice not found")

// ErrSellerIncomplete is returned when the configured seller identity lacks
// something a legally valid invoice requires (SIREN, address). Issuing is
// refused rather than producing a non-compliant document.
var ErrSellerIncomplete = errors.New("invoicing: seller identity incomplete")

// Status is the invoice lifecycle state. Values match Lungor's CHECK
// constraint on invoices.status.
type Status string

const (
	StatusDraft         Status = "draft"
	StatusOpen          Status = "open"
	StatusPaid          Status = "paid"
	StatusVoid          Status = "void"
	StatusUncollectible Status = "uncollectible"
)

// LineKind distinguishes the flat subscription fee from usage-based lines
// computed from the metering ledger at period close.
type LineKind string

const (
	LineFlat  LineKind = "flat"
	LineUsage LineKind = "usage"
)

// ListItem is one row of the invoices list. Shape mirrors Lungor's
// invoiceListItemView exactly, including json tags.
type ListItem struct {
	ID            string  `json:"id"`
	Number        string  `json:"number"`
	CustomerName  string  `json:"customer_name"`
	CustomerEmail string  `json:"customer_email"`
	Amount        int64   `json:"amount"`
	Currency      string  `json:"currency"`
	Status        string  `json:"status"`
	IssuedAt      *string `json:"issued_at,omitempty"` // RFC3339
}

// Line is a single billed item on an invoice.
type Line struct {
	ID          string   `json:"id"`
	Kind        LineKind `json:"kind"`
	Description string   `json:"description"`
	Unit        string   `json:"unit,omitempty"`
	Quantity    int64    `json:"quantity"`
	UnitAmount  int64    `json:"unit_amount"`
	Amount      int64    `json:"amount"`
}

// Invoice is the full invoice document as returned by GetInvoice.
type Invoice struct {
	ID          string  `json:"id"`
	Number      string  `json:"number"`
	Status      Status  `json:"status"`
	Subtotal    int64   `json:"subtotal"`
	TaxAmount   int64   `json:"tax_amount"`
	Total       int64   `json:"total"`
	Currency    string  `json:"currency"`
	TaxCountry  string  `json:"tax_country,omitempty"`
	TaxRate     float64 `json:"tax_rate"`
	PeriodStart *string `json:"period_start,omitempty"`
	PeriodEnd   *string `json:"period_end,omitempty"`
	DueAt       *string `json:"due_at,omitempty"`
	PaidAt      *string `json:"paid_at,omitempty"`
	// IssuedAt is the legal issue date — the date the document bears and the
	// one the numbering sequence is chronological in. Distinct from CreatedAt,
	// which is merely when the row was written.
	IssuedAt       *string `json:"issued_at,omitempty"`
	CustomerName   string  `json:"customer_name"`
	CustomerEmail  string  `json:"customer_email"`
	Lines          []Line  `json:"lines"`
	PDFURL         string  `json:"pdf_url,omitempty"`
	SubscriptionID *string `json:"subscription_id,omitempty"`
	// Livemode is false for invoices issued while the app is in test mode.
	// Carried for parity with Lungor even though Techtuel has no test mode yet.
	Livemode  bool   `json:"livemode"`
	CreatedAt string `json:"created_at"`
}

// ListParams filters an invoice listing. AppID and Livemode exist to match
// Lungor's query parameters; the local implementation applies them the same way
// so behaviour does not change at swap time.
type ListParams struct {
	// CustomerID scopes the listing to one end customer. In Techtuel this is
	// the account owner_id. Required: a customer must never see another's
	// invoices, so an empty value lists nothing rather than everything.
	CustomerID string
	AppID      string
	Livemode   bool
	Limit      int
}

// ClosePeriodInput issues the invoice for one billing period.
type ClosePeriodInput struct {
	CustomerID    string
	CustomerName  string
	CustomerEmail string
	// CustomerCountry drives VAT for a VAT-registered seller. Under franchise
	// en base it is recorded but does not change the rate (always zero).
	CustomerCountry string
	// CustomerVATID is the buyer's intra-EU VAT number, when they have one.
	CustomerVATID  string
	SubscriptionID string
	Currency       string
	PeriodStart    string // RFC3339
	PeriodEnd      string // RFC3339
	Lines          []ClosePeriodLine
}

// ClosePeriodLine is one line to bill. Amounts are VAT-exclusive minor units.
type ClosePeriodLine struct {
	Kind        LineKind
	Description string
	Unit        string
	Quantity    int64
	UnitAmount  int64
}

// Client is the invoicing port. Every method corresponds to one endpoint of the
// Lungor billing API; an HTTP implementation and the in-process one are
// interchangeable behind it.
type Client interface {
	// ListInvoices returns invoices matching params, newest first.
	ListInvoices(ctx context.Context, params ListParams) ([]ListItem, error)

	// GetInvoice returns one invoice with its lines. It returns
	// ErrInvoiceNotFound when id is unknown OR belongs to another customer —
	// the two are deliberately indistinguishable so the endpoint cannot be used
	// to probe for the existence of other customers' invoices.
	GetInvoice(ctx context.Context, customerID, id string) (Invoice, error)

	// GetInvoicePDF returns the Factur-X PDF bytes for an invoice. Same
	// not-found semantics as GetInvoice.
	GetInvoicePDF(ctx context.Context, customerID, id string) ([]byte, error)

	// ClosePeriod issues an invoice for a billing period and returns it. It is
	// idempotent per (customer, period): calling it twice returns the existing
	// invoice rather than issuing a second one, because an invoice number is
	// legally irrevocable once allocated.
	ClosePeriod(ctx context.Context, in ClosePeriodInput) (Invoice, error)
}
