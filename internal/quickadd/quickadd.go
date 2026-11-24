// Package quickadd parses quick-add syntax (SPEC §5.1).
//
// The grammar is shared with web/src/lib/quickadd.ts; both must pass
// testdata/quickadd/cases.json. Change a case there before changing behavior.
package quickadd

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ErrEmptyTitle is returned when only tokens were entered.
var ErrEmptyTitle = errors.New("empty_title")

type Project struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
}

// Context is everything names and dates resolve against.
type Context struct {
	Now            time.Time
	Loc            *time.Location
	WeekStart      time.Weekday
	Projects       []Project
	Labels         []string
	CurrentProject string
}

// Token is one recognized piece of syntax, with byte offsets into the input.
type Token struct {
	Kind  string `json:"kind"` // project, column, label, priority, date
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Value string `json:"value"`
}

type Result struct {
	Title      string   `json:"title"`
	Project    string   `json:"project,omitempty"`
	ProjectNew string   `json:"project_new,omitempty"`
	Column     string   `json:"column,omitempty"`
	Priority   *int     `json:"priority,omitempty"`
	DueDate    string   `json:"due_date,omitempty"`
	DueTime    string   `json:"due_time,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	LabelsNew  []string `json:"labels_new,omitempty"`
	Tokens     []Token  `json:"tokens"`
}

type word struct {
	text       string // as typed, minus a leading escape backslash
	start, end int
	escaped    bool
}

func split(input string) []word {
	var out []word
	i := 0
	for i < len(input) {
		for i < len(input) && (input[i] == ' ' || input[i] == '\t' || input[i] == '\n') {
			i++
		}
		if i >= len(input) {
			break
		}
		start := i
		if strings.HasPrefix(input[i:], `#"`) {
			if j := strings.IndexByte(input[i+2:], '"'); j >= 0 {
				end := i + 2 + j + 1
				out = append(out, word{text: input[start:end], start: start, end: end})
				i = end
				continue
			}
		}
		for i < len(input) && input[i] != ' ' && input[i] != '\t' && input[i] != '\n' {
			i++
		}
		w := word{text: input[start:i], start: start, end: i}
		if strings.HasPrefix(w.text, `\`) && len(w.text) > 1 {
			w.text, w.escaped = w.text[1:], true
		}
		out = append(out, w)
	}
	return out
}

var (
	reName     = regexp.MustCompile(`^[\p{L}\p{N}_.\-]+$`)
	reInUnit   = regexp.MustCompile(`^(\d+)(d|w|m)$`)
	reISO      = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	reSlash    = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})(?:/(\d{4}))?$`)
	reDayNum   = regexp.MustCompile(`^(\d{1,2})(?:st|nd|rd|th)?$`)
	reClock24  = regexp.MustCompile(`^([01]?\d|2[0-3]):([0-5]\d)$`)
	reClock12  = regexp.MustCompile(`^(1[0-2]|0?[1-9])(?::([0-5]\d))?(am|pm)$`)
	reBareHour = regexp.MustCompile(`^(1[0-2]|0?[1-9])(?::([0-5]\d))?$`)
)

var priorities = map[string]int{
	"1": 1, "2": 2, "3": 3, "4": 4,
	"low": 1, "med": 2, "medium": 2, "high": 3, "urgent": 4,
}

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday,
	"mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
	"wed": time.Wednesday, "weds": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday,
}

var months = map[string]time.Month{
	"jan": 1, "january": 1, "feb": 2, "february": 2, "mar": 3, "march": 3,
	"apr": 4, "april": 4, "may": 5, "jun": 6, "june": 6, "jul": 7, "july": 7,
	"aug": 8, "august": 8, "sep": 9, "sept": 9, "september": 9,
	"oct": 10, "october": 10, "nov": 11, "november": 11, "dec": 12, "december": 12,
}

// Parse parses input against ctx.
func Parse(input string, ctx Context) (Result, error) {
	if ctx.Loc == nil {
		ctx.Loc = time.UTC
	}
	now := ctx.Now.In(ctx.Loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, ctx.Loc)

	words := split(input)
	used := make([]bool, len(words))
	res := Result{Tokens: []Token{}}
	var columnWord = -1
	var project *Project

	tok := func(kind string, from, to int, value string) {
		res.Tokens = append(res.Tokens, Token{Kind: kind, Text: input[words[from].start:words[to].end], Start: words[from].start, End: words[to].end, Value: value})
		for k := from; k <= to; k++ {
			used[k] = true
		}
	}

	for i := 0; i < len(words); i++ {
		w := words[i]
		if w.escaped || used[i] || len(w.text) < 2 {
			continue
		}
		lw := strings.ToLower(w.text)
		switch {
		case w.text[0] == '#' && res.Project == "" && res.ProjectNew == "":
			name := w.text[1:]
			if strings.HasPrefix(name, `"`) && strings.HasSuffix(name, `"`) && len(name) >= 2 {
				name = name[1 : len(name)-1]
			} else if !reName.MatchString(name) {
				continue
			}
			if name == "" {
				continue
			}
			if p := matchProject(ctx.Projects, name); p != nil {
				project = p
				res.Project = p.Name
				tok("project", i, i, p.Name)
			} else {
				res.ProjectNew = name
				tok("project", i, i, name)
			}
			continue
		case w.text[0] == '/' && columnWord < 0 && reName.MatchString(w.text[1:]):
			columnWord = i // resolved once the project is known
			continue
		case w.text[0] == '@' && reName.MatchString(w.text[1:]):
			name := w.text[1:]
			if canon, ok := matchName(ctx.Labels, name); ok {
				res.Labels = append(res.Labels, canon)
				tok("label", i, i, canon)
			} else {
				res.LabelsNew = append(res.LabelsNew, name)
				tok("label", i, i, name)
			}
			continue
		case w.text[0] == '!' && res.Priority == nil:
			if p, ok := priorities[lw[1:]]; ok {
				res.Priority = &p
				tok("priority", i, i, strconv.Itoa(p))
			}
			continue
		}
		if res.DueDate != "" {
			continue
		}
		if strings.HasPrefix(lw, "due:") {
			// Explicit prefix: the date phrase starts inside this word.
			rest := lw[4:]
			seq := []string{rest}
			for k := i + 1; k < len(words) && k < i+4; k++ {
				seq = append(seq, strings.ToLower(words[k].text))
			}
			if rest == "" {
				seq = seq[1:]
			}
			if d, tm, n := matchDate(seq, today, ctx.WeekStart); n > 0 {
				last := i + n - 1
				if rest == "" {
					last = i + n
				}
				t2, m := matchTime(lowerFrom(words, last+1))
				if m > 0 {
					tm, last = t2, last+m
				}
				res.DueDate, res.DueTime = d, tm
				tok("date", i, last, d)
			}
			continue
		}
		if d, tm, n := matchDate(lowerFrom(words, i), today, ctx.WeekStart); n > 0 && !anyUsed(used, i, i+n) {
			last := i + n - 1
			if t2, m := matchTime(lowerFrom(words, last+1)); m > 0 {
				tm, last = t2, last+m
			}
			res.DueDate, res.DueTime = d, tm
			tok("date", i, last, d)
			i = last
		}
	}

	if columnWord >= 0 {
		target := project
		if target == nil && res.ProjectNew == "" && ctx.CurrentProject != "" {
			target = matchProject(ctx.Projects, ctx.CurrentProject)
			if target != nil {
				res.Project = target.Name
			}
		}
		if target != nil {
			if canon, ok := matchName(target.Columns, words[columnWord].text[1:]); ok {
				res.Column = canon
				tok("column", columnWord, columnWord, canon)
			}
		}
	}

	var title []string
	for i, w := range words {
		if !used[i] {
			title = append(title, w.text)
		}
	}
	res.Title = strings.Join(title, " ")
	if strings.TrimSpace(res.Title) == "" {
		return res, ErrEmptyTitle
	}
	return res, nil
}

func lowerFrom(words []word, i int) []string {
	var out []string
	for k := i; k < len(words) && k < i+4; k++ {
		if words[k].escaped {
			break
		}
		out = append(out, strings.ToLower(words[k].text))
	}
	return out
}

func anyUsed(used []bool, from, to int) bool {
	for k := from; k < to && k < len(used); k++ {
		if used[k] {
			return true
		}
	}
	return false
}

func normalize(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '-' || r == '_' {
			return -1
		}
		return unicode.ToLower(r)
	}, s)
}

func matchName(names []string, q string) (string, bool) {
	for _, n := range names {
		if strings.EqualFold(n, q) {
			return n, true
		}
	}
	nq := normalize(q)
	for _, n := range names {
		if normalize(n) == nq {
			return n, true
		}
	}
	var hit string
	count := 0
	for _, n := range names {
		if strings.HasPrefix(normalize(n), nq) {
			hit, count = n, count+1
		}
	}
	return hit, count == 1
}

func matchProject(ps []Project, q string) *Project {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name
	}
	canon, ok := matchName(names, q)
	if !ok {
		return nil
	}
	for i := range ps {
		if ps[i].Name == canon {
			return &ps[i]
		}
	}
	return nil
}

func fmtDate(t time.Time) string { return t.Format("2006-01-02") }

// matchDate recognizes a date phrase at the start of ws. It returns the date,
// an implied time (for "tonight") and how many words were consumed.
func matchDate(ws []string, today time.Time, weekStart time.Weekday) (date, tm string, n int) {
	if len(ws) == 0 {
		return "", "", 0
	}
	w0 := ws[0]
	switch w0 {
	case "today":
		return fmtDate(today), "", 1
	case "tonight":
		return fmtDate(today), "20:00", 1
	case "tomorrow", "tmr", "tmrw":
		return fmtDate(today.AddDate(0, 0, 1)), "", 1
	}
	if wd, ok := weekdays[w0]; ok {
		diff := (int(wd) - int(today.Weekday()) + 7) % 7
		if diff == 0 {
			diff = 7
		}
		return fmtDate(today.AddDate(0, 0, diff)), "", 1
	}
	if w0 == "next" && len(ws) > 1 {
		if wd, ok := weekdays[ws[1]]; ok {
			sinceStart := (int(today.Weekday()) - int(weekStart) + 7) % 7
			nextWeek := today.AddDate(0, 0, 7-sinceStart)
			off := (int(wd) - int(weekStart) + 7) % 7
			return fmtDate(nextWeek.AddDate(0, 0, off)), "", 2
		}
		if ws[1] == "week" {
			return fmtDate(today.AddDate(0, 0, 7)), "", 2
		}
	}
	if w0 == "in" && len(ws) > 1 {
		if m := reInUnit.FindStringSubmatch(ws[1]); m != nil {
			k, _ := strconv.Atoi(m[1])
			return addUnit(today, k, m[2]), "", 2
		}
		if k, err := strconv.Atoi(ws[1]); err == nil && len(ws) > 2 {
			switch ws[2] {
			case "day", "days":
				return addUnit(today, k, "d"), "", 3
			case "week", "weeks":
				return addUnit(today, k, "w"), "", 3
			case "month", "months":
				return addUnit(today, k, "m"), "", 3
			}
		}
	}
	if m := reISO.FindStringSubmatch(w0); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		if t, ok := validDate(y, mo, d, today.Location()); ok {
			return fmtDate(t), "", 1
		}
	}
	if m := reSlash.FindStringSubmatch(w0); m != nil {
		mo, _ := strconv.Atoi(m[1])
		d, _ := strconv.Atoi(m[2])
		if m[3] != "" {
			y, _ := strconv.Atoi(m[3])
			if t, ok := validDate(y, mo, d, today.Location()); ok {
				return fmtDate(t), "", 1
			}
		} else if t, ok := rollForward(today, time.Month(mo), d); ok {
			return fmtDate(t), "", 1
		}
	}
	if mo, ok := months[w0]; ok && len(ws) > 1 {
		if m := reDayNum.FindStringSubmatch(ws[1]); m != nil {
			d, _ := strconv.Atoi(m[1])
			if t, ok := rollForward(today, mo, d); ok {
				return fmtDate(t), "", 2
			}
		}
	}
	if m := reDayNum.FindStringSubmatch(w0); m != nil && len(ws) > 1 {
		if mo, ok := months[ws[1]]; ok {
			d, _ := strconv.Atoi(m[1])
			if t, ok := rollForward(today, mo, d); ok {
				return fmtDate(t), "", 2
			}
		}
	}
	return "", "", 0
}

func addUnit(t time.Time, k int, unit string) string {
	switch unit {
	case "w":
		return fmtDate(t.AddDate(0, 0, 7*k))
	case "m":
		return fmtDate(t.AddDate(0, k, 0))
	default:
		return fmtDate(t.AddDate(0, 0, k))
	}
}

func validDate(y, mo, d int, loc *time.Location) (time.Time, bool) {
	if mo < 1 || mo > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, loc)
	return t, t.Day() == d
}

// rollForward picks this year's month/day, or next year's if it already passed.
func rollForward(today time.Time, mo time.Month, d int) (time.Time, bool) {
	t, ok := validDate(today.Year(), int(mo), d, today.Location())
	if !ok {
		return t, false
	}
	if t.Before(today) {
		t, ok = validDate(today.Year()+1, int(mo), d, today.Location())
	}
	return t, ok
}

// matchTime recognizes "at 5pm", "9am", "17:00", "at 5:30 pm" at the start of ws.
func matchTime(ws []string) (string, int) {
	off := 0
	if len(ws) > 0 && ws[0] == "at" {
		off = 1
	}
	if len(ws) <= off {
		return "", 0
	}
	w := ws[off]
	if m := reClock24.FindStringSubmatch(w); m != nil {
		h, _ := strconv.Atoi(m[1])
		return pad(h) + ":" + m[2], off + 1
	}
	if m := reClock12.FindStringSubmatch(w); m != nil {
		return clock12(m[1], m[2], m[3]), off + 1
	}
	if m := reBareHour.FindStringSubmatch(w); m != nil && len(ws) > off+1 && (ws[off+1] == "am" || ws[off+1] == "pm") {
		return clock12(m[1], m[2], ws[off+1]), off + 2
	}
	return "", 0
}

func clock12(hs, ms, ampm string) string {
	h, _ := strconv.Atoi(hs)
	if ms == "" {
		ms = "00"
	}
	if ampm == "pm" && h != 12 {
		h += 12
	}
	if ampm == "am" && h == 12 {
		h = 0
	}
	return pad(h) + ":" + ms
}

func pad(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
