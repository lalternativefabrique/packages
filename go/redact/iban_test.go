package redact_test

import (
	"strings"
	"testing"

	"github.com/lalternative/packages/go/redact"
)

func TestIBANs(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"grouped by four", "VIR SEPA FR76 3000 6000 0112 3456 7890 189 LOYER", "VIR SEPA ••••0189 LOYER"},
		{"compact", "FR7630006000011234567890189", "••••0189"},
		{"glued to a word", "VIRFR7630006000011234567890189 ACME", "VIR••••0189 ACME"},
		{"glued to a BIC", "FR7630006000011234567890189BNPAFRPP", "••••0189BNPAFRPP"},
		{"odd grouping", "DE FR76 30006000011234 567890189", "DE ••••0189"},
		{"lower case", "iban fr76 3000 6000 0112 3456 7890 189", "iban ••••0189"},
		{"non-breaking spaces", "FR76 3000 6000 0112 3456 7890 189", "••••0189"},
		{"dashes", "DE89-3704-0044-0532-0130-00", "••••3000"},
		{"followed by an amount", "VIR GB82 WEST 1234 5698 7654 32 1 200,00", "VIR ••••5432 1 200,00"},
		{"half masked by an earlier pass", "FR76 30006000011234 ••••0189", "•••• ••••0189"},
		{"cut short", "VIR FR76 3000 6000 01 ACME", "VIR •••• ACME"},
		{"cut short, then an amount", "REF FR76 3000 6000 01 3,10", "REF •••• 3,10"},
		{"bank letters cut short", "GB82 WEST 1234 5698 76", "••••"},
		{"two in one label", "DE89370400440532013000 VERS FR7630006000011234567890189", "••••3000 VERS ••••0189"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redact.IBANs(c.in)
			if got != c.want {
				t.Fatalf("IBANs(%q) = %q, want %q", c.in, got, c.want)
			}
			if again := redact.IBANs(got); again != got {
				t.Fatalf("IBANs is not idempotent: %q then %q", got, again)
			}
		})
	}
}

func TestIBANsLeavesOtherTextAlone(t *testing.T) {
	for _, in := range []string{
		"",
		"CB CARREFOUR 12/11 45,20",
		"PRLV SEPA FR12ZZZ123456 EDF",
		"ECH PRET 0012345 DU 05/11/2026",
		"FR76",
		"fr12 ans",
		"FACTURE NO 2026-11-0042",
		"••••0189",
		"VIR INST DE MARTIN 12,50",
	} {
		if got := redact.IBANs(in); got != in {
			t.Errorf("IBANs(%q) = %q, want it unchanged", in, got)
		}
		if redact.ContainsIBAN(in) {
			t.Errorf("Contains(%q) = true", in)
		}
	}
}

func TestIBANsLongText(t *testing.T) {
	page := strings.Repeat("05/11 VIR SEPA ACME FR7630006000011234567890189 1 200,00\n", 300)
	got := redact.IBANs(page)
	if strings.Contains(got, "30006000011234567890") || strings.Count(got, "••••0189") != 300 {
		t.Fatal("an IBAN of a long page survived")
	}
}

func TestTextMasksTheAccountNumbersOfItsIBANs(t *testing.T) {
	in := "COMPTE DE DEPOT N° 01234567890 IBAN FR82 3000 6000 1001 2345 6789 000 REF 7654321 DU 05/11/2026 CP 31170"
	got := redact.Text(in)
	want := "COMPTE DE DEPOT N° •••• IBAN ••••9000 REF 7654321 DU 05/11/2026 CP 31170"
	if got != want {
		t.Fatalf("Text = %q, want %q", got, want)
	}
	if redact.Text("COMPTE N° 01234567890") != "COMPTE N° 01234567890" {
		t.Error("an account number with no IBAN to vouch for it was masked")
	}
}
