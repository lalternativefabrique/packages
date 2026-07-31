package facturx

import (
	"strings"
	"testing"
)

func issuedInput() Input {
	return Input{
		Number:    "INV-000042",
		Currency:  "EUR",
		IssueDate: "20270315",
		Seller:    Party{Name: "Dr Martin", VATID: "FR12345678901", Email: "m@ex.fr"},
		Buyer:     Party{Name: "ACME SARL", VATID: "FR98765432109"},
		Lines: []Line{
			{Description: "Consultation", Quantity: 2, UnitAmount: 5000, VATRate: 2000},
			{Description: "Rapport", Quantity: 1, UnitAmount: 3000, VATRate: 1000},
		},
	}
}

func TestGenerateXML_Contains(t *testing.T) {
	xmlBytes, err := GenerateXML(issuedInput())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	s := string(xmlBytes)

	wants := []string{
		`<?xml version="1.0"`,
		"rsm:CrossIndustryInvoice",
		"urn:cen.eu:en16931:2017",          // EN 16931 guideline
		"<ram:ID>INV-000042</ram:ID>",      // invoice number
		"<ram:TypeCode>380</ram:TypeCode>", // commercial invoice
		`format="102">20270315`,            // issue date YYYYMMDD
		`schemeID="9930">FR12345678901`,    // seller VAT id
		`schemeID="9930">FR98765432109`,    // buyer VAT id
		"Consultation",
		"Rapport",
		"<ram:InvoiceCurrencyCode>EUR</ram:InvoiceCurrencyCode>",
	}
	for _, w := range wants {
		if !strings.Contains(s, w) {
			t.Errorf("XML missing %q", w)
		}
	}
}

func TestGenerateXML_Totals(t *testing.T) {
	s, _ := GenerateXML(issuedInput())
	str := string(s)
	// excl VAT: 2*5000 + 1*3000 = 13000 -> 130.00
	if !strings.Contains(str, "<ram:LineTotalAmount>130.00</ram:LineTotalAmount>") {
		t.Error("missing grand line total 130.00")
	}
	// VAT: 10000*20% + 3000*10% = 2000+300 = 2300 -> 23.00
	if !strings.Contains(str, ">23.00</ram:TaxTotalAmount>") {
		t.Error("missing tax total 23.00")
	}
	// incl VAT: 15300 -> 153.00
	if !strings.Contains(str, "<ram:GrandTotalAmount>153.00</ram:GrandTotalAmount>") {
		t.Error("missing grand total 153.00")
	}
}

func TestVATBreakdown_PerRate(t *testing.T) {
	doc, err := GenerateCII(issuedInput())
	if err != nil {
		t.Fatal(err)
	}
	tb := doc.Transaction.Settlement.TaxBreakdown
	if len(tb) != 2 {
		t.Fatalf("expected 2 VAT rates, got %d", len(tb))
	}
	// sorted ascending by rate: 10% then 20%
	if tb[0].RatePercent != "10.00" || tb[1].RatePercent != "20.00" {
		t.Errorf("rates = %s,%s want 10.00,20.00", tb[0].RatePercent, tb[1].RatePercent)
	}
	if tb[0].BasisAmount.Value != "30.00" || tb[0].CalculatedAmount.Value != "3.00" {
		t.Errorf("10%% bucket wrong: basis=%s tax=%s", tb[0].BasisAmount.Value, tb[0].CalculatedAmount.Value)
	}
	if tb[1].BasisAmount.Value != "100.00" || tb[1].CalculatedAmount.Value != "20.00" {
		t.Errorf("20%% bucket wrong: basis=%s tax=%s", tb[1].BasisAmount.Value, tb[1].CalculatedAmount.Value)
	}
}

func TestGenerate_RejectsIncomplete(t *testing.T) {
	in := Input{
		Currency: "EUR", // no Number, no IssueDate
		Seller:   Party{Name: "Dr Martin", VATID: "FR12345678901"},
		Buyer:    Party{Name: "ACME"},
		Lines:    []Line{{Description: "X", Quantity: 1, UnitAmount: 100, VATRate: 2000}},
	}
	if _, err := GenerateXML(in); err != ErrIncomplete {
		t.Fatalf("err = %v, want ErrIncomplete", err)
	}
}

func TestGenerateFacturX_ProducesPDF(t *testing.T) {
	out, err := GenerateFacturX(issuedInput())
	if err != nil {
		t.Fatalf("generate facturx: %v", err)
	}
	if !strings.HasPrefix(string(out[:5]), "%PDF-") {
		t.Errorf("output is not a PDF (prefix %q)", string(out[:5]))
	}
	if len(out) < 1000 {
		t.Errorf("PDF suspiciously small: %d bytes", len(out))
	}
}

func TestMoneyFormatting(t *testing.T) {
	cases := map[int64]string{0: "0.00", 5: "0.05", 100: "1.00", 12345: "123.45", -250: "-2.50"}
	for in, want := range cases {
		if got := money(in); got != want {
			t.Errorf("money(%d) = %s, want %s", in, got, want)
		}
	}
}
