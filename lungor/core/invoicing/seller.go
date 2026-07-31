package invoicing

import "strings"

// TaxRegime is the SELLER's VAT regime. It is a property of the seller, not of
// a country: Lungor's tax package derives the rate from the merchant country
// alone, which silently bills 20% for a French seller under franchise en base —
// VAT they are not allowed to collect. The regime is therefore explicit here.
type TaxRegime string

const (
	// RegimeFranchise is the French "franchise en base de TVA" (CGI art. 293 B):
	// the seller is under the turnover threshold, charges NO VAT, and the
	// invoice must carry the exemption mention instead of a VAT breakdown.
	RegimeFranchise TaxRegime = "franchise"

	// RegimeStandard is a VAT-registered seller charging the standard rate.
	// Reserved for the day the franchise threshold is crossed; the rate then
	// comes from the seller country (and, for EU B2C, OSS).
	RegimeStandard TaxRegime = "standard"
)

// LegalMentionFranchise is the exact wording French law requires on an invoice
// issued under the franchise en base regime. It is not decorative: an invoice
// showing no VAT without it is irregular.
const LegalMentionFranchise = "TVA non applicable, art. 293 B du CGI"

// Seller is the legal entity issuing the invoices — the Lungor "tenant". Its
// identity is fixed configuration, not per-request data.
type Seller struct {
	// TenantID and AppID are the Lungor scoping ids. They are constants here
	// (one company, one product) and become real ids at swap time.
	TenantID string
	AppID    string

	Name    string
	Country string // ISO 3166-1 alpha-2
	Regime  TaxRegime

	// VATID is the intra-EU VAT number. Empty under franchise en base — the
	// seller has no VAT number precisely because they collect no VAT.
	VATID string

	// SIREN is the 9-digit French company identifier. Mandatory on a French
	// invoice; there is no valid document without it.
	SIREN string

	// Address is the registered office, one line per element. Mandatory.
	Address []string

	Email string
}

// Rate returns the VAT rate to apply, in basis points (2000 = 20%). Under
// franchise en base it is always zero regardless of the buyer's country: the
// seller is outside the VAT system entirely, so neither OSS nor reverse-charge
// applies.
func (s Seller) Rate(buyerCountry string) int32 {
	if s.Regime == RegimeFranchise {
		return 0
	}
	return standardRateBp(s.Country)
}

// LegalMention returns the statutory mention to print on the invoice, or "" if
// the regime requires none.
func (s Seller) LegalMention() string {
	if s.Regime == RegimeFranchise {
		return LegalMentionFranchise
	}
	return ""
}

// Validate reports whether the seller carries everything a legally valid French
// invoice requires. Issuing is refused when it does not, because a missing
// SIREN or address produces a document that looks fine and is not compliant —
// a failure that would otherwise only surface at an audit, long after the
// invoices were sent.
func (s Seller) Validate() error {
	if strings.TrimSpace(s.Name) == "" ||
		strings.TrimSpace(s.SIREN) == "" ||
		len(s.Address) == 0 {
		return ErrSellerIncomplete
	}
	// A standard-regime seller charges VAT and must therefore have a VAT number.
	if s.Regime == RegimeStandard && strings.TrimSpace(s.VATID) == "" {
		return ErrSellerIncomplete
	}
	return nil
}

// standardRateBp is the standard VAT rate by country, in basis points. Only
// populated for the countries this seller could plausibly be established in;
// unknown country yields 0 rather than a guess.
func standardRateBp(country string) int32 {
	switch strings.ToUpper(strings.TrimSpace(country)) {
	case "FR":
		return 2000
	case "BE":
		return 2100
	case "DE":
		return 1900
	case "ES":
		return 2100
	case "IT":
		return 2200
	case "LU":
		return 1700
	}
	return 0
}
