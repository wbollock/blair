// Package recur implements the small subset of RFC 5545 RRULE that Blair's
// repeating tasks use (ADR-0013): FREQ, INTERVAL, BYDAY (weekly) and
// BYMONTHDAY (monthly). web/src/lib/recur.ts mirrors it; both must pass
// testdata/recur/next.json.
package recur

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Rule struct {
	Freq       string // DAILY, WEEKLY, MONTHLY, YEARLY
	Interval   int
	ByDay      []time.Weekday // WEEKLY only, sorted Monday first
	ByMonthDay int            // MONTHLY only, 1–31 (clamped to short months)
}

var dayCodes = []string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

// monFirst orders weekdays Monday..Sunday.
func monFirst(d time.Weekday) int { return (int(d) + 6) % 7 }

// Parse validates and normalizes an RRULE string.
func Parse(s string) (Rule, error) {
	r := Rule{Interval: 1}
	for part := range strings.SplitSeq(strings.ToUpper(strings.TrimSpace(s)), ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return r, fmt.Errorf("bad rule part %q", part)
		}
		switch k {
		case "FREQ":
			if !slices.Contains([]string{"DAILY", "WEEKLY", "MONTHLY", "YEARLY"}, v) {
				return r, errors.New("FREQ must be DAILY, WEEKLY, MONTHLY or YEARLY")
			}
			r.Freq = v
		case "INTERVAL":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 365 {
				return r, errors.New("INTERVAL must be 1–365")
			}
			r.Interval = n
		case "BYDAY":
			for code := range strings.SplitSeq(v, ",") {
				i := slices.Index(dayCodes, code)
				if i < 0 {
					return r, fmt.Errorf("bad BYDAY %q", code)
				}
				if !slices.Contains(r.ByDay, time.Weekday(i)) {
					r.ByDay = append(r.ByDay, time.Weekday(i))
				}
			}
		case "BYMONTHDAY":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 31 {
				return r, errors.New("BYMONTHDAY must be 1–31")
			}
			r.ByMonthDay = n
		default:
			return r, fmt.Errorf("unsupported rule part %s", k)
		}
	}
	switch {
	case r.Freq == "":
		return r, errors.New("FREQ is required")
	case len(r.ByDay) > 0 && r.Freq != "WEEKLY":
		return r, errors.New("BYDAY needs FREQ=WEEKLY")
	case r.ByMonthDay > 0 && r.Freq != "MONTHLY":
		return r, errors.New("BYMONTHDAY needs FREQ=MONTHLY")
	}
	slices.SortFunc(r.ByDay, func(a, b time.Weekday) int { return monFirst(a) - monFirst(b) })
	return r, nil
}

// String is the canonical form: FREQ;INTERVAL (if >1);BYDAY;BYMONTHDAY.
func (r Rule) String() string {
	s := "FREQ=" + r.Freq
	if r.Interval > 1 {
		s += ";INTERVAL=" + strconv.Itoa(r.Interval)
	}
	if len(r.ByDay) > 0 {
		codes := make([]string, len(r.ByDay))
		for i, d := range r.ByDay {
			codes[i] = dayCodes[d]
		}
		s += ";BYDAY=" + strings.Join(codes, ",")
	}
	if r.ByMonthDay > 0 {
		s += ";BYMONTHDAY=" + strconv.Itoa(r.ByMonthDay)
	}
	return s
}

// clampDay is day d of the month, or the month's last day if it is shorter.
func clampDay(y int, m time.Month, d int) time.Time {
	last := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(y, m, min(d, last), 0, 0, 0, 0, time.UTC)
}

func addMonths(d time.Time, n, day int) time.Time {
	first := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, n, 0)
	return clampDay(first.Year(), first.Month(), day)
}

// Next is the first occurrence strictly after from, or on/after it when inclusive.
// Dates are calendar dates (midnight UTC).
func (r Rule) Next(from time.Time, inclusive bool) time.Time {
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	iv := max(r.Interval, 1)
	switch r.Freq {
	case "DAILY":
		if inclusive {
			return from
		}
		return from.AddDate(0, 0, iv)
	case "WEEKLY":
		if len(r.ByDay) == 0 {
			if inclusive {
				return from
			}
			return from.AddDate(0, 0, 7*iv)
		}
		start := from
		if !inclusive {
			start = from.AddDate(0, 0, 1)
		}
		on := func(t time.Time) bool { return slices.Contains(r.ByDay, t.Weekday()) }
		if iv == 1 {
			for k := range 7 {
				if t := start.AddDate(0, 0, k); on(t) {
					return t
				}
			}
		}
		// Every n weeks: the rest of from's week, then the first match n weeks on.
		week := from.AddDate(0, 0, -monFirst(from.Weekday()))
		for t := start; t.Before(week.AddDate(0, 0, 7)); t = t.AddDate(0, 0, 1) {
			if on(t) {
				return t
			}
		}
		next := week.AddDate(0, 0, 7*iv)
		for k := range 7 {
			if t := next.AddDate(0, 0, k); on(t) {
				return t
			}
		}
	case "MONTHLY":
		if r.ByMonthDay == 0 {
			if inclusive {
				return from
			}
			return addMonths(from, iv, from.Day())
		}
		if c := clampDay(from.Year(), from.Month(), r.ByMonthDay); c.After(from) || (inclusive && c.Equal(from)) {
			return c
		}
		return addMonths(from, iv, r.ByMonthDay)
	case "YEARLY":
		if inclusive {
			return from
		}
		return addMonths(from, 12*iv, from.Day())
	}
	return from
}

var weekdayWords = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday, "mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
	"wed": time.Wednesday, "weds": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday, "sat": time.Saturday, "saturday": time.Saturday,
}

var units = map[string]string{
	"day": "DAILY", "days": "DAILY", "week": "WEEKLY", "weeks": "WEEKLY",
	"month": "MONTHLY", "months": "MONTHLY", "year": "YEARLY", "years": "YEARLY",
}

// Phrase reads the words after "every" (lowercase): day, weekday, week, month,
// year, other <unit>, <n> <units>, mon[,thu…], or 1st…31st. It returns the rule
// and how many words it used, or n == 0 when the words are not a schedule.
func Phrase(words []string) (Rule, int) {
	if len(words) == 0 {
		return Rule{}, 0
	}
	w := words[0]
	if w == "weekday" || w == "weekdays" {
		return Rule{Freq: "WEEKLY", Interval: 1, ByDay: []time.Weekday{1, 2, 3, 4, 5}}, 1
	}
	if f, ok := units[w]; ok && !strings.HasSuffix(w, "s") {
		return Rule{Freq: f, Interval: 1}, 1
	}
	if len(words) > 1 {
		if f, ok := units[words[1]]; ok {
			if w == "other" && !strings.HasSuffix(words[1], "s") {
				return Rule{Freq: f, Interval: 2}, 2
			}
			if n, err := strconv.Atoi(w); err == nil && n >= 1 && n <= 365 && (n == 1) == !strings.HasSuffix(words[1], "s") {
				return Rule{Freq: f, Interval: n}, 2
			}
		}
	}
	var days []time.Weekday
	for part := range strings.SplitSeq(w, ",") {
		d, ok := weekdayWords[part]
		if !ok {
			days = nil
			break
		}
		if !slices.Contains(days, d) {
			days = append(days, d)
		}
	}
	if len(days) > 0 {
		slices.SortFunc(days, func(a, b time.Weekday) int { return monFirst(a) - monFirst(b) })
		return Rule{Freq: "WEEKLY", Interval: 1, ByDay: days}, 1
	}
	for _, suf := range []string{"st", "nd", "rd", "th"} {
		if n, err := strconv.Atoi(strings.TrimSuffix(w, suf)); err == nil && strings.HasSuffix(w, suf) && n >= 1 && n <= 31 {
			return Rule{Freq: "MONTHLY", Interval: 1, ByMonthDay: n}, 1
		}
	}
	return Rule{}, 0
}
