// Package redact keeps bank details out of what an app stores, logs or sends
// to a model: an IBAN is reduced to its last four characters in a text, and a
// PDF is rebuilt from its words with every IBAN grayed out.
package redact

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/lalternative/packages/go/docfacts"
)

const Hidden = "••••"

// lengths is the SWIFT IBAN registry: every country that issues IBANs and the
// one length its IBANs have.
var lengths = map[string]int{
	"AD": 24, "AE": 23, "AL": 28, "AT": 20, "AZ": 28, "BA": 20, "BE": 16, "BG": 22, "BH": 22, "BI": 27,
	"BR": 29, "BY": 28, "CH": 21, "CR": 22, "CY": 28, "CZ": 24, "DE": 22, "DJ": 27, "DK": 18, "DO": 28,
	"EE": 20, "EG": 29, "ES": 24, "FI": 18, "FK": 18, "FO": 18, "FR": 27, "GB": 22, "GE": 22, "GI": 23,
	"GL": 18, "GR": 27, "GT": 28, "HN": 28, "HR": 21, "HU": 28, "IE": 22, "IL": 23, "IQ": 23, "IS": 26,
	"IT": 27, "JO": 30, "KW": 30, "KZ": 20, "LB": 28, "LC": 32, "LI": 21, "LT": 20, "LU": 20, "LV": 21,
	"LY": 25, "MC": 27, "MD": 24, "ME": 22, "MK": 19, "MN": 20, "MR": 27, "MT": 31, "MU": 30, "NI": 28,
	"NL": 18, "NO": 15, "OM": 23, "PK": 24, "PL": 28, "PS": 29, "PT": 25, "QA": 29, "RO": 24, "RS": 22,
	"RU": 33, "SA": 24, "SC": 31, "SD": 18, "SE": 24, "SI": 19, "SK": 24, "SM": 27, "SO": 23, "ST": 25,
	"SV": 28, "TL": 23, "TN": 24, "TR": 26, "UA": 29, "VA": 22, "VG": 24, "XK": 20, "YE": 30,
}

// minFragmentDigits is how many digits after the check digits make a run
// that starts like an IBAN but fails its checksum (cut short, half masked,
// misread) a fragment of one. A SEPA creditor identifier (FR12ZZZ123456)
// carries six and stays readable.
const minFragmentDigits = 10

const maxFragmentChars = 34

// Span is where an IBAN, or a fragment of one, sits in a text, in bytes.
// Last4 is the IBAN's last four characters, empty for a fragment.
type Span struct {
	Start, End int
	Last4      string
	compact    string
}

// FindIBANs lists every IBAN in s, glued to a word, spaced any way, or cut
// short, in order.
func FindIBANs(s string) []Span {
	var out []Span
	for i := 0; i < len(s); {
		if end, compact, ok := validAt(s, i); ok {
			out = append(out, Span{Start: i, End: end, Last4: compact[len(compact)-4:], compact: compact})
			i = end
			continue
		}
		if end, ok := fragmentAt(s, i); ok {
			out = append(out, Span{Start: i, End: end})
			i = end
			continue
		}
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
	}
	return out
}

// IBANs replaces every IBAN in s by "••••" and its last four characters, and
// every fragment of one by "••••" alone. It is idempotent.
func IBANs(s string) string {
	return apply(s, FindIBANs(s))
}

// Text masks every IBAN of s like IBANs, and also the account numbers those
// IBANs carry wherever s prints them alone ("COMPTE N° 35619046200").
func Text(s string) string {
	ibans := FindIBANs(s)
	return apply(s, merge(ibans, accountsOf(ibans).spans(s, ibans)))
}

func apply(s string, spans []Span) string {
	if len(spans) == 0 {
		return s
	}
	var b strings.Builder
	last := 0
	for _, sp := range spans {
		b.WriteString(s[last:sp.Start])
		b.WriteString(Hidden + sp.Last4)
		last = sp.End
	}
	b.WriteString(s[last:])
	return b.String()
}

// minAccountDigits is the shortest run of digits taken for an account
// number when an IBAN of the document holds it: long enough that no date,
// amount or postcode qualifies.
const minAccountDigits = 7

// accounts are the digit runs of the basic bank account numbers inside the
// IBANs a document prints: bank, branch, account number and key.
type accounts []string

func accountsOf(ibans []Span) accounts {
	var out accounts
	for _, sp := range ibans {
		for _, run := range digitRuns(sp.compact[min(4, len(sp.compact)):]) {
			if run[1]-run[0] >= minAccountDigits {
				out = append(out, sp.compact[4+run[0]:4+run[1]])
			}
		}
	}
	return out
}

// spans finds, outside the IBANs themselves, every run of digits that is
// part of one of the document's account numbers.
func (a accounts) spans(s string, ibans []Span) []Span {
	if len(a) == 0 {
		return nil
	}
	var out []Span
	for _, run := range digitRuns(s) {
		start, end := run[0], run[1]
		if end-start < minAccountDigits || inside(ibans, start, end) {
			continue
		}
		for _, acc := range a {
			if strings.Contains(acc, s[start:end]) {
				out = append(out, Span{Start: start, End: end})
				break
			}
		}
	}
	return out
}

func digitRuns(s string) [][2]int {
	var out [][2]int
	for i := 0; i < len(s); {
		if !isDigit(s[i]) {
			i++
			continue
		}
		j := i
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		out = append(out, [2]int{i, j})
		i = j
	}
	return out
}

func inside(spans []Span, start, end int) bool {
	for _, sp := range spans {
		if start < sp.End && sp.Start < end {
			return true
		}
	}
	return false
}

func merge(a, b []Span) []Span {
	out := append(append([]Span(nil), a...), b...)
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// ContainsIBAN reports whether IBANs would change s.
func ContainsIBAN(s string) bool { return len(FindIBANs(s)) > 0 }

func validAt(s string, i int) (int, string, bool) {
	country, ok := prefixAt(s, i, true)
	if !ok {
		return 0, "", false
	}
	want := lengths[country]
	var compact strings.Builder
	j, n := i, 0
	for n < want && j < len(s) {
		if c := s[j]; isAlnum(c) {
			compact.WriteByte(upper(c))
			n++
			j++
			continue
		}
		w := separatorAt(s, j)
		if w == 0 || n < 4 || j+w >= len(s) || !isAlnum(s[j+w]) {
			break
		}
		j += w
	}
	if n != want || !docfacts.ValidIBAN(compact.String()) {
		return 0, "", false
	}
	return j, compact.String(), true
}

// fragmentAt reads, from an upper-case country code and two check digits,
// the groups that carry a digit (the first one may be a bank's letters), and
// takes them for an IBAN fragment when they hold enough digits.
func fragmentAt(s string, i int) (int, bool) {
	if _, ok := prefixAt(s, i, false); !ok {
		return 0, false
	}
	j := i + 4
	end, chars, digits := j, 0, 0
	for group := 0; chars < maxFragmentChars; group++ {
		k := j + separatorAt(s, j)
		start, groupDigits := k, 0
		for k < len(s) && isAlnum(s[k]) {
			if isDigit(s[k]) {
				groupDigits++
			}
			k++
		}
		if k == start || (groupDigits == 0 && group > 0) || decimalAt(s, k) {
			break
		}
		chars += k - start
		digits += groupDigits
		end, j = k, k
	}
	return end, digits >= minFragmentDigits
}

// decimalAt tells an amount that follows an IBAN fragment ("01 3,10") from
// one more group of the fragment.
func decimalAt(s string, i int) bool {
	return i+1 < len(s) && (s[i] == ',' || s[i] == '.') && isDigit(s[i+1])
}

func prefixAt(s string, i int, anyCase bool) (string, bool) {
	if i+4 > len(s) {
		return "", false
	}
	a, b := s[i], s[i+1]
	if !anyCase && (!isUpper(a) || !isUpper(b)) {
		return "", false
	}
	if !isLetter(a) || !isLetter(b) || !isDigit(s[i+2]) || !isDigit(s[i+3]) {
		return "", false
	}
	country := string([]byte{upper(a), upper(b)})
	_, ok := lengths[country]
	return country, ok
}

// separatorAt is the width of the one separator a printed IBAN may carry
// between two of its characters: a space, a non-breaking one, a dash, or the
// underscore a file name puts in place of a space.
func separatorAt(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	switch s[i] {
	case ' ', '-', '_', '\t':
		return 1
	}
	r, w := utf8.DecodeRuneInString(s[i:])
	if r == ' ' || r == ' ' || r == ' ' {
		return w
	}
	return 0
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isUpper(c byte) bool  { return c >= 'A' && c <= 'Z' }
func isLetter(c byte) bool { return isUpper(c) || (c >= 'a' && c <= 'z') }
func isAlnum(c byte) bool  { return isDigit(c) || isLetter(c) }

func upper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 'a' + 'A'
	}
	return c
}
