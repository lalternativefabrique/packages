package facturx

import (
	"strings"
	"testing"
)

// franchiseInput is a seller under franchise en base: no VAT number, a SIREN as
// sole identifier, and every line at a zero rate.
func franchiseInput() Input {
	return Input{
		Number:    "2026-0001",
		Currency:  "EUR",
		IssueDate: "20260729",
		Seller: Party{
			Name:    "L'Alternative",
			LegalID: "123456789",
			Address: []string{"1 rue de la Paix", "75002 Paris"},
			Country: "FR",
			Email:   "contact@lalternative.fr",
		},
		Buyer: Party{
			Name:    "Client Test",
			Email:   "client@example.com",
			Country: "FR",
		},
		LegalMention: "TVA non applicable, art. 293 B du CGI",
		PeriodStart:  "20260701",
		PeriodEnd:    "20260731",
		Lines: []Line{
			{Description: "Abonnement Techtuel Pro", Quantity: 1, UnitAmount: 1900, VATRate: 0},
		},
	}
}

// A zero-rated line must be declared EXEMPT, not standard-rated at 0%. Getting
// this wrong produces arithmetically-correct but semantically invalid CII that
// an e-invoicing platform rejects.
func TestFranchiseUsesExemptCategory(t *testing.T) {
	doc, err := GenerateCII(franchiseInput())
	if err != nil {
		t.Fatalf("GenerateCII: %v", err)
	}

	line := doc.Transaction.Lines[0].Settlement.Tax
	if line.CategoryCode != categoryExempt {
		t.Errorf("line category = %q, want %q", line.CategoryCode, categoryExempt)
	}

	bd := doc.Transaction.Settlement.TaxBreakdown
	if len(bd) != 1 {
		t.Fatalf("breakdown has %d entries, want 1", len(bd))
	}
	if bd[0].CategoryCode != categoryExempt {
		t.Errorf("breakdown category = %q, want %q", bd[0].CategoryCode, categoryExempt)
	}
	// BR-E-10: an exempt breakdown line must say why.
	if bd[0].ExemptionReason == "" {
		t.Error("exempt breakdown carries no exemption reason")
	}
}

// With no VAT number, the SIREN is the only thing identifying the seller. If it
// were dropped the invoice would name an unidentifiable issuer.
func TestFranchiseSellerIdentifiedBySIREN(t *testing.T) {
	doc, err := GenerateCII(franchiseInput())
	if err != nil {
		t.Fatalf("GenerateCII: %v", err)
	}

	seller := doc.Transaction.Agreement.Seller
	if seller.LegalOrg == nil {
		t.Fatal("seller has no legal organization id")
	}
	if got := seller.LegalOrg.ID.Value; got != "123456789" {
		t.Errorf("seller legal id = %q, want SIREN 123456789", got)
	}
	if got := seller.LegalOrg.ID.SchemeID; got != schemeSIREN {
		t.Errorf("scheme = %q, want %q (SIREN)", got, schemeSIREN)
	}
	if seller.Address == nil || seller.Address.CountryID != "FR" {
		t.Error("seller address or country missing")
	}
}

// Totals must stay coherent at a zero rate: nothing is invented as tax.
func TestFranchiseTotalsCarryNoVAT(t *testing.T) {
	in := franchiseInput()
	if got := in.totalVAT(); got != 0 {
		t.Errorf("total VAT = %d, want 0 under franchise", got)
	}
	if in.totalInclVAT() != in.totalExclVAT() {
		t.Errorf("gross %d != net %d with no VAT", in.totalInclVAT(), in.totalExclVAT())
	}
}

// The mention must reach BOTH layers: the structured note a machine reads, and
// the PDF a human reads. Present in only one of them is a non-compliant invoice.
func TestFranchiseMentionInBothLayers(t *testing.T) {
	in := franchiseInput()

	doc, err := GenerateCII(in)
	if err != nil {
		t.Fatalf("GenerateCII: %v", err)
	}
	if len(doc.Document.Notes) == 0 ||
		!strings.Contains(doc.Document.Notes[0].Content, "293 B") {
		t.Error("legal mention missing from CII notes")
	}

	xml, err := Marshal(doc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(xml), "293 B") {
		t.Error("legal mention missing from marshalled XML")
	}

	// The full PDF/A-3 container, not just the layout: this is what a customer
	// downloads, and the mention has to survive the embedding.
	pdf, err := GenerateFacturX(in)
	if err != nil {
		t.Fatalf("GenerateFacturX: %v", err)
	}
	if len(pdf) == 0 {
		t.Error("empty PDF")
	}
}

// The billing period tells the reader which month is being charged.
func TestBillingPeriodCarried(t *testing.T) {
	doc, err := GenerateCII(franchiseInput())
	if err != nil {
		t.Fatalf("GenerateCII: %v", err)
	}
	p := doc.Transaction.Settlement.Period
	if p == nil {
		t.Fatal("billing period missing")
	}
	if p.Start.Value != "20260701" || p.End.Value != "20260731" {
		t.Errorf("period = %s..%s, want 20260701..20260731", p.Start.Value, p.End.Value)
	}
}

// A half-open period is not a period; CII requires both bounds.
func TestBillingPeriodOmittedWhenIncomplete(t *testing.T) {
	in := franchiseInput()
	in.PeriodEnd = ""
	doc, err := GenerateCII(in)
	if err != nil {
		t.Fatalf("GenerateCII: %v", err)
	}
	if doc.Transaction.Settlement.Period != nil {
		t.Error("period emitted with only one bound set")
	}
}

// A VAT-registered seller must keep the standard category — the exempt path
// must not leak into the normal case.
func TestStandardRateStaysStandard(t *testing.T) {
	in := franchiseInput()
	in.Seller.VATID = "FR12345678901"
	in.LegalMention = ""
	in.Lines[0].VATRate = 2000

	doc, err := GenerateCII(in)
	if err != nil {
		t.Fatalf("GenerateCII: %v", err)
	}
	if got := doc.Transaction.Lines[0].Settlement.Tax.CategoryCode; got != categoryStandard {
		t.Errorf("category = %q, want %q", got, categoryStandard)
	}
	bd := doc.Transaction.Settlement.TaxBreakdown[0]
	if bd.ExemptionReason != "" {
		t.Errorf("standard-rated line carries exemption reason %q", bd.ExemptionReason)
	}
	// The VAT number must win over the SIREN when both are present.
	if doc.Transaction.Agreement.Seller.LegalOrg.ID.SchemeID != schemeVAT {
		t.Error("VAT-registered seller not identified by VAT scheme")
	}
}

// A description is caller-supplied text. fpdf's SplitText indexes a 256-entry
// width table BY RUNE, so any character above U+00FF panicked the process that
// issues invoices — an em dash in a product label was enough. Found in
// production the moment the invoice line was given a typographic separator.
func TestTypographicCharactersDoNotPanic(t *testing.T) {
	for _, desc := range []string{
		"Techtuel Pro — juillet 2026", // em dash: the actual crash
		"Abonnement « Pro »",          // guillemets
		"L’Alternative",               // curly apostrophe
		"Forfait 5€/mois",             // euro sign
		"Pack 🎧 audio",                // emoji, outside any fold
		"Offre… complète",             // ellipsis
	} {
		in := franchiseInput()
		in.Lines[0].Description = desc

		// Must not panic, and must still produce a document.
		pdf, err := GenerateFacturX(in)
		if err != nil {
			t.Errorf("GenerateFacturX(%q): %v", desc, err)
			continue
		}
		if len(pdf) == 0 {
			t.Errorf("GenerateFacturX(%q): empty PDF", desc)
		}
	}
}

// The fold must keep text legible rather than mangling everything to '?'.
func TestCP1252FoldKeepsTextReadable(t *testing.T) {
	cases := map[string]string{
		"Pro — juillet": "Pro - juillet",
		"L’Alternative": "L'Alternative",
		"5€":            "5EUR",
		// Already inside cp1252 (171/187) — kept verbatim, not folded. Same for
		// accents, which is the whole reason the fold is rune-range based
		// rather than an allow-list of ASCII.
		"« Pro »":       "« Pro »",
		"déjà accentué": "déjà accentué",
		"🎧":             "?", // no sensible equivalent
	}
	for in, want := range cases {
		if got := toCP1252(in); got != want {
			t.Errorf("toCP1252(%q) = %q, want %q", in, got, want)
		}
	}
}
