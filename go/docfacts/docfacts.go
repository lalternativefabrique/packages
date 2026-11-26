// Package docfacts finds, deterministically, the amounts, IBANs and dates a
// document prints, and checks the facts someone claims to have read in it
// against what it actually shows. It knows no business: whether a figure is
// a balance or a VAT total is the caller's decision.
package docfacts

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/lalternative/packages/go/pdftext"
)

// Location is where a fact sits: its page, its horizontal extent and its
// baseline, in points, plus the line it was read on.
type Location struct {
	Page int
	X0   float64
	X1   float64
	Y    float64
	Line int
}

// Sign is what the document itself says about an amount's direction: a
// leading or trailing minus, or a DB/CR marker. Unknown is the common case of
// a statement that tells debits from credits by column only.
type Sign int

const (
	SignUnknown Sign = iota
	SignPositive
	SignNegative
)

// Amount is a figure printed with two decimals, in cents, always >= 0: the
// direction is in Sign. Alternatives are the smaller figures the same text
// also reads as when its leading digit groups belong to a word before it, as
// "Réf 12 345,00" may be a reference 12 then 345,00.
type Amount struct {
	Cents        int64
	Sign         Sign
	Raw          string
	At           Location
	Alternatives []int64
}

// Signed applies the document's sign; an unknown sign reads as positive.
func (a Amount) Signed() int64 {
	if a.Sign == SignNegative {
		return -a.Cents
	}
	return a.Cents
}

type IBAN struct {
	Value string
	At    Location
}

// Date is a day printed as 05/11/2026, 05/11, 05.11.26 or 5 nov. 2026. Year
// is 0 when the document leaves it out.
type Date struct {
	Day   int
	Month int
	Year  int
	Raw   string
	At    Location
}

// Facts lists what a document prints, in reading order: page, then top to
// bottom, then left to right.
type Facts struct {
	Amounts []Amount
	IBANs   []IBAN
	Dates   []Date
}

func Scan(doc *pdftext.Document) Facts {
	var f Facts
	for i, line := range doc.Lines() {
		scanLine(&f, i, line)
	}
	return f
}

type span struct {
	start, end int
	word       int
}

// lineText rebuilds the line as one string, remembering which word each byte
// came from, so a match in the string maps back to positions on the page.
func lineText(line pdftext.Line) (string, []span) {
	var b strings.Builder
	spans := make([]span, 0, len(line.Words))
	for i, w := range line.Words {
		if i > 0 {
			b.WriteByte(' ')
		}
		start := b.Len()
		b.WriteString(w.Text)
		spans = append(spans, span{start: start, end: b.Len(), word: i})
	}
	return b.String(), spans
}

func locate(line pdftext.Line, spans []span, index, start, end int) Location {
	loc := Location{Page: line.Page, Y: line.Y, Line: index}
	first := true
	for _, s := range spans {
		if s.end <= start || s.start >= end {
			continue
		}
		w := line.Words[s.word]
		if first || w.X0 < loc.X0 {
			loc.X0 = w.X0
		}
		if first || w.X1 > loc.X1 {
			loc.X1 = w.X1
		}
		first = false
	}
	return loc
}

const (
	groupSepClass = ` .'\x{a0}\x{202f}\x{2009}`
	groupSepRunes = " .'\u00a0\u202f\u2009"
)

var (
	amountRe = regexp.MustCompile(`((?:[-−+]\s?)?)` +
		`(\d{1,3}(?:[` + groupSepClass + `]\d{3})+|\d+),(\d{2})` +
		`(?:\s?(?:EUROS?|EUR|€))?` +
		`(-|\s(?:DB|CR)\b)?`)
	ibanRe     = regexp.MustCompile(`\b[A-Z]{2}\d{2}(?:\s?[A-Z0-9]{1,4}){2,9}`)
	dateRe     = regexp.MustCompile(`(\d{1,2})[/.](\d{1,2})(?:[/.](\d{4}|\d{2}))?`)
	longDateRe = regexp.MustCompile(`(?i)(\d{1,2})(?:er)?\s+(janv(?:ier)?|f[ée]v(?:rier)?|mars|avr(?:il)?|mai|juin|juil(?:let)?|ao[uû]t|sept(?:embre)?|oct(?:obre)?|nov(?:embre)?|d[ée]c(?:embre)?)\.?(?:\s+(\d{4}))?`)
)

// each runs re over text and keeps the matches whose surroundings make them
// a token of their own. Go's regexp has no lookaround, so a rejected match
// is retried one character further: in "04/09 196,90", "09 196,90" is
// refused for the slash before it and "196,90" is then found.
func each(re *regexp.Regexp, text string, isolated func(before, after rune) bool, fn func(m []int)) {
	for pos := 0; pos < len(text); {
		m := re.FindStringSubmatchIndex(text[pos:])
		if m == nil {
			return
		}
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
		before, after := runeBefore(text, m[0]), runeAt(text, m[1])
		if !isolated(before, after) {
			pos = m[0] + 1
			continue
		}
		fn(m)
		pos = m[1]
	}
}

func runeBefore(text string, i int) rune {
	if i == 0 {
		return ' '
	}
	r := []rune(text[:i])
	return r[len(r)-1]
}

func runeAt(text string, i int) rune {
	for _, r := range text[i:] {
		return r
	}
	return ' '
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func amountIsolated(before, after rune) bool {
	return !isWordRune(before) && before != ',' && before != '.' && before != '/' && !isWordRune(after) && after != ','
}

func dateIsolated(before, after rune) bool {
	return !unicode.IsDigit(before) && before != '/' && before != '.' && before != ',' &&
		!unicode.IsDigit(after) && after != '/' && after != ','
}

func wordIsolated(before, after rune) bool { return !isWordRune(before) && !isWordRune(after) }

func scanLine(f *Facts, index int, line pdftext.Line) {
	text, spans := lineText(line)
	each(amountRe, text, amountIsolated, func(m []int) {
		if a, ok := parseAmount(text, m); ok {
			a.At = locate(line, spans, index, m[0], m[7])
			f.Amounts = append(f.Amounts, a)
		}
	})
	each(ibanRe, text, func(before, _ rune) bool { return !isWordRune(before) }, func(m []int) {
		if v, n, ok := longestIBAN(text[m[0]:m[1]]); ok {
			f.IBANs = append(f.IBANs, IBAN{Value: v, At: locate(line, spans, index, m[0], m[0]+n)})
		}
	})
	each(dateRe, text, dateIsolated, func(m []int) {
		if d, ok := parseDate(text, m); ok {
			d.At = locate(line, spans, index, m[0], m[1])
			f.Dates = append(f.Dates, d)
		}
	})
	each(longDateRe, text, wordIsolated, func(m []int) {
		if d, ok := parseLongDate(text, m); ok {
			d.At = locate(line, spans, index, m[0], m[1])
			f.Dates = append(f.Dates, d)
		}
	})
}

func parseAmount(text string, m []int) (Amount, bool) {
	prefix := strings.TrimSpace(text[m[2]:m[3]])
	integer := text[m[4]:m[5]]
	decimals := text[m[6]:m[7]]
	suffix := ""
	if m[8] >= 0 {
		suffix = strings.TrimSpace(text[m[8]:m[9]])
	}
	groups := strings.FieldsFunc(integer, func(r rune) bool { return strings.ContainsRune(groupSepRunes, r) })
	digits := strings.Join(groups, "")
	cents, err := strconv.ParseInt(digits+decimals, 10, 64)
	if err != nil {
		return Amount{}, false
	}
	a := Amount{Cents: cents, Raw: text[m[0]:m[7]]}
	switch {
	case prefix == "-" || prefix == "−" || suffix == "-" || suffix == "DB":
		a.Sign = SignNegative
	case prefix == "+" || suffix == "CR":
		a.Sign = SignPositive
	}
	for i := 1; i < len(groups); i++ {
		if alt, err := strconv.ParseInt(strings.Join(groups[i:], "")+decimals, 10, 64); err == nil {
			a.Alternatives = append(a.Alternatives, alt)
		}
	}
	return a, true
}

// ibanLengths are the lengths the registry fixes for the countries a French
// business banks with most; any other country only has to pass the checksum.
var ibanLengths = map[string]int{
	"FR": 27, "MC": 27, "DE": 22, "BE": 16, "ES": 24, "IT": 27, "LU": 20, "NL": 18,
	"PT": 25, "CH": 21, "GB": 22, "IE": 22, "AT": 20, "LT": 20, "EE": 20, "MT": 31,
}

// longestIBAN keeps the longest prefix of the candidate that is a valid IBAN:
// a printed IBAN is often followed by a BIC or a word the pattern swallows.
func longestIBAN(candidate string) (string, int, bool) {
	fields := strings.Fields(candidate)
	for n := len(fields); n >= 1; n-- {
		v := strings.Join(fields[:n], "")
		if ValidIBAN(v) {
			return v, len(strings.Join(fields[:n], " ")), true
		}
	}
	return "", 0, false
}

// ValidIBAN checks the ISO 13616 mod-97 checksum, and the length for the
// countries ibanLengths knows. Spaces are ignored, letters must be upper case.
func ValidIBAN(iban string) bool {
	v := strings.ReplaceAll(iban, " ", "")
	if len(v) < 15 || len(v) > 34 {
		return false
	}
	for i, r := range v {
		switch {
		case i < 2 && (r < 'A' || r > 'Z'):
			return false
		case i >= 2 && i < 4 && !unicode.IsDigit(r):
			return false
		case (r < 'A' || r > 'Z') && (r < '0' || r > '9'):
			return false
		}
	}
	if want, ok := ibanLengths[v[:2]]; ok && len(v) != want {
		return false
	}
	rearranged := v[4:] + v[:4]
	rem := 0
	for _, r := range rearranged {
		if r >= 'A' {
			n := int(r-'A') + 10
			rem = (rem*100 + n) % 97
		} else {
			rem = (rem*10 + int(r-'0')) % 97
		}
	}
	return rem == 1
}

func parseDate(text string, m []int) (Date, bool) {
	day, _ := strconv.Atoi(text[m[2]:m[3]])
	month, _ := strconv.Atoi(text[m[4]:m[5]])
	year := 0
	end := m[5]
	if m[6] >= 0 {
		year, _ = strconv.Atoi(text[m[6]:m[7]])
		if year < 100 {
			year += 2000
		}
		end = m[7]
	}
	if !validDay(day, month, year) {
		return Date{}, false
	}
	return Date{Day: day, Month: month, Year: year, Raw: text[m[2]:end]}, true
}

var monthPrefixes = []string{"janv", "f", "mars", "avr", "mai", "juin", "juil", "ao", "sept", "oct", "nov", "d"}

func parseLongDate(text string, m []int) (Date, bool) {
	day, _ := strconv.Atoi(text[m[2]:m[3]])
	name := strings.ToLower(text[m[4]:m[5]])
	month := 0
	for i, p := range monthPrefixes {
		if strings.HasPrefix(name, p) {
			month = i + 1
			break
		}
	}
	year := 0
	end := m[5]
	if m[6] >= 0 {
		year, _ = strconv.Atoi(text[m[6]:m[7]])
		end = m[7]
	}
	if !validDay(day, month, year) {
		return Date{}, false
	}
	return Date{Day: day, Month: month, Year: year, Raw: text[m[2]:end]}, true
}

func validDay(day, month, year int) bool {
	if month < 1 || month > 12 || day < 1 {
		return false
	}
	days := [...]int{31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[month-1]
	if month == 2 && year != 0 && !(year%4 == 0 && (year%100 != 0 || year%400 == 0)) {
		days = 28
	}
	return day <= days
}
