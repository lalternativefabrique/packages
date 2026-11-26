package docfacts

import (
	"strings"

	"github.com/lalternative/packages/go/pdftext"
)

// Column is a header word such as "Débit" and where it sits on its page.
type Column struct {
	Name string
	Page int
	X0   float64
	X1   float64
	Y    float64
}

// Headers finds the words of doc that read as one of names, ignoring case and
// accents: Headers(doc, "débit", "crédit") finds "DEBIT" and "Crédit". A
// header printed again on every page is found on every page.
func Headers(doc *pdftext.Document, names ...string) []Column {
	wanted := make(map[string]string, len(names))
	for _, n := range names {
		wanted[fold(n)] = n
	}
	var out []Column
	for _, line := range doc.Lines() {
		for _, w := range line.Words {
			if name, ok := wanted[fold(w.Text)]; ok {
				out = append(out, Column{Name: name, Page: w.Page, X0: w.X0, X1: w.X1, Y: w.Y})
			}
		}
	}
	return out
}

// ColumnOf returns the column an amount is printed under: the closest header
// above it on its page whose horizontal extent overlaps the amount's, with
// slack for figures right-aligned past a short header.
func ColumnOf(columns []Column, a Amount) (Column, bool) {
	var best Column
	found := false
	for _, c := range columns {
		if c.Page != a.At.Page || c.Y <= a.At.Y {
			continue
		}
		slack := (c.X1 - c.X0) / 2
		if a.At.X1 < c.X0-slack || a.At.X0 > c.X1+slack {
			continue
		}
		if !found || c.Y < best.Y || (c.Y == best.Y && distance(c, a) < distance(best, a)) {
			best, found = c, true
		}
	}
	return best, found
}

func distance(c Column, a Amount) float64 {
	d := (c.X0+c.X1)/2 - (a.At.X0+a.At.X1)/2
	if d < 0 {
		return -d
	}
	return d
}

var accents = strings.NewReplacer(
	"à", "a", "â", "a", "ä", "a", "é", "e", "è", "e", "ê", "e", "ë", "e",
	"î", "i", "ï", "i", "ô", "o", "ö", "o", "ù", "u", "û", "u", "ü", "u", "ç", "c",
)

func fold(s string) string {
	return accents.Replace(strings.ToLower(strings.Trim(s, ".:;()")))
}
