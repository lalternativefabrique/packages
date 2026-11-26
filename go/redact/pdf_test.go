package redact_test

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"
	"github.com/lalternative/packages/go/docfacts"
	"github.com/lalternative/packages/go/pdftext"
	"github.com/lalternative/packages/go/redact"
)

func statement(t *testing.T) []byte {
	t.Helper()
	p := fpdf.New("P", "pt", "A4", "")
	p.SetCompression(false)
	tr := p.UnicodeTranslatorFromDescriptor("")
	p.AddPage()
	p.SetFont("Helvetica", "", 9)
	p.Text(40, 60, "RELEVE DE COMPTE")
	p.Text(40, 80, "IBAN FR76 3000 6000 0112 3456 7890 189 BIC BNPAFRPP")
	p.Text(40, 120, tr("Date"))
	p.Text(120, 120, tr("Libellé"))
	p.Text(400, 120, tr("Débit"))
	p.Text(480, 120, tr("Crédit"))
	p.Text(40, 140, "05/11")
	p.Text(120, 140, "VIR SEPA VERSFR7630006000011234567890189 LOYER")
	p.Text(400, 140, "1 200,00")
	p.Text(40, 160, "06/11")
	p.Text(120, 160, "VIR DE DE89 3704 0044 0532 0130 00")
	p.Text(480, 160, "27,60")
	p.Text(40, 180, "07/11")
	p.Text(120, 180, "REF FR76 3000 6000 01")
	p.Text(400, 180, "3,10")
	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPDFGraysOutEveryIBAN(t *testing.T) {
	c, err := redact.PDF(statement(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(c.IBANs, ",") != "0189,0189,3000" || c.Hidden != 4 {
		t.Errorf("IBANs = %v, hidden = %d", c.IBANs, c.Hidden)
	}
	doc, err := pdftext.Read(c.PDF)
	if err != nil {
		t.Fatal(err)
	}
	text := doc.Text()
	for _, leak := range []string{"3000 6000", "30006000", "3704", "0532", "FR76"} {
		if strings.Contains(text, leak) || bytes.Contains(c.PDF, []byte(leak)) {
			t.Errorf("the copy still holds %q:\n%s", leak, text)
		}
	}
	for _, kept := range []string{"IBAN ••••0189 BIC BNPAFRPP", "VERS••••0189 LOYER", "VIR DE ••••3000", "REF ••••", "Libellé"} {
		if !strings.Contains(text, kept) {
			t.Errorf("the copy lost %q:\n%s", kept, text)
		}
	}
	if len(docfacts.Scan(doc).IBANs) != 0 {
		t.Error("a valid IBAN survived in the copy")
	}
}

func TestPDFKeepsFiguresInTheirColumns(t *testing.T) {
	orig := statement(t)
	c, err := redact.PDF(orig)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := pdftext.Read(orig)
	after, err := pdftext.Read(c.PDF)
	if err != nil {
		t.Fatal(err)
	}
	if p := after.Pages[0]; math.Abs(p.Width-before.Pages[0].Width) > 0.01 || math.Abs(p.Height-before.Pages[0].Height) > 0.01 {
		t.Errorf("page size changed: %.1fx%.1f", p.Width, p.Height)
	}
	fa, fb := docfacts.Scan(after), docfacts.Scan(before)
	if len(fa.Amounts) != len(fb.Amounts) {
		t.Fatalf("amounts: %d after, %d before", len(fa.Amounts), len(fb.Amounts))
	}
	cols := docfacts.Headers(after, "débit", "crédit")
	for _, a := range fa.Amounts {
		c, ok := docfacts.ColumnOf(cols, a)
		switch a.Cents {
		case 120000, 310:
			if !ok || c.Name != "débit" {
				t.Errorf("%d not under Débit", a.Cents)
			}
		case 2760:
			if !ok || c.Name != "crédit" {
				t.Errorf("%d not under Crédit", a.Cents)
			}
		}
	}
}

func TestPDFIsDeterministic(t *testing.T) {
	a, _ := redact.PDF(statement(t))
	b, _ := redact.PDF(statement(t))
	if !bytes.Equal(a.PDF, b.PDF) {
		t.Error("two copies of the same PDF differ")
	}
}

func TestPDFRefusesAScan(t *testing.T) {
	p := fpdf.New("P", "pt", "A4", "")
	p.AddPage()
	p.Rect(10, 10, 100, 100, "F")
	var buf bytes.Buffer
	_ = p.Output(&buf)
	if _, err := redact.PDF(buf.Bytes()); !errors.Is(err, pdftext.ErrNoText) {
		t.Errorf("err = %v, want ErrNoText", err)
	}
	if _, err := redact.PDF([]byte("not a pdf")); !errors.Is(err, pdftext.ErrUnreadable) {
		t.Errorf("err = %v, want ErrUnreadable", err)
	}
}
