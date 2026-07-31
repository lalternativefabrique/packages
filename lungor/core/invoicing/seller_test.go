package invoicing

import "testing"

func franchiseSeller() Seller {
	return Seller{
		Name:    "L'Alternative",
		Country: "FR",
		Regime:  RegimeFranchise,
		SIREN:   "123456789",
		Address: []string{"1 rue de la Paix", "75002 Paris"},
	}
}

// The regime, not the country, decides the rate. Lungor's tax package keys on
// the merchant country alone, which would bill 20% for this seller — VAT they
// are not registered to collect.
func TestFranchiseChargesNoVATRegardlessOfBuyer(t *testing.T) {
	s := franchiseSeller()
	for _, buyer := range []string{"FR", "DE", "BE", "US", ""} {
		if got := s.Rate(buyer); got != 0 {
			t.Errorf("Rate(%q) = %d, want 0 under franchise", buyer, got)
		}
	}
}

// A zero-VAT invoice is only regular if it states why.
func TestFranchiseCarriesLegalMention(t *testing.T) {
	if got := franchiseSeller().LegalMention(); got != LegalMentionFranchise {
		t.Errorf("LegalMention() = %q, want %q", got, LegalMentionFranchise)
	}
	standard := Seller{Regime: RegimeStandard, Country: "FR"}
	if got := standard.LegalMention(); got != "" {
		t.Errorf("VAT-registered seller emits mention %q, want none", got)
	}
}

func TestStandardRegimeUsesCountryRate(t *testing.T) {
	s := Seller{Regime: RegimeStandard, Country: "FR"}
	if got := s.Rate("FR"); got != 2000 {
		t.Errorf("Rate = %d, want 2000", got)
	}
}

// Validate is the guard that stops a non-compliant document from being issued
// at all — a failure that would otherwise only surface at an audit.
func TestValidateRejectsIncompleteSeller(t *testing.T) {
	cases := map[string]func(*Seller){
		"no SIREN":    func(s *Seller) { s.SIREN = "" },
		"no address":  func(s *Seller) { s.Address = nil },
		"no name":     func(s *Seller) { s.Name = "" },
		"blank SIREN": func(s *Seller) { s.SIREN = "   " },
	}
	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			s := franchiseSeller()
			break_(&s)
			if err := s.Validate(); err == nil {
				t.Error("Validate() accepted an incomplete seller")
			}
		})
	}

	if err := franchiseSeller().Validate(); err != nil {
		t.Errorf("Validate() rejected a complete seller: %v", err)
	}
}

// A seller who charges VAT must have a number to charge it under; one who does
// not charge VAT must not be required to have one.
func TestValidateVATNumberRequiredOnlyWhenCharging(t *testing.T) {
	standard := franchiseSeller()
	standard.Regime = RegimeStandard
	if err := standard.Validate(); err == nil {
		t.Error("VAT-registered seller accepted without a VAT number")
	}

	standard.VATID = "FR12345678901"
	if err := standard.Validate(); err != nil {
		t.Errorf("complete VAT-registered seller rejected: %v", err)
	}

	// Franchise: no VAT number, and that is correct, not incomplete.
	if err := franchiseSeller().Validate(); err != nil {
		t.Errorf("franchise seller rejected for having no VAT number: %v", err)
	}
}
