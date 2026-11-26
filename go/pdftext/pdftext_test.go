package pdftext_test

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"

	"github.com/lalternative/packages/go/pdftext"
)

func render(t *testing.T, draw func(p *fpdf.Fpdf, tr func(string) string)) []byte {
	t.Helper()
	p := fpdf.New("P", "pt", "A4", "")
	p.SetCompression(true)
	p.AddPage()
	p.SetFont("Helvetica", "", 10)
	draw(p, p.UnicodeTranslatorFromDescriptor(""))
	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func statement(p *fpdf.Fpdf, tr func(string) string) {
	p.Text(40, 60, tr("Relevé de compte"))
	p.Text(40, 80, "IBAN FR76 3000 6000 0112 3456 7890 189")
	p.Text(40, 120, "Date")
	p.Text(100, 120, tr("Libellé"))
	p.Text(380, 120, tr("Débit"))
	p.Text(470, 120, tr("Crédit"))
	p.Text(40, 140, "05/10")
	p.Text(100, 140, "PRLV SEPA SCALEWAY")
	p.Text(380, 140, "27,60")
	p.Text(40, 155, "12/10")
	p.Text(100, 155, "VIR CLIENT ACME")
	p.Text(470, 155, tr("1\u00a0719,50"))
}

func TestReadKeepsWordsInReadingOrderWithTheirColumns(t *testing.T) {
	doc, err := pdftext.Read(render(t, statement))
	if err != nil {
		t.Fatal(err)
	}
	text := doc.Text()
	for _, want := range []string{"Relevé de compte", "Date Libellé Débit Crédit", "05/10 PRLV SEPA SCALEWAY 27,60", "12/10 VIR CLIENT ACME 1 719,50"} {
		if !strings.Contains(text, want) {
			t.Errorf("text misses %q:\n%s", want, text)
		}
	}
	lines := doc.Lines()
	var debit, amount pdftext.Word
	for _, l := range lines {
		for _, w := range l.Words {
			switch w.Text {
			case "Débit":
				debit = w
			case "27,60":
				amount = w
			}
		}
	}
	if debit.Text == "" || amount.Text == "" {
		t.Fatalf("words not found in %+v", lines)
	}
	if amount.X0 < debit.X0-1 || amount.X0 > debit.X1+1 {
		t.Errorf("27,60 at x=%.1f is not under Débit [%.1f, %.1f]", amount.X0, debit.X0, debit.X1)
	}
	if amount.Page != 1 || amount.Y >= debit.Y {
		t.Errorf("27,60 must sit below the header on page 1, got page %d y=%.1f header y=%.1f", amount.Page, amount.Y, debit.Y)
	}
}

func TestReadOrdersLinesTopToBottomWhateverTheDrawingOrder(t *testing.T) {
	doc, err := pdftext.Read(render(t, func(p *fpdf.Fpdf, _ func(string) string) {
		p.Text(300, 200, "right")
		p.Text(40, 300, "last")
		p.Text(40, 200, "left")
		p.Text(40, 100, "first")
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Text(); got != "first\nleft right\nlast" {
		t.Errorf("text = %q", got)
	}
}

func TestReadDropsAGlyphDrawnTwiceInPlace(t *testing.T) {
	doc, err := pdftext.Read(render(t, func(p *fpdf.Fpdf, _ func(string) string) {
		p.Text(40, 100, "1 719,50")
		p.Text(40.2, 100, "1 719,50")
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Text(); got != "1 719,50" {
		t.Errorf("fake bold read as %q", got)
	}
}

func TestReadSpreadsPages(t *testing.T) {
	doc, err := pdftext.Read(render(t, func(p *fpdf.Fpdf, _ func(string) string) {
		p.Text(40, 100, "one")
		p.AddPage()
		p.Text(40, 100, "two")
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 2 || doc.Pages[1].Text() != "two" || doc.Pages[1].Lines[0].Words[0].Page != 2 {
		t.Errorf("pages = %+v", doc.Pages)
	}
}

func TestReadRefusesWhatIsNotAPDF(t *testing.T) {
	if _, err := pdftext.Read([]byte("not a pdf")); !errors.Is(err, pdftext.ErrUnreadable) {
		t.Errorf("err = %v, want ErrUnreadable", err)
	}
	pdf := render(t, func(*fpdf.Fpdf, func(string) string) {})
	if _, err := pdftext.Read(pdf[:len(pdf)/2]); !errors.Is(err, pdftext.ErrUnreadable) {
		t.Errorf("truncated PDF: err = %v, want ErrUnreadable", err)
	}
}

func TestReadTellsAPDFWithoutTextFromAnUnreadableOne(t *testing.T) {
	doc, err := pdftext.Read(render(t, func(p *fpdf.Fpdf, _ func(string) string) {
		p.Rect(40, 40, 100, 100, "F")
	}))
	if !errors.Is(err, pdftext.ErrNoText) || doc == nil || len(doc.Pages) != 1 {
		t.Errorf("doc = %+v, err = %v, want one empty page and ErrNoText", doc, err)
	}
}

func TestReadGivesThePageSize(t *testing.T) {
	doc, err := pdftext.Read(render(t, func(p *fpdf.Fpdf, _ func(string) string) { p.Text(40, 100, "x") }))
	if err != nil {
		t.Fatal(err)
	}
	if p := doc.Pages[0]; math.Abs(p.Width-595.28) > 0.1 || math.Abs(p.Height-841.89) > 0.1 {
		t.Errorf("page size = %.2f x %.2f, want A4", p.Width, p.Height)
	}
}
