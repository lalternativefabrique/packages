// Package pdftext reads the text layer of a PDF in pure Go and keeps where
// every word sits on its page, so a caller can tell columns apart.
package pdftext

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"unicode"

	gopdf "github.com/ledongthuc/pdf"
)

var (
	ErrUnreadable = errors.New("pdftext: unreadable PDF")
	// ErrNoText is returned for a PDF that parses but carries no text layer,
	// typically a scan: it needs OCR, not this package.
	ErrNoText = errors.New("pdftext: the PDF has no text layer")
)

// Word is a run of glyphs with no gap between them. X0 and X1 bound it
// horizontally, Y is its baseline; all in points from the page's bottom-left.
type Word struct {
	Text string
	Page int
	X0   float64
	X1   float64
	Y    float64
	Size float64
}

type Line struct {
	Page  int
	Y     float64
	Words []Word
}

func (l Line) Text() string {
	parts := make([]string, len(l.Words))
	for i, w := range l.Words {
		parts[i] = w.Text
	}
	return strings.Join(parts, " ")
}

type Page struct {
	Number int
	Lines  []Line
}

func (p Page) Text() string {
	lines := make([]string, len(p.Lines))
	for i, l := range p.Lines {
		lines[i] = l.Text()
	}
	return strings.Join(lines, "\n")
}

type Document struct {
	Pages []Page
}

func (d *Document) Text() string {
	pages := make([]string, len(d.Pages))
	for i, p := range d.Pages {
		pages[i] = p.Text()
	}
	return strings.Join(pages, "\n\f")
}

func (d *Document) Lines() []Line {
	var out []Line
	for _, p := range d.Pages {
		out = append(out, p.Lines...)
	}
	return out
}

// Read parses data. A malformed PDF is ErrUnreadable, never a panic.
func Read(data []byte) (doc *Document, err error) {
	defer func() {
		if r := recover(); r != nil {
			doc, err = nil, fmt.Errorf("%w: %v", ErrUnreadable, r)
		}
	}()
	reader, err := gopdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	doc = &Document{}
	words := 0
	for n := 1; n <= reader.NumPage(); n++ {
		page := reader.Page(n)
		var glyphs []glyph
		if !page.V.IsNull() {
			for _, t := range page.Content().Text {
				glyphs = append(glyphs, glyph{text: t.S, x: t.X, y: t.Y, w: t.W, size: t.FontSize})
			}
		}
		lines := assemble(n, advanceUnmeasured(glyphs))
		for _, l := range lines {
			words += len(l.Words)
		}
		doc.Pages = append(doc.Pages, Page{Number: n, Lines: lines})
	}
	if words == 0 {
		return doc, ErrNoText
	}
	return doc, nil
}

type glyph struct {
	text       string
	x, y, w    float64
	size       float64
	whitespace bool
}

const (
	lineTolerance = 0.35
	wordGap       = 0.18
	overstrike    = 0.05
)

// assemble groups glyphs into lines by baseline and lines into words by the
// horizontal gap between glyphs, the way a reader sees them, whatever order
// the content stream drew them in. Only horizontal text is kept: the parser
// reports a rotated glyph with a zero font size, and a bank's vertical margin
// code read as one-character lines would bleed into the movements beside it.
func assemble(page int, raw []glyph) []Line {
	glyphs := make([]glyph, 0, len(raw))
	for _, g := range raw {
		if g.text == "" || g.size <= 0 {
			continue
		}
		g.whitespace = strings.TrimFunc(g.text, unicode.IsSpace) == ""
		glyphs = append(glyphs, g)
	}
	sort.SliceStable(glyphs, func(i, j int) bool { return glyphs[i].y > glyphs[j].y })

	var rows [][]glyph
	var rowY, rowSize float64
	for _, g := range glyphs {
		if len(rows) > 0 && math.Abs(g.y-rowY) <= lineTolerance*math.Max(rowSize, g.size) {
			rows[len(rows)-1] = append(rows[len(rows)-1], g)
			continue
		}
		rows = append(rows, []glyph{g})
		rowY, rowSize = g.y, g.size
	}

	lines := make([]Line, 0, len(rows))
	for _, row := range rows {
		sort.SliceStable(row, func(i, j int) bool { return row[i].x < row[j].x })
		row = dropOverstrikes(row)
		line := Line{Page: page, Y: row[0].y}
		var cur *Word
		var b strings.Builder
		flush := func() {
			if cur != nil {
				cur.Text = b.String()
				line.Words = append(line.Words, *cur)
				cur = nil
				b.Reset()
			}
		}
		for _, g := range row {
			if g.whitespace {
				flush()
				continue
			}
			end := g.x + width(g)
			if cur != nil && g.x-cur.X1 > wordGap*math.Max(cur.Size, g.size) {
				flush()
			}
			if cur == nil {
				cur = &Word{Page: page, X0: g.x, X1: end, Y: g.y, Size: g.size}
			}
			b.WriteString(g.text)
			cur.X1 = math.Max(cur.X1, end)
		}
		flush()
		if len(line.Words) > 0 {
			lines = append(lines, line)
		}
	}
	return lines
}

// advanceUnmeasured spreads the glyphs of a font that publishes no widths,
// such as the standard Helvetica: the parser then reports every glyph of a
// string at the string's origin, and "3000" would read as overlapping zeros.
// Glyphs come in drawing order, so a glyph at the very origin of the one
// before it continues that string.
func advanceUnmeasured(glyphs []glyph) []glyph {
	var originX, nextX float64
	for i := range glyphs {
		g := &glyphs[i]
		if i > 0 && g.w == 0 && g.x == originX && g.y == glyphs[i-1].y {
			g.x = nextX
		} else {
			originX = g.x
		}
		nextX = g.x + width(*g)
	}
	return glyphs
}

// dropOverstrikes removes a glyph drawn again at the same place, the way some
// generators fake bold: read twice, "1 719,50" would become "11 771199,,5500".
func dropOverstrikes(row []glyph) []glyph {
	out := row[:0:0]
	for _, g := range row {
		if n := len(out); n > 0 {
			prev := out[n-1]
			if prev.text == g.text && math.Abs(prev.x-g.x) <= overstrike*g.size && math.Abs(prev.y-g.y) <= overstrike*g.size {
				continue
			}
		}
		out = append(out, g)
	}
	return out
}

// width falls back to half an em when the font publishes no glyph widths,
// which some embedded subsets omit.
func width(g glyph) float64 {
	if g.w > 0 {
		return g.w
	}
	return 0.5 * g.size
}
