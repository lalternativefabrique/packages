package domain

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Parsed is what one typed line says: what kind of moment, which day, which
// time, and what is left as its title.
type Parsed struct {
	Kind      Kind
	KindFound bool
	Day       time.Time
	DayFound  bool
	Hour      int
	Minute    int
	HasTime   bool
	Title     string
}

const (
	sep = `(?:^|[\s,.;])`
	end = `(?:$|[\s,.;])`
)

var (
	kindPatterns = []struct {
		kind Kind
		re   *regexp.Regexp
	}{
		{KindReminder, regexp.MustCompile(`(?i)^\s*(?:rappelle[- ]moi(?:\s+(?:de|d'|d’))?|rappel|n['’]oublie pas(?:\s+(?:de|d'|d’))?|penser à)` + end)},
		{KindMail, regexp.MustCompile(`(?i)^\s*(?:e-?mail|mail|écrire|envoyer(?:\s+un\s+mail)?)(?:\s+à)?` + end)},
		{KindNote, regexp.MustCompile(`(?i)^\s*(?:note|noter)(?:\s+(?:sur|que))?` + end)},
		{KindAppointment, regexp.MustCompile(`(?i)^\s*(?:rdv|rendez-vous)(?:\s+(?:avec|chez))?` + end)},
	}
	timePattern    = regexp.MustCompile(`(?i)` + sep + `(?:à\s+)?(\d{1,2})\s*(?:h|:)\s*(\d{2})?` + end)
	noonPattern    = regexp.MustCompile(`(?i)` + sep + `(?:à\s+)?(midi|minuit)` + end)
	relativeDays   = regexp.MustCompile(`(?i)` + sep + `(aujourd['’]hui|après-demain|apres-demain|demain)` + end)
	weekdayPattern = regexp.MustCompile(`(?i)` + sep + `(lundi|mardi|mercredi|jeudi|vendredi|samedi|dimanche)(?:\s+prochain)?` + end)
	datePattern    = regexp.MustCompile(`(?i)` + sep + `(?:le\s+)?(\d{1,2})(?:\s+(janvier|février|fevrier|mars|avril|mai|juin|juillet|août|aout|septembre|octobre|novembre|décembre|decembre)|/(\d{1,2}))` + end)
	dayOfPattern   = regexp.MustCompile(`(?i)` + sep + `le\s+(\d{1,2})(?:er)?` + end)
	leadingFiller  = regexp.MustCompile(`(?i)^(?:(?:à|a|de|le|la|pour)\s+|d['’])`)
	spaces         = regexp.MustCompile(`\s+`)
)

var weekdays = map[string]time.Weekday{
	"dimanche": time.Sunday, "lundi": time.Monday, "mardi": time.Tuesday, "mercredi": time.Wednesday,
	"jeudi": time.Thursday, "vendredi": time.Friday, "samedi": time.Saturday,
}

var months = map[string]time.Month{
	"janvier": time.January, "février": time.February, "fevrier": time.February, "mars": time.March,
	"avril": time.April, "mai": time.May, "juin": time.June, "juillet": time.July, "août": time.August,
	"aout": time.August, "septembre": time.September, "octobre": time.October, "novembre": time.November,
	"décembre": time.December, "decembre": time.December,
}

// Parse reads a line such as « Dentiste jeudi 16h » in the person's zone.
// A day already gone this month or year is the next one; a weekday is the
// next one to come, never today.
func Parse(line string, now time.Time, loc *time.Location) Parsed {
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	p := Parsed{Kind: KindAppointment, Day: today}
	rest := " " + line + " "

	for _, k := range kindPatterns {
		if at := k.re.FindStringIndex(strings.TrimSpace(rest)); at != nil {
			p.Kind, p.KindFound = k.kind, true
			rest = " " + strings.TrimSpace(rest)[at[1]:] + " "
			break
		}
	}

	if m := timePattern.FindStringSubmatchIndex(rest); m != nil {
		p.Hour, _ = strconv.Atoi(rest[m[2]:m[3]])
		if m[4] >= 0 {
			p.Minute, _ = strconv.Atoi(rest[m[4]:m[5]])
		}
		if p.Hour < 24 && p.Minute < 60 {
			p.HasTime = true
			rest = cut(rest, m)
		}
	} else if m := noonPattern.FindStringSubmatchIndex(rest); m != nil {
		p.HasTime = true
		if strings.EqualFold(rest[m[2]:m[3]], "midi") {
			p.Hour = 12
		}
		rest = cut(rest, m)
	}

	if m := relativeDays.FindStringSubmatchIndex(rest); m != nil {
		switch word := strings.ToLower(rest[m[2]:m[3]]); {
		case strings.HasPrefix(word, "apr"):
			p.Day = today.AddDate(0, 0, 2)
		case word == "demain":
			p.Day = today.AddDate(0, 0, 1)
		}
		p.DayFound = true
		rest = cut(rest, m)
	} else if m := weekdayPattern.FindStringSubmatchIndex(rest); m != nil {
		ahead := (int(weekdays[strings.ToLower(rest[m[2]:m[3]])]) - int(today.Weekday()) + 7) % 7
		if ahead == 0 {
			ahead = 7
		}
		p.Day, p.DayFound = today.AddDate(0, 0, ahead), true
		rest = cut(rest, m)
	} else if m := datePattern.FindStringSubmatchIndex(rest); m != nil {
		d, _ := strconv.Atoi(rest[m[2]:m[3]])
		var month time.Month
		if m[4] >= 0 {
			month = months[strings.ToLower(rest[m[4]:m[5]])]
		} else {
			n, _ := strconv.Atoi(rest[m[6]:m[7]])
			month = time.Month(n)
		}
		if day, ok := dateOf(today, month, d); ok {
			p.Day, p.DayFound = day, true
			rest = cut(rest, m)
		}
	} else if m := dayOfPattern.FindStringSubmatchIndex(rest); m != nil {
		d, _ := strconv.Atoi(rest[m[2]:m[3]])
		from := today
		if d < today.Day() {
			from = time.Date(today.Year(), today.Month()+1, 1, 0, 0, 0, 0, loc)
		}
		if day, ok := dateOf(from, from.Month(), d); ok {
			p.Day, p.DayFound = day, true
			rest = cut(rest, m)
		}
	}

	p.Title = title(rest)
	return p
}

// dateOf places day d of month in the year to come from from.
func dateOf(from time.Time, month time.Month, d int) (time.Time, bool) {
	if month < time.January || month > time.December || d < 1 || d > 31 {
		return time.Time{}, false
	}
	day := time.Date(from.Year(), month, d, 0, 0, 0, 0, from.Location())
	if day.Before(from) {
		day = time.Date(from.Year()+1, month, d, 0, 0, 0, 0, from.Location())
	}
	if day.Day() != d {
		return time.Time{}, false
	}
	return day, true
}

func cut(s string, m []int) string {
	return s[:m[0]] + " " + s[m[1]:]
}

func title(rest string) string {
	t := strings.TrimSpace(spaces.ReplaceAllString(rest, " "))
	t = strings.Trim(t, " ,.;-")
	t = strings.TrimSpace(leadingFiller.ReplaceAllString(t, ""))
	if t == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(t)
	return string(unicode.ToUpper(r)) + t[size:]
}
