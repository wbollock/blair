package recur

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

type vectors struct {
	Next []struct {
		Rule      string `json:"rule"`
		From      string `json:"from"`
		Inclusive bool   `json:"inclusive"`
		Want      string `json:"want"`
	} `json:"next"`
	Canonical []struct {
		In    string `json:"in"`
		Want  string `json:"want"`
		Error bool   `json:"error"`
	} `json:"canonical"`
}

func TestSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/recur/next.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	for _, c := range v.Next {
		r, err := Parse(c.Rule)
		if err != nil {
			t.Fatalf("%s: %v", c.Rule, err)
		}
		from, _ := time.Parse(time.DateOnly, c.From)
		if got := r.Next(from, c.Inclusive).Format(time.DateOnly); got != c.Want {
			t.Errorf("next(%s, %s, %v) = %s, want %s", c.Rule, c.From, c.Inclusive, got, c.Want)
		}
	}
	for _, c := range v.Canonical {
		r, err := Parse(c.In)
		if c.Error {
			if err == nil {
				t.Errorf("Parse(%q) accepted %s", c.In, r)
			}
			continue
		}
		if err != nil || r.String() != c.Want {
			t.Errorf("Parse(%q) = %s, %v; want %s", c.In, r, err, c.Want)
		}
	}
}

func TestPhrase(t *testing.T) {
	for in, want := range map[string]string{
		"day": "FREQ=DAILY", "weekdays": "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR", "other week": "FREQ=WEEKLY;INTERVAL=2",
		"3 months": "FREQ=MONTHLY;INTERVAL=3", "1 year": "FREQ=YEARLY", "fri,mon": "FREQ=WEEKLY;BYDAY=MO,FR", "15th": "FREQ=MONTHLY;BYMONTHDAY=15",
		"box": "", "days": "", "3 month": "", "2": "", "mon,box": "", "32nd": "",
	} {
		r, n := Phrase(splitWords(in))
		got := ""
		if n > 0 {
			got = r.String()
		}
		if got != want {
			t.Errorf("Phrase(%q) = %q, want %q", in, got, want)
		}
	}
}

func splitWords(s string) []string { return strings.Fields(s) }
