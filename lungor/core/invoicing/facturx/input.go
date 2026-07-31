// Package facturx generates Factur-X (EN 16931) invoices: the EN 16931 CII
// (Cross Industry Invoice) XML and the PDF/A-3 container that embeds it.
//
// It is deliberately DECOUPLED from any application's invoice model: callers
// build a neutral [Input] from their own domain types and pass it in. This is a
// port of the Sceauz invoicing/facturx package (ADR 0002 / 0003 there), made
// self-contained so it can be grafted into any billing context — here, Lungor's
// finance context maps its Invoice + Tenant + Customer to an Input.
//
// All monetary amounts are integer MINOR units (cents). VAT rates are basis
// points (2000 = 20.00%). The package owns the CII structs and the PDF/A-3
// assembly with no third-party invoice dependency; conformance is built to spec
// and PROVEN by the veraPDF CI gate, not by this code.
package facturx

import "errors"

// ErrIncomplete is returned when an Input is missing the data a legal invoice
// document requires (a number and an issue date).
var ErrIncomplete = errors.New("facturx: input must have a number and issue date")

// Input is the neutral contract the generator consumes. Callers map their own
// invoice/seller/buyer model onto it. Only fields populated here appear in the
// output; everything is optional except Number and IssueDate (enforced).
type Input struct {
	Number    string // legal invoice number (BT-1)
	Currency  string // ISO 4217 code, e.g. "EUR"
	IssueDate string // YYYYMMDD (format 102); set => document is issuable
	Seller    Party
	Buyer     Party
	Lines     []Line

	// LegalMention is a statutory note printed on the document and carried in
	// the CII as an included note (BT-22). It exists because an invoice showing
	// no VAT is only regular when it states WHY: a French seller under
	// franchise en base must print "TVA non applicable, art. 293 B du CGI".
	// Empty for a VAT-registered seller, which needs no such mention.
	LegalMention string

	// PeriodStart and PeriodEnd bound the billing period (BT-73/BT-74), in
	// YYYYMMDD. Both empty for a one-off invoice.
	PeriodStart string
	PeriodEnd   string
}

// Party is one side of the trade (seller or buyer). VATID is the intra-EU VAT
// number (the EN 16931 / European-standard identifier — preferred over a
// country-local SIREN). Empty fields are omitted from the output.
type Party struct {
	Name  string
	VATID string // intra-EU VAT number (e.g. "FR12345678901")
	Email string

	// LegalID is a country-local registration number (French SIREN/SIRET). It
	// is REQUIRED on a French invoice, and it is the only identifier a seller
	// under franchise en base has: they hold no VAT number precisely because
	// they collect no VAT, so VATID alone cannot identify them.
	LegalID string
	// LegalIDScheme is the ISO 6523 scheme of LegalID ("0002" = SIREN,
	// "0009" = SIRET). Defaults to SIREN when LegalID is set and this is empty.
	LegalIDScheme string

	// Address is the registered office, one element per line. Mandatory on a
	// French invoice for both parties.
	Address []string
	// Country is the ISO 3166-1 alpha-2 code of the party's address (BT-40 /
	// BT-55) — the one address element EN 16931 makes mandatory.
	Country string
}

// Line is a single billed line. UnitAmount and the derived totals are in minor
// units; VATRate is basis points (2000 = 20%).
type Line struct {
	Description string
	Quantity    int64
	UnitAmount  int64 // unit price, minor units, VAT-exclusive
	VATRate     int32 // basis points

	// VATAmount, when non-nil, is the authoritative VAT for this line in minor
	// units and overrides the rate-based computation. Use it when the VAT was
	// already decided upstream (e.g. a tax-inclusive PSP charge split where tax
	// is the remainder) so the document reproduces the persisted amounts to the
	// cent instead of re-deriving — which could drift by ±1 cent on rounding.
	VATAmount *int64
}

// lineTotal is the VAT-exclusive line total (quantity * unit price).
func (l Line) lineTotal() int64 { return l.Quantity * l.UnitAmount }

// lineVAT is the line's VAT: the explicit override when set, else derived from
// the VAT-exclusive total and rate.
func (l Line) lineVAT() int64 {
	if l.VATAmount != nil {
		return *l.VATAmount
	}
	return l.lineTotal() * int64(l.VATRate) / 10000
}

// totalExclVAT sums the VAT-exclusive line totals (BT-106).
func (in Input) totalExclVAT() int64 {
	var t int64
	for _, l := range in.Lines {
		t += l.lineTotal()
	}
	return t
}

// totalVAT sums the per-line VAT amounts (BT-110).
func (in Input) totalVAT() int64 {
	var t int64
	for _, l := range in.Lines {
		t += l.lineVAT()
	}
	return t
}

// totalInclVAT is the grand total payable (BT-112 / BT-115).
func (in Input) totalInclVAT() int64 { return in.totalExclVAT() + in.totalVAT() }

// issuable reports whether the Input carries the minimum for a legal document.
func (in Input) issuable() bool { return in.Number != "" && in.IssueDate != "" }
