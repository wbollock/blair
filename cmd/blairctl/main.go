// Command blairctl is a command line client for the Blair API.
//
// It reads BLAIR_URL and BLAIR_TOKEN from the environment. Add -json to any
// command to print the raw API response.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const usage = `usage: blairctl <command> [flags]

commands:
  project list
  task list   [-project NAME] [-status todo,doing,waiting] [-search TEXT] [-limit N]
  task get    ID
  task add    TITLE [-quick] [-project NAME] [-column NAME] [-status S] [-priority 0-4] [-due YYYY-MM-DD] [-description TEXT]
  task edit   ID [-title T] [-project NAME] [-column NAME] [-status S] [-priority 0-4] [-due YYYY-MM-DD|none] [-description TEXT]
  task done   ID

environment: BLAIR_URL, BLAIR_TOKEN
`

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "blairctl:", err)
		os.Exit(1)
	}
}

func run(args []string, getenv func(string) string, out io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(out, usage)
		return nil
	}
	c, err := newClient(getenv)
	if err != nil {
		return err
	}
	cmd, rest := args[0], args[1:]
	if len(rest) == 0 {
		return fmt.Errorf("missing subcommand\n\n%s", usage)
	}
	switch cmd + " " + rest[0] {
	case "project list":
		return c.projectList(rest[1:], out)
	case "task list":
		return c.taskList(rest[1:], out)
	case "task get":
		return c.taskGet(rest[1:], out)
	case "task add":
		return c.taskAdd(rest[1:], out)
	case "task edit":
		return c.taskEdit(rest[1:], out)
	case "task done":
		return c.taskDone(rest[1:], out)
	}
	return fmt.Errorf("unknown command %q\n\n%s", cmd+" "+rest[0], usage)
}

type client struct {
	base  string
	token string
	http  *http.Client
}

func newClient(getenv func(string) string) (*client, error) {
	base, token := strings.TrimRight(getenv("BLAIR_URL"), "/"), getenv("BLAIR_TOKEN")
	if base == "" {
		return nil, errors.New("BLAIR_URL is not set")
	}
	if token == "" {
		return nil, errors.New("BLAIR_TOKEN is not set")
	}
	return &client{base: base, token: token, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

// do sends a request to the API and returns the response body. Non-2xx
// responses become errors that carry the problem detail.
func (c *client) do(method, path string, query url.Values, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	u := c.base + "/api/v1" + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		var p struct{ Title, Detail string }
		_ = json.Unmarshal(raw, &p)
		msg := strings.TrimSpace(p.Title + ": " + p.Detail)
		if p.Title == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return nil, fmt.Errorf("%s %s: %d %s", method, path, res.StatusCode, msg)
	}
	return raw, nil
}

// parseFlags parses flags that may appear before or after positional
// arguments and returns the positionals. A bare "--" ends flag parsing, and
// everything after it is positional. A flag that takes a value consumes the
// next argument even when that argument is "--".
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append(positional, args[i+1:]...), nil
		}
		if len(arg) < 2 || arg[0] != '-' {
			positional = append(positional, arg)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		f := fs.Lookup(name)
		if f == nil {
			return nil, fmt.Errorf("unknown flag: %s", arg)
		}
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
			if !hasValue {
				value = "true"
			}
		} else if !hasValue {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("flag needs a value: %s", arg)
			}
			i++
			value = args[i]
		}
		if err := fs.Set(name, value); err != nil {
			return nil, fmt.Errorf("invalid value for %s: %w", arg, err)
		}
	}
	return positional, nil
}

func newFlagSet(name string) (*flag.FlagSet, *bool) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return fs, fs.Bool("json", false, "print the raw API response")
}

func printJSON(out io.Writer, raw []byte, indent bool) error {
	if indent {
		var buf bytes.Buffer
		if err := json.Indent(&buf, raw, "", "  "); err != nil {
			return err
		}
		raw = buf.Bytes()
	}
	_, err := fmt.Fprintln(out, strings.TrimSpace(string(raw)))
	return err
}

type task struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Status   string  `json:"status"`
	Priority int     `json:"priority"`
	DueDate  *string `json:"due_date"`
}

func printTask(out io.Writer, t task) {
	due := ""
	if t.DueDate != nil {
		due = *t.DueDate
	}
	fmt.Fprintf(out, "%s\t%s\tp%d\t%s\t%s\n", t.ID, t.Status, t.Priority, due, t.Title)
}

func (c *client) projectList(args []string, out io.Writer) error {
	fs, asJSON := newFlagSet("project list")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	raw, err := c.do("GET", "/projects", nil, nil)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(out, raw, false)
	}
	var res struct {
		Data []struct {
			ID      string `json:"id"`
			Name    string `json:"name"`
			Columns []struct {
				Name string `json:"name"`
			} `json:"columns"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return err
	}
	for _, p := range res.Data {
		cols := make([]string, len(p.Columns))
		for i, col := range p.Columns {
			cols[i] = col.Name
		}
		fmt.Fprintf(out, "%s\t%s\t%s\n", p.ID, p.Name, strings.Join(cols, ", "))
	}
	return nil
}

// projectID resolves a project name to its id. Archived projects match too,
// and an active project wins over an archived one with the same name.
func (c *client) projectID(name string) (string, error) {
	raw, err := c.do("GET", "/projects", url.Values{"include_archived": {"true"}}, nil)
	if err != nil {
		return "", err
	}
	var res struct {
		Data []struct {
			ID         string  `json:"id"`
			Name       string  `json:"name"`
			ArchivedAt *string `json:"archived_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	archivedID := ""
	for _, p := range res.Data {
		if !strings.EqualFold(p.Name, name) {
			continue
		}
		if p.ArchivedAt == nil {
			return p.ID, nil
		}
		if archivedID == "" {
			archivedID = p.ID
		}
	}
	if archivedID == "" {
		return "", fmt.Errorf("project not found: %s", name)
	}
	return archivedID, nil
}

func (c *client) taskList(args []string, out io.Writer) error {
	fs, asJSON := newFlagSet("task list")
	project := fs.String("project", "", "only tasks in this project")
	status := fs.String("status", "todo,doing,waiting", "comma-separated statuses")
	search := fs.String("search", "", "full-text search over title and description")
	limit := fs.Int("limit", 200, "maximum number of tasks")
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	q := url.Values{"status": {*status}, "limit": {fmt.Sprint(*limit)}}
	if *search != "" {
		q.Set("q", *search)
	}
	if *project != "" {
		id, err := c.projectID(*project)
		if err != nil {
			return err
		}
		q.Set("project_id", id)
	}
	raw, err := c.do("GET", "/tasks", q, nil)
	if err != nil {
		return err
	}
	if *asJSON {
		return printJSON(out, raw, false)
	}
	var res struct {
		Data []task `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return err
	}
	for _, t := range res.Data {
		printTask(out, t)
	}
	return nil
}

func (c *client) taskGet(args []string, out io.Writer) error {
	fs, _ := newFlagSet("task get")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("task get needs one task ID")
	}
	raw, err := c.do("GET", "/tasks/"+url.PathEscape(pos[0]), nil, nil)
	if err != nil {
		return err
	}
	return printJSON(out, raw, true)
}

// taskFields are the flags shared by task add and task edit.
type taskFields struct {
	project, column, status, due, description *string
	priority                                  *int
}

func addTaskFields(fs *flag.FlagSet) taskFields {
	return taskFields{
		project:     fs.String("project", "", "project name"),
		column:      fs.String("column", "", "kanban column name"),
		status:      fs.String("status", "", "todo, doing, waiting, done or cancelled"),
		due:         fs.String("due", "", "due date, YYYY-MM-DD"),
		description: fs.String("description", "", "task description"),
		priority:    fs.Int("priority", 0, "0 none, 1 low, 2 medium, 3 high, 4 urgent"),
	}
}

// bodyFromFlags builds the request body from the flags the user set.
func (f taskFields) bodyFromFlags(fs *flag.FlagSet) map[string]any {
	body := map[string]any{}
	fs.Visit(func(fl *flag.Flag) {
		switch fl.Name {
		case "project":
			body["project_name"] = *f.project
		case "column":
			body["column_name"] = *f.column
		case "status":
			body["status"] = *f.status
		case "description":
			body["description"] = *f.description
		case "priority":
			body["priority"] = *f.priority
		case "due":
			if *f.due == "none" {
				body["due_date"] = nil
			} else {
				body["due_date"] = *f.due
			}
		}
	})
	return body
}

func (c *client) taskAdd(args []string, out io.Writer) error {
	fs, asJSON := newFlagSet("task add")
	quick := fs.Bool("quick", false, "treat the text as quick-add syntax")
	fields := addTaskFields(fs)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return errors.New("task add needs a title")
	}
	body := fields.bodyFromFlags(fs)
	text := strings.Join(pos, " ")
	if *quick {
		body["quick_add"] = text
		if *fields.project != "" {
			id, err := c.projectID(*fields.project)
			if err != nil {
				return err
			}
			body["context_project_id"] = id
		}
	} else {
		body["title"] = text
	}
	raw, err := c.do("POST", "/tasks", nil, body)
	if err != nil {
		return err
	}
	return printTaskResult(out, raw, *asJSON)
}

func (c *client) taskEdit(args []string, out io.Writer) error {
	fs, asJSON := newFlagSet("task edit")
	title := fs.String("title", "", "new title")
	fields := addTaskFields(fs)
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("task edit needs one task ID")
	}
	body := fields.bodyFromFlags(fs)
	fs.Visit(func(fl *flag.Flag) {
		if fl.Name == "title" {
			body["title"] = *title
		}
	})
	if len(body) == 0 {
		return errors.New("task edit needs at least one field to change")
	}
	return c.patchTask(pos[0], body, out, *asJSON)
}

func (c *client) taskDone(args []string, out io.Writer) error {
	fs, asJSON := newFlagSet("task done")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("task done needs one task ID")
	}
	return c.patchTask(pos[0], map[string]any{"status": "done"}, out, *asJSON)
}

func (c *client) patchTask(id string, body map[string]any, out io.Writer, asJSON bool) error {
	raw, err := c.do("PATCH", "/tasks/"+url.PathEscape(id), nil, body)
	if err != nil {
		return err
	}
	return printTaskResult(out, raw, asJSON)
}

// printTaskResult prints the task returned by create or update.
func printTaskResult(out io.Writer, raw []byte, asJSON bool) error {
	if asJSON {
		return printJSON(out, raw, false)
	}
	var t task
	if err := json.Unmarshal(raw, &t); err != nil {
		return err
	}
	printTask(out, t)
	return nil
}
