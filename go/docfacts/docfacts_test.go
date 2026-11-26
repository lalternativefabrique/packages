package docfacts_test

import (
	"testing"

	"github.com/lalternative/packages/go/docfacts"
	"github.com/lalternative/packages/go/pdftext"
)

func doc(lines ...[]pdftext.Word) *pdftext.Document {
	page := pdftext.Page{Number: 1}
	for i, words := range lines {
		y := 800 - float64(i)*15
		for j := range words {
			words[j].Page, words[j].Y = 1, y
			if words[j].X1 == 0 {
				words[j].X1 = words[j].X0 + float64(len(words[j].Text))*5
			}
		}
		page.Lines = append(page.Lines, pdftext.Line{Page: 1, Y: y, Words: words})
	}
	return &pdftext.Document{Pages: []pdftext.Page{page}}
}

func text(s string) []pdftext.Word {
	var out []pdftext.Word
	x := 40.0
	word := ""
	flush := func() {
		if word != "" {
			out = append(out, pdftext.Word{Text: word, X0: x})
			x += float64(len(word))*5 + 5
			word = ""
		}
	}
	for _, r := range s {
		if r == ' ' {
			flush()
			continue
		}
		word += string(r)
	}
	flush()
	return out
}

func amounts(t *testing.T, line string) []docfacts.Amount {
	t.Helper()
	return docfacts.Scan(doc(text(line))).Amounts
}

func TestScanReadsFrenchAmountsToTheCent(t *testing.T) {
	for line, want := range map[string][]int64{
		"04/09 VIR MLLE STUTZ 04/09 04/09 - 1 320,00 €": {-132000},
		"VIR FRANCE TRAVAIL 2 196,90 €":                 {219690},
		"PRLV SEPA 27,60- COTISATION":                   {-2760},
		"FRAIS 45,00 DB REMISE 12,00 CR":                {-4500, 1200},
		"Solde 1.719,50 EUR":                            {171950},
		"27,60 40,57":                                   {2760, 4057},
		"−8,00 +3,50":                                   {-800, 350},
		"total 0,99 euros":                              {99},
	} {
		got := amounts(t, line)
		if len(got) != len(want) {
			t.Errorf("%q: got %+v, want %v", line, got, want)
			continue
		}
		for i, a := range got {
			if a.Signed() != want[i] {
				t.Errorf("%q: amount %d = %d (%q), want %d", line, i, a.Signed(), a.Raw, want[i])
			}
		}
	}
}

func TestScanDoesNotGlueADateToTheAmountAfterIt(t *testing.T) {
	got := amounts(t, "04/09 196,90")
	if len(got) != 1 || got[0].Cents != 19690 {
		t.Fatalf("got %+v, want one amount of 196,90", got)
	}
}

func TestScanIgnoresFiguresInsideWordsAndLongerNumbers(t *testing.T) {
	for _, line := range []string{"REF12,00", "1234,567", "CB****1847", "05/10/2026", "BD94WT9", "3,5%"} {
		if got := amounts(t, line); len(got) != 0 {
			t.Errorf("%q: got %+v, want no amount", line, got)
		}
	}
}

func TestScanKeepsTheShorterReadingOfGroupedDigits(t *testing.T) {
	got := amounts(t, "Réf 12 345,00")
	if len(got) != 1 || got[0].Cents != 1234500 || len(got[0].Alternatives) != 1 || got[0].Alternatives[0] != 34500 {
		t.Fatalf("got %+v, want 12 345,00 with the alternative 345,00", got)
	}
}

func TestScanFindsValidIBANsOnly(t *testing.T) {
	f := docfacts.Scan(doc(
		text("IBAN FR76 3000 6000 0112 3456 7890 189 BIC CCBPFRPPTLS"),
		text("IBAN FR76 1000 6000 0112 3456 7890 189"),
	))
	if len(f.IBANs) != 1 || f.IBANs[0].Value != "FR7630006000011234567890189" {
		t.Fatalf("IBANs = %+v", f.IBANs)
	}
	if !docfacts.ValidIBAN("FR76 3000 6000 0112 3456 7890 189") || docfacts.ValidIBAN("FR7630006000011234567890") {
		t.Error("checksum and length must both hold")
	}
}

func TestScanReadsDates(t *testing.T) {
	f := docfacts.Scan(doc(text("Relevé au 05/10/2026 du 5 nov. 2026 opération 12/10 valeur 31.12.25 1er mars")))
	want := []docfacts.Date{{Day: 5, Month: 10, Year: 2026}, {Day: 12, Month: 10}, {Day: 31, Month: 12, Year: 2025}, {Day: 5, Month: 11, Year: 2026}, {Day: 1, Month: 3}}
	if len(f.Dates) != len(want) {
		t.Fatalf("dates = %+v", f.Dates)
	}
	for i, d := range want {
		g := f.Dates[i]
		if g.Day != d.Day || g.Month != d.Month || g.Year != d.Year {
			t.Errorf("date %d = %+v, want %+v", i, g, d)
		}
	}
	if got := docfacts.Scan(doc(text("31/02/2026 13/13"))).Dates; len(got) != 0 {
		t.Errorf("impossible dates read as %+v", got)
	}
}

func TestMatchNeedsOnePrintedFigurePerClaim(t *testing.T) {
	f := docfacts.Scan(doc(
		text("SOLDE AU 03/09/2026 606,14 €"),
		text("PRLV COFIDIS - 27,60 €"),
		text("PRLV COFIDIS - 27,60 €"),
		text("CB BOULANGERIE - 13,40 €"),
		text("SOLDE AU 05/10/2026 524,54 €"),
	))
	check := f.Match([]docfacts.Claim{
		docfacts.AmountClaim("opening", 60614),
		docfacts.AmountClaim("line 1", -2760),
		docfacts.AmountClaim("line 2", -2760),
		docfacts.AmountClaim("line 3", -2760),
		docfacts.AmountClaim("closing", 52454),
		docfacts.DateClaim("end", 2026, 10, 5),
	})
	if len(check.Missing) != 1 || check.Missing[0].Key != "line 3" {
		t.Fatalf("missing = %+v, want only the third 27,60", check.Missing)
	}
	if len(check.Unclaimed) != 1 || check.Unclaimed[0].Cents != 1340 {
		t.Fatalf("unclaimed = %+v, want the 13,40 nobody read", check.Unclaimed)
	}
	if check.OK() {
		t.Error("a missing claim must fail the check")
	}
	for _, fd := range check.Found {
		if fd.Claim.Key == "line 2" && (fd.At.Line != 2 || fd.Amount.Sign != docfacts.SignNegative) {
			t.Errorf("line 2 found at %+v sign %v, want the second COFIDIS line, negative", fd.At, fd.Amount.Sign)
		}
	}
}

func TestMatchFallsBackToTheShorterReadingOnlyWhenNeeded(t *testing.T) {
	f := docfacts.Scan(doc(text("Réf 12 345,00")))
	if check := f.Match([]docfacts.Claim{docfacts.AmountClaim("line", 34500)}); !check.OK() {
		t.Errorf("345,00 must be found through the alternative reading: %+v", check)
	}
	if check := f.Match([]docfacts.Claim{docfacts.AmountClaim("line", 1234500)}); !check.OK() || len(check.Unclaimed) != 0 {
		t.Errorf("12 345,00 must be found as printed: %+v", check)
	}
}

func TestMatchChecksIBANsAndDates(t *testing.T) {
	f := docfacts.Scan(doc(text("IBAN FR76 3000 6000 0112 3456 7890 189 au 05/11")))
	check := f.Match([]docfacts.Claim{
		docfacts.IBANClaim("account", "fr76 3000 6000 0112 3456 7890 189"),
		docfacts.IBANClaim("other", "FR7630006000011234567890190"),
		docfacts.DateClaim("end", 2026, 11, 5),
	})
	if len(check.Missing) != 1 || check.Missing[0].Key != "other" {
		t.Fatalf("missing = %+v", check.Missing)
	}
}

func TestColumnOfTellsDebitFromCredit(t *testing.T) {
	d := doc(
		[]pdftext.Word{{Text: "Date", X0: 40}, {Text: "Libellé", X0: 100}, {Text: "Débit", X0: 380, X1: 405}, {Text: "Crédit", X0: 470, X1: 498}},
		[]pdftext.Word{{Text: "05/10", X0: 40}, {Text: "PRLV", X0: 100}, {Text: "27,60", X0: 378, X1: 402}},
		[]pdftext.Word{{Text: "12/10", X0: 40}, {Text: "VIR", X0: 100}, {Text: "1", X0: 462, X1: 467}, {Text: "719,50", X0: 469, X1: 498}},
	)
	cols := docfacts.Headers(d, "débit", "crédit")
	f := docfacts.Scan(d)
	if len(cols) != 2 || len(f.Amounts) != 2 {
		t.Fatalf("columns = %+v amounts = %+v", cols, f.Amounts)
	}
	for i, want := range []string{"débit", "crédit"} {
		c, ok := docfacts.ColumnOf(cols, f.Amounts[i])
		if !ok || c.Name != want {
			t.Errorf("amount %s is under %+v (%t), want %s", f.Amounts[i].Raw, c, ok, want)
		}
	}
}
