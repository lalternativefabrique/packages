package redact

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/lalternative/packages/go/pdftext"
)

// Copy is a PDF rebuilt with its IBANs grayed out. IBANs lists the last
// four characters of each one, in reading order, so a caller can still tell
// which account a document is about; Hidden counts every grayed span,
// fragments included.
type Copy struct {
	PDF    []byte
	IBANs  []string
	Hidden int
}

const (
	a4Width, a4Height = 595.28, 841.89
	defaultSize       = 9.0
	boxGray           = 200
)

var epoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// PDF rebuilds data from its text layer alone: every word is drawn again in
// Helvetica where the original printed it, and every IBAN becomes a gray box
// that shows only its last four characters. Nothing of the original is
// copied, neither its streams, images, fonts nor metadata, so the copy holds
// no bank detail a viewer or an extractor could recover. It keeps the
// figures and their columns, but not the logos and rules.
//
// A PDF with no text layer (a scan) fails with pdftext.ErrNoText: it cannot
// be redacted without OCR, and a caller must not keep it.
func PDF(data []byte) (*Copy, error) {
	doc, err := pdftext.Read(data)
	if err != nil {
		return nil, err
	}
	out := &Copy{}
	p := fpdf.New("P", "pt", "A4", "")
	p.SetCreationDate(epoch)
	p.SetModificationDate(epoch)
	p.SetAutoPageBreak(false, 0)
	p.SetMargins(0, 0, 0)
	tr := p.UnicodeTranslatorFromDescriptor("")
	var held accounts
	for _, line := range doc.Lines() {
		held = append(held, accountsOf(FindIBANs(lineText(line)))...)
	}
	for _, page := range doc.Pages {
		w, h := page.Width, page.Height
		if w == 0 || h == 0 {
			w, h = a4Width, a4Height
		}
		p.AddPageFormat("P", fpdf.SizeType{Wd: w, Ht: h})
		p.SetFont("Helvetica", "", defaultSize)
		for _, line := range page.Lines {
			drawLine(p, tr, h, line, held, out)
		}
	}
	var buf bytes.Buffer
	if err := p.Output(&buf); err != nil {
		return nil, fmt.Errorf("redact: write copy: %w", err)
	}
	out.PDF = buf.Bytes()
	return out, nil
}

type placed struct {
	word       pdftext.Word
	start, end int
}

func lineText(line pdftext.Line) string {
	s, _ := placeWords(line)
	return s
}

func placeWords(line pdftext.Line) (string, []placed) {
	var text strings.Builder
	words := make([]placed, len(line.Words))
	for i, w := range line.Words {
		if i > 0 {
			text.WriteByte(' ')
		}
		words[i] = placed{word: w, start: text.Len()}
		text.WriteString(w.Text)
		words[i].end = text.Len()
	}
	return text.String(), words
}

func drawLine(p *fpdf.Fpdf, tr func(string) string, pageHeight float64, line pdftext.Line, held accounts, out *Copy) {
	s, words := placeWords(line)
	ibans := FindIBANs(s)
	spans := merge(ibans, held.spans(s, ibans))
	for _, sp := range ibans {
		if sp.Last4 != "" {
			out.IBANs = append(out.IBANs, sp.Last4)
		}
	}
	out.Hidden += len(spans)

	for i := 0; i < len(words); {
		first := words[i]
		if !overlaps(first, spans) {
			drawWord(p, tr, pageHeight, first.word.Text, first.word.X0, first.word.X1, first.word.Y, first.word.Size)
			i++
			continue
		}
		j := i
		for j+1 < len(words) && shareSpan(words[j], words[j+1], spans) {
			j++
		}
		last := words[j]
		drawHidden(p, tr, pageHeight, maskRange(s, first.start, last.end, spans), first.word, last.word)
		i = j + 1
	}
}

func overlaps(w placed, spans []Span) bool {
	for _, sp := range spans {
		if w.start < sp.End && sp.Start < w.end {
			return true
		}
	}
	return false
}

// shareSpan keeps a run of words together while the next one holds part of
// the same hidden span, so an IBAN printed in groups of four becomes one box.
func shareSpan(cur, next placed, spans []Span) bool {
	for _, sp := range spans {
		if cur.start < sp.End && sp.Start < cur.end && next.start < sp.End && sp.Start < next.end {
			return true
		}
	}
	return false
}

func maskRange(s string, from, to int, spans []Span) string {
	var b strings.Builder
	at := from
	for _, sp := range spans {
		if sp.End <= from || sp.Start >= to {
			continue
		}
		start := max(sp.Start, from)
		b.WriteString(s[at:start])
		b.WriteString(Hidden + sp.Last4)
		at = min(sp.End, to)
	}
	b.WriteString(s[at:to])
	return b.String()
}

func drawHidden(p *fpdf.Fpdf, tr func(string) string, pageHeight float64, text string, first, last pdftext.Word) {
	size := first.Size
	if size <= 0 {
		size = defaultSize
	}
	x0, x1 := first.X0, max(last.X1, first.X0+size)
	top := pageHeight - first.Y - size*0.85
	p.SetFillColor(boxGray, boxGray, boxGray)
	p.Rect(x0-1, top, x1-x0+2, size*1.15, "F")
	drawWord(p, tr, pageHeight, text, x0, x1, first.Y, size)
}

func drawWord(p *fpdf.Fpdf, tr func(string) string, pageHeight float64, text string, x0, x1, y, size float64) {
	if size <= 0 {
		size = defaultSize
	}
	encoded := tr(text)
	p.SetFontSize(size)
	if target := x1 - x0; target > 0 {
		if w := p.GetStringWidth(encoded); w > target*1.02 {
			p.SetFontSize(size * target / w)
		}
	}
	p.Text(x0, pageHeight-y, encoded)
}
