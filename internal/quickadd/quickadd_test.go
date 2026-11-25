package quickadd

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

type vectorFile struct {
	Context struct {
		Now       string    `json:"now"`
		TZ        string    `json:"tz"`
		WeekStart string    `json:"week_start"`
		Projects  []Project `json:"projects"`
		Labels    []string  `json:"labels"`
	} `json:"context"`
	Cases []struct {
		Name           string `json:"name"`
		Input          string `json:"input"`
		ContextProject string `json:"context_project"`
		Expect         struct {
			Title      string   `json:"title"`
			Project    string   `json:"project"`
			ProjectNew string   `json:"project_new"`
			Column     string   `json:"column"`
			ColumnNew  string   `json:"column_new"`
			Priority   *int     `json:"priority"`
			DueDate    string   `json:"due_date"`
			DueTime    string   `json:"due_time"`
			Recurrence string   `json:"recurrence"`
			Labels     []string `json:"labels"`
			Error      string   `json:"error"`
		} `json:"expect"`
	} `json:"cases"`
}

func loadVectors(t testing.TB) (vectorFile, Context) {
	raw, err := os.ReadFile("../../testdata/quickadd/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation(vf.Context.TZ)
	if err != nil {
		t.Fatal(err)
	}
	now, err := time.Parse(time.RFC3339, vf.Context.Now)
	if err != nil {
		t.Fatal(err)
	}
	ws := time.Monday
	if vf.Context.WeekStart == "sunday" {
		ws = time.Sunday
	}
	return vf, Context{Now: now, Loc: loc, WeekStart: ws, Projects: vf.Context.Projects, Labels: vf.Context.Labels}
}

func TestSharedVectors(t *testing.T) {
	vf, base := loadVectors(t)
	for _, c := range vf.Cases {
		t.Run(c.Name, func(t *testing.T) {
			ctx := base
			ctx.CurrentProject = c.ContextProject
			got, err := Parse(c.Input, ctx)
			if c.Expect.Error != "" {
				if err == nil || err.Error() != c.Expect.Error {
					t.Fatalf("error = %v, want %s", err, c.Expect.Error)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			check := func(field, got, want string) {
				if got != want {
					t.Errorf("%s = %q, want %q", field, got, want)
				}
			}
			check("title", got.Title, c.Expect.Title)
			check("project", got.Project, c.Expect.Project)
			check("project_new", got.ProjectNew, c.Expect.ProjectNew)
			check("column", got.Column, c.Expect.Column)
			check("column_new", got.ColumnNew, c.Expect.ColumnNew)
			check("due_date", got.DueDate, c.Expect.DueDate)
			check("due_time", got.DueTime, c.Expect.DueTime)
			check("recurrence", got.Recurrence, c.Expect.Recurrence)
			if (got.Priority == nil) != (c.Expect.Priority == nil) || (got.Priority != nil && *got.Priority != *c.Expect.Priority) {
				t.Errorf("priority = %v, want %v", got.Priority, c.Expect.Priority)
			}
			if !slices.Equal(got.Labels, c.Expect.Labels) {
				t.Errorf("labels = %v, want %v", got.Labels, c.Expect.Labels)
			}
			for _, tk := range got.Tokens {
				if c.Input[tk.Start:tk.End] != tk.Text {
					t.Errorf("token span %d:%d = %q, text %q", tk.Start, tk.End, c.Input[tk.Start:tk.End], tk.Text)
				}
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	vf, ctx := loadVectors(f)
	for _, c := range vf.Cases {
		f.Add(c.Input)
	}
	f.Fuzz(func(t *testing.T, in string) {
		res, err := Parse(in, ctx)
		if err == nil && strings.TrimSpace(res.Title) == "" {
			t.Fatalf("empty title without error for %q", in)
		}
		for _, tk := range res.Tokens {
			if tk.Start < 0 || tk.End > len(in) || tk.Start > tk.End {
				t.Fatalf("bad token span %+v for %q", tk, in)
			}
		}
	})
}
