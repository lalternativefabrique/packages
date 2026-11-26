package docfacts

import "strings"

type Kind int

const (
	KindAmount Kind = iota + 1
	KindIBAN
	KindDate
)

// Claim is one fact someone says the document shows. Key is the caller's
// name for it ("closing", "line 12") and comes back untouched.
type Claim struct {
	Key   string
	Kind  Kind
	Cents int64
	IBAN  string
	Day   int
	Month int
	Year  int
}

// AmountClaim compares by absolute value: a statement prints 27,60 in its
// debit column for a movement of -27,60. Check Found[i].Amount.Sign, or the
// column, when the direction matters.
func AmountClaim(key string, cents int64) Claim {
	return Claim{Key: key, Kind: KindAmount, Cents: cents}
}

func IBANClaim(key, iban string) Claim {
	return Claim{Key: key, Kind: KindIBAN, IBAN: strings.ToUpper(strings.ReplaceAll(iban, " ", ""))}
}

// DateClaim with year 0 matches the day and month whatever year is printed;
// a printed date without a year matches any claimed year.
func DateClaim(key string, year, month, day int) Claim {
	return Claim{Key: key, Kind: KindDate, Year: year, Month: month, Day: day}
}

type Found struct {
	Claim  Claim
	At     Location
	Amount *Amount
}

// Check is the verdict on a set of claims. Unclaimed lists the amounts the
// document prints that no claim used: a movement the reading skipped shows
// up there, among headers and totals the caller is expected to expect.
type Check struct {
	Found     []Found
	Missing   []Claim
	Unclaimed []Amount
}

func (c Check) OK() bool { return len(c.Missing) == 0 }

// Match pairs each claim with a distinct fact of the document, so three
// claimed movements of 27,60 need three 27,60 printed. Claims are served in
// the order given, each taking the first unused fact in reading order; an
// amount's alternative readings are only tried once every claim has had its
// chance at a plain one.
func (f Facts) Match(claims []Claim) Check {
	var c Check
	usedAmount := make([]bool, len(f.Amounts))
	usedIBAN := make([]bool, len(f.IBANs))
	usedDate := make([]bool, len(f.Dates))
	pending := make([]int, 0, len(claims))
	found := make([]*Found, len(claims))

	for i, cl := range claims {
		switch cl.Kind {
		case KindAmount:
			if j := firstAmount(f.Amounts, usedAmount, abs(cl.Cents), false); j >= 0 {
				usedAmount[j] = true
				found[i] = &Found{Claim: cl, At: f.Amounts[j].At, Amount: &f.Amounts[j]}
				continue
			}
			pending = append(pending, i)
		case KindIBAN:
			for j, v := range f.IBANs {
				if !usedIBAN[j] && v.Value == cl.IBAN {
					usedIBAN[j] = true
					found[i] = &Found{Claim: cl, At: v.At}
					break
				}
			}
		case KindDate:
			for j, d := range f.Dates {
				if !usedDate[j] && d.Day == cl.Day && d.Month == cl.Month && (d.Year == 0 || cl.Year == 0 || d.Year == cl.Year) {
					usedDate[j] = true
					found[i] = &Found{Claim: cl, At: d.At}
					break
				}
			}
		}
	}
	for _, i := range pending {
		if j := firstAmount(f.Amounts, usedAmount, abs(claims[i].Cents), true); j >= 0 {
			usedAmount[j] = true
			found[i] = &Found{Claim: claims[i], At: f.Amounts[j].At, Amount: &f.Amounts[j]}
		}
	}

	for i, cl := range claims {
		if found[i] != nil {
			c.Found = append(c.Found, *found[i])
		} else {
			c.Missing = append(c.Missing, cl)
		}
	}
	for j, a := range f.Amounts {
		if !usedAmount[j] {
			c.Unclaimed = append(c.Unclaimed, a)
		}
	}
	return c
}

func firstAmount(amounts []Amount, used []bool, cents int64, alternatives bool) int {
	for j, a := range amounts {
		if used[j] {
			continue
		}
		if !alternatives && a.Cents == cents {
			return j
		}
		if alternatives {
			for _, alt := range a.Alternatives {
				if alt == cents {
					return j
				}
			}
		}
	}
	return -1
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
