package facturx

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/go-pdf/fpdf"
)

// renderInvoicePDF produces the human-readable invoice PDF (the visual half of
// Factur-X). It is plain PDF here; the PDF/A-3 conformance markers (OutputIntent,
// ICC, XMP) and the embedded XML are added in a separate post-processing step
// (see container.go). Kept deliberately simple — layout polish is orthogonal to
// compliance.
//
// Encoding: fpdf's core fonts (Helvetica) use WinAnsi/cp1252, but our strings
// are UTF-8. Every drawn string is run through a cp1252 translator (`tr`) so
// French accents render correctly instead of mojibake. cp1252 covers all of
// French/Western-European; CJK/emoji would need an embedded UTF-8 TTF (future).
func renderInvoicePDF(in Input) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	tr := pdf.UnicodeTranslatorFromDescriptor("") // cp1252
	pdf.SetTitle(tr(fmt.Sprintf("Invoice %s", in.Number)), false)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()

	// Header
	pdf.SetFont("Helvetica", "B", 18)
	pdf.CellFormat(0, 12, tr("FACTURE"), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 6, tr(fmt.Sprintf("N° %s", in.Number)), "", 1, "L", false, 0, "")
	if d := displayDate(in.IssueDate); d != "" {
		pdf.CellFormat(0, 6, tr("Date : "+d), "", 1, "L", false, 0, "")
	}
	pdf.Ln(4)

	// Seller / Buyer blocks
	pdf.SetFont("Helvetica", "B", 10)
	pdf.CellFormat(95, 6, tr("Émetteur"), "", 0, "L", false, 0, "")
	pdf.CellFormat(95, 6, tr("Client"), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	party := func(p Party) []string {
		var out []string
		// Skipped rather than emitted blank: a buyer we only know by email has
		// no name, and starting their block with an empty line reads as a
		// rendering fault. Deduped against the email for the same reason —
		// printing the address twice, once as the name, looked like a bug.
		if p.Name != "" && p.Name != p.Email {
			out = append(out, p.Name)
		}
		out = append(out, p.Address...)
		if p.LegalID != "" {
			out = append(out, "SIREN "+p.LegalID)
		}
		if p.VATID != "" {
			out = append(out, "TVA "+p.VATID)
		}
		if p.Email != "" {
			out = append(out, p.Email)
		}
		return out
	}
	sLines, bLines := party(in.Seller), party(in.Buyer)
	n := len(sLines)
	if len(bLines) > n {
		n = len(bLines)
	}
	for i := 0; i < n; i++ {
		s, b := "", ""
		if i < len(sLines) {
			s = sLines[i]
		}
		if i < len(bLines) {
			b = bLines[i]
		}
		pdf.CellFormat(95, 5, tr(toCP1252(s)), "", 0, "L", false, 0, "")
		pdf.CellFormat(95, 5, tr(toCP1252(b)), "", 1, "L", false, 0, "")
	}
	pdf.Ln(6)

	// Lines table header
	pdf.SetFont("Helvetica", "B", 9)
	pdf.CellFormat(80, 7, tr("Désignation"), "1", 0, "L", false, 0, "")
	pdf.CellFormat(20, 7, tr("Qté"), "1", 0, "R", false, 0, "")
	pdf.CellFormat(30, 7, tr("PU HT"), "1", 0, "R", false, 0, "")
	pdf.CellFormat(20, 7, tr("TVA"), "1", 0, "R", false, 0, "")
	pdf.CellFormat(40, 7, tr("Total HT"), "1", 1, "R", false, 0, "")

	pdf.SetFont("Helvetica", "", 9)
	for _, l := range in.Lines {
		drawLineRow(pdf, tr, l, in.Currency)
	}
	pdf.Ln(4)

	// Totals
	totals := [][2]string{
		{"Total HT", money(in.totalExclVAT())},
		{"TVA", money(in.totalVAT())},
		{"Total TTC", money(in.totalInclVAT())},
	}
	for i, tt := range totals {
		if i == len(totals)-1 {
			pdf.SetFont("Helvetica", "B", 10)
		}
		pdf.CellFormat(150, 7, tr(tt[0]), "", 0, "R", false, 0, "")
		pdf.CellFormat(40, 7, tr(tt[1]+" "+in.Currency), "1", 1, "R", false, 0, "")
	}

	// Statutory mention. Printed below the totals because that is where a reader
	// looks to understand a zero-VAT total: under franchise en base the document
	// is only regular if it says why no VAT was charged.
	if in.LegalMention != "" {
		pdf.Ln(6)
		pdf.SetFont("Helvetica", "", 9)
		// Folded like the line descriptions: MultiCell measures by rune too, so
		// a typographic character in a configured mention would panic here.
		pdf.MultiCell(0, 5, tr(toCP1252(in.LegalMention)), "", "L", false)
	}

	if pdf.Err() {
		return nil, fmt.Errorf("render pdf: %w", pdf.Error())
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("render pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// cp1252Folds maps the typographic characters that routinely appear in
// human-written text onto their nearest cp1252 equivalent. They are the ones
// worth keeping legible; anything else outside the table becomes '?'.
var cp1252Folds = map[rune]string{
	'—': "-", // em dash
	'–': "-", // en dash
	'‑': "-", // non-breaking hyphen
	'’': "'", // right single quote
	'‘': "'",
	'“': `"`,
	'”': `"`,
	'«': `"`,
	'»': `"`,
	'…': "...",
	' ': " ", // non-breaking space
	'€': "EUR",
}

// toCP1252 folds a string onto the 256-rune range the PDF font table covers.
//
// This is a SAFETY function, not a formatting one: fpdf's SplitText indexes its
// width table by rune, so a single rune above 255 panics — and the text here is
// caller-supplied (an invoice line description, a customer name). A typographic
// dash pasted into a product label would otherwise kill the process that issues
// invoices. Unmappable runes become '?' so the document still renders, visibly
// imperfect rather than absent.
func toCP1252(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r < 256:
			b.WriteRune(r)
		default:
			if fold, ok := cp1252Folds[r]; ok {
				b.WriteString(fold)
			} else {
				b.WriteByte('?')
			}
		}
	}
	return b.String()
}

// displayDate turns the YYYYMMDD issue date into DD/MM/YYYY for the human PDF.
// Returns "" if the value is not 8 digits (the PDF simply omits the date line).
func displayDate(yyyymmdd string) string {
	if len(yyyymmdd) != 8 {
		return ""
	}
	return yyyymmdd[6:8] + "/" + yyyymmdd[4:6] + "/" + yyyymmdd[0:4]
}

// drawLineRow draws one invoice line. The description column uses MultiCell so a
// long designation wraps inside its 80mm column instead of overrunning the
// table; the numeric columns are drawn on the row's first visual line and the
// cursor is advanced by the description's height.
func drawLineRow(pdf *fpdf.Fpdf, tr func(string) string, l Line, currency string) {
	const (
		descW  = 80.0
		lineH  = 6.0
		qtyW   = 20.0
		puW    = 30.0
		tvaW   = 20.0
		totalW = 40.0
	)
	x, y := pdf.GetXY()

	// Measure wrapped-line count on the ORIGINAL UTF-8 string: SplitText indexes
	// the font width table by rune and panics on cp1252 bytes, so it must never
	// see the translated text.
	//
	// It is indexed by rune into a 256-entry table, so it also panics on any
	// rune ABOVE 255 — an em dash, a curly quote, an emoji. A description is
	// caller-supplied text, so that turned a typographic character into a
	// process-killing panic in the worker that issues invoices. Characters
	// outside the table are folded to their nearest cp1252 equivalent first.
	desc := toCP1252(l.Description)
	lines := pdf.SplitText(desc, descW-2)
	rowH := lineH * float64(len(lines))
	if rowH < lineH {
		rowH = lineH
	}

	// Description (wrapping) in its own box, drawn with the cp1252-translated
	// text so accents render correctly.
	pdf.MultiCell(descW, lineH, tr(desc), "1", "L", false)

	// Numeric columns: a single full-height cell each, aligned to the row top.
	pdf.SetXY(x+descW, y)
	pdf.CellFormat(qtyW, rowH, fmt.Sprintf("%d", l.Quantity), "1", 0, "R", false, 0, "")
	pdf.CellFormat(puW, rowH, tr(money(l.UnitAmount)+" "+currency), "1", 0, "R", false, 0, "")
	pdf.CellFormat(tvaW, rowH, tr(percent(l.VATRate)+"%"), "1", 0, "R", false, 0, "")
	pdf.CellFormat(totalW, rowH, tr(money(l.lineTotal())+" "+currency), "1", 0, "R", false, 0, "")

	// Advance to the next row (below the taller of desc/numeric).
	pdf.SetXY(x, y+rowH)
}
