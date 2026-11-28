package mcpsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/wbollock/blair/internal/auth"
	"github.com/wbollock/blair/internal/oauth"
	"github.com/wbollock/blair/internal/service"
	"github.com/wbollock/blair/internal/store"
)

// fakeVerifier maps test tokens to principals.
type fakeVerifier map[string]*oauth.Principal

func (f fakeVerifier) VerifyAccessToken(_ context.Context, tok string) (*oauth.Principal, error) {
	if p, ok := f[tok]; ok {
		return p, nil
	}
	return nil, oauth.ErrInvalidToken
}

type env struct {
	t    *testing.T
	svc  *service.Service
	db   *store.DB
	user *service.User
	srv  *httptest.Server
	ver  fakeVerifier
}

const metaURL = "https://blair.test/.well-known/oauth-protected-resource/mcp"

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := auth.NewSecretBox(bytes.Repeat([]byte{3}, 32), "totp")
	svc := service.New(db, box, service.Options{Registration: "closed"})
	u, err := svc.CreateUser(context.Background(), service.NewUser{Email: "will@example.com", Password: "plum-orbit-kettle-91", Timezone: "UTC"}, true)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, svc: svc, db: db, user: u, ver: fakeVerifier{}}
	e.ver["rw"] = &oauth.Principal{User: u, GrantID: "g-rw", ClientID: "c1", Scopes: []string{"tasks:read", "tasks:write"}, Expires: time.Now().Add(time.Hour)}
	e.ver["ro"] = &oauth.Principal{User: u, GrantID: "g-ro", ClientID: "c1", Scopes: []string{"tasks:read"}, Expires: time.Now().Add(time.Hour)}
	e.srv = httptest.NewServer(Handler(svc, e.ver, Config{Version: "test", MetadataURL: metaURL}))
	t.Cleanup(e.srv.Close)
	return e
}

type bearer struct{ tok string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return http.DefaultTransport.RoundTrip(r)
}

func (e *env) session(tok string) *mcp.ClientSession {
	e.t.Helper()
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	s, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.srv.URL, HTTPClient: &http.Client{Transport: bearer{tok}}}, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { s.Close() })
	return s
}

// call returns the structured result, or the tool's error text.
func call(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) (map[string]any, string) {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	if res.IsError {
		var sb strings.Builder
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				sb.WriteString(tc.Text)
			}
		}
		return nil, sb.String()
	}
	raw, _ := json.Marshal(res.StructuredContent)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out, ""
}

func mustCall(t *testing.T, s *mcp.ClientSession, name string, args map[string]any) map[string]any {
	t.Helper()
	out, errText := call(t, s, name, args)
	if errText != "" {
		t.Fatalf("%s failed: %s", name, errText)
	}
	return out
}

func TestToolListHasNoDelete(t *testing.T) {
	e := newEnv(t)
	s := e.session("rw")
	res, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
		if strings.Contains(strings.ToLower(tl.Name), "delete") || strings.Contains(strings.ToLower(tl.Name), "remove") || strings.Contains(strings.ToLower(tl.Name), "trash") {
			t.Errorf("tool %q looks destructive", tl.Name)
		}
		// Only tools that purely add something may claim to be non-destructive; an edit replaces
		// values, even though nothing is ever deleted.
		additive := tl.Name == "create_task" || tl.Name == "plan_today"
		edits := slices.Contains([]string{"update_task", "complete_task", "move_task"}, tl.Name)
		switch {
		case tl.Annotations == nil:
			t.Errorf("tool %q has no annotations", tl.Name)
		case additive && (tl.Annotations.DestructiveHint == nil || *tl.Annotations.DestructiveHint):
			t.Errorf("tool %q only adds and should say so", tl.Name)
		case edits && (tl.Annotations.DestructiveHint == nil || !*tl.Annotations.DestructiveHint):
			t.Errorf("tool %q replaces values and must not claim to be non-destructive", tl.Name)
		}
		if tl.Annotations != nil && tl.Annotations.ReadOnlyHint == slices.Contains([]string{"create_task", "update_task", "complete_task", "move_task", "plan_today"}, tl.Name) {
			t.Errorf("tool %q has the wrong read-only hint", tl.Name)
		}
	}
	slices.Sort(names)
	want := []string{"complete_task", "create_task", "get_task", "list_projects", "list_tasks", "move_task", "overdue", "plan_today", "search", "slipping", "today", "update_task"}
	if !slices.Equal(names, want) {
		t.Fatalf("tools = %v\nwant    %v", names, want)
	}
}

func TestStatelessAndNoStreams(t *testing.T) {
	e := newEnv(t)
	for _, m := range []string{"GET", "DELETE"} {
		req, _ := http.NewRequest(m, e.srv.URL, nil)
		req.Header.Set("Authorization", "Bearer rw")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d, want 405", m, res.StatusCode)
		}
		if res.Header.Get("Mcp-Session-Id") != "" {
			t.Errorf("%s: stateless server set a session id", m)
		}
	}
}

func TestAuthChallenge(t *testing.T) {
	e := newEnv(t)
	do := func(auth string, hdr map[string]string) *http.Response {
		req, _ := http.NewRequest("POST", e.srv.URL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	res := do("", nil)
	h := res.Header.Get("WWW-Authenticate")
	if res.StatusCode != 401 || !strings.Contains(h, `resource_metadata="`+metaURL+`"`) || strings.Contains(h, "error=") {
		t.Fatalf("no token: %d %q", res.StatusCode, h)
	}
	res = do("Bearer nope", nil)
	if h := res.Header.Get("WWW-Authenticate"); res.StatusCode != 401 || !strings.Contains(h, `error="invalid_token"`) || !strings.Contains(h, "resource_metadata=") {
		t.Fatalf("bad token: %d %q", res.StatusCode, h)
	}
	if res := do("Basic abc", nil); res.StatusCode != 401 {
		t.Fatalf("basic auth: %d", res.StatusCode)
	}
	// A Blair personal access token is not an MCP credential.
	if res := do("Bearer blair_pat_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG", nil); res.StatusCode != 401 {
		t.Fatalf("PAT: %d", res.StatusCode)
	}
	if res := do("Bearer rw", map[string]string{"Origin": "https://evil.example"}); res.StatusCode != 403 {
		t.Fatalf("foreign origin: %d", res.StatusCode)
	}
	if res := do("Bearer rw", map[string]string{"Origin": "https://claude.ai"}); res.StatusCode != 200 {
		t.Fatalf("claude.ai origin: %d", res.StatusCode)
	}
	if res := do("Bearer rw", nil); res.StatusCode != 200 {
		t.Fatalf("valid token: %d", res.StatusCode)
	}
}

func TestReadOnlyGrantCannotWrite(t *testing.T) {
	e := newEnv(t)
	rw := e.session("rw")
	made := mustCall(t, rw, "create_task", map[string]any{"title": "from claude"})
	id := made["id"].(string)

	ro := e.session("ro")
	mustCall(t, ro, "get_task", map[string]any{"id": id})
	for name, args := range map[string]map[string]any{
		"create_task":   {"title": "x"},
		"update_task":   {"id": id, "version": 1, "title": "y"},
		"complete_task": {"id": id},
		"move_task":     {"id": id},
		"plan_today":    {"add_task_ids": []string{id}},
	} {
		if _, errText := call(t, ro, name, args); !strings.Contains(errText, "not granted tasks:write") {
			t.Errorf("%s with a read-only grant: %q", name, errText)
		}
	}
	got := mustCall(t, rw, "get_task", map[string]any{"id": id})
	if got["task"].(map[string]any)["title"] != "from claude" {
		t.Fatal("a refused write changed the task")
	}
}

func TestTaskLifecycle(t *testing.T) {
	e := newEnv(t)
	s := e.session("rw")

	// Project + column + label made the normal way; the tools only reference them by name.
	ctx := context.Background()
	proj, err := e.svc.CreateProject(ctx, e.user, service.ProjectCreate{Name: "Homelab"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateLabel(ctx, e.user, service.LabelCreate{Name: "house"}); err != nil {
		t.Fatal(err)
	}

	projects := mustCall(t, s, "list_projects", nil)
	ps := projects["projects"].([]any)
	if len(ps) != 1 || ps[0].(map[string]any)["name"] != "Homelab" || len(ps[0].(map[string]any)["columns"].([]any)) < 3 {
		t.Fatalf("projects: %v", projects)
	}

	made := mustCall(t, s, "create_task", map[string]any{"title": "Fix gutter", "project": "homelab", "column": "Doing", "priority": 3,
		"due_date": "2000-01-02", "labels": []string{"house"}, "description": "ladder in the shed"})
	id := made["id"].(string)
	if made["project"] != "Homelab" || made["status"] != "doing" || made["priority"] != float64(3) || made["version"] != float64(1) || made["labels"].([]any)[0] != "house" {
		t.Fatalf("created: %v", made)
	}
	if made["project_id"] != proj.ID {
		t.Fatal("project id")
	}

	if _, errText := call(t, s, "create_task", map[string]any{"title": "x", "project": "No such project"}); !strings.Contains(errText, "no project") {
		t.Fatalf("unknown project: %q", errText)
	}
	if _, errText := call(t, s, "create_task", map[string]any{"title": "x", "labels": []string{"ghost"}}); !strings.Contains(errText, "no label") {
		t.Fatalf("unknown label must not be auto-created: %q", errText)
	}
	if _, errText := call(t, s, "create_task", map[string]any{"title": ""}); !strings.Contains(errText, "invalid input") {
		t.Fatalf("empty title: %q", errText)
	}

	// Reads.
	got := mustCall(t, s, "get_task", map[string]any{"id": id})
	if got["task"].(map[string]any)["description"] != "ladder in the shed" {
		t.Fatalf("get_task: %v", got)
	}
	list := mustCall(t, s, "list_tasks", map[string]any{"project": "Homelab", "label": "house"})
	if len(list["tasks"].([]any)) != 1 {
		t.Fatalf("list_tasks: %v", list)
	}
	if n := len(mustCall(t, s, "list_tasks", map[string]any{"status": []string{"done"}})["tasks"].([]any)); n != 0 {
		t.Fatalf("done tasks: %d", n)
	}
	if n := len(mustCall(t, s, "search", map[string]any{"query": "gutter"})["tasks"].([]any)); n != 1 {
		t.Fatalf("search: %d", n)
	}
	if n := len(mustCall(t, s, "overdue", nil)["tasks"].([]any)); n != 1 {
		t.Fatalf("overdue: %d", n)
	}
	mustCall(t, s, "slipping", nil)

	// Update needs the current version (ETag semantics).
	if _, errText := call(t, s, "update_task", map[string]any{"id": id, "title": "x"}); !strings.Contains(errText, "version") {
		t.Fatalf("missing version: %q", errText)
	}
	upd := mustCall(t, s, "update_task", map[string]any{"id": id, "version": 1, "title": "Fix the gutter", "priority": 1, "clear": []string{"due_date"}})
	if upd["title"] != "Fix the gutter" || upd["version"] != float64(2) || upd["due_date"] != nil {
		t.Fatalf("updated: %v", upd)
	}
	if _, errText := call(t, s, "update_task", map[string]any{"id": id, "version": 1, "title": "stale write"}); !strings.Contains(errText, "version mismatch") || !strings.Contains(errText, "current version 2") {
		t.Fatalf("stale version: %q", errText)
	}
	if n := mustCall(t, s, "get_task", map[string]any{"id": id})["task"].(map[string]any)["title"]; n != "Fix the gutter" {
		t.Fatalf("stale write changed the task: %v", n)
	}
	if _, errText := call(t, s, "update_task", map[string]any{"id": id, "version": 2, "clear": []string{"title"}}); errText == "" {
		t.Fatal("clearing the title must be refused")
	}

	// Plan, move, complete.
	plan := mustCall(t, s, "plan_today", map[string]any{"add_task_ids": []string{id}})
	if len(plan["tasks"].([]any)) != 1 {
		t.Fatalf("plan: %v", plan)
	}
	today := mustCall(t, s, "today", nil)
	if len(today["planned"].([]any)) != 1 || today["date"] == "" {
		t.Fatalf("today: %v", today)
	}
	moved := mustCall(t, s, "move_task", map[string]any{"id": id, "column": "Done"})
	if moved["status"] != "done" {
		t.Fatalf("move to Done: %v", moved)
	}
	mustCall(t, s, "move_task", map[string]any{"id": id, "column": "To do"})
	done := mustCall(t, s, "complete_task", map[string]any{"id": id})
	if done["status"] != "done" || done["completed_at"] == nil {
		t.Fatalf("complete: %v", done)
	}
	if again := mustCall(t, s, "complete_task", map[string]any{"id": id}); again["version"] != done["version"] {
		t.Fatal("completing twice must be a no-op")
	}
	if _, errText := call(t, s, "get_task", map[string]any{"id": "0198b2a0-0000-7000-8000-000000000000"}); errText != "not found" {
		t.Fatalf("missing task: %q", errText)
	}
}

func TestOnlyOwnTasksAreVisible(t *testing.T) {
	e := newEnv(t)
	other, err := e.svc.CreateUser(context.Background(), service.NewUser{Email: "other@example.com", Password: "plum-orbit-kettle-92", Timezone: "UTC"}, false)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := e.svc.CreateTask(context.Background(), other, service.TaskCreate{Title: "private"})
	if err != nil {
		t.Fatal(err)
	}
	s := e.session("rw")
	if _, errText := call(t, s, "get_task", map[string]any{"id": theirs.Task.ID}); errText != "not found" {
		t.Fatalf("read another user's task: %q", errText)
	}
	if _, errText := call(t, s, "complete_task", map[string]any{"id": theirs.Task.ID}); errText != "not found" {
		t.Fatalf("complete another user's task: %q", errText)
	}
	if n := len(mustCall(t, s, "search", map[string]any{"query": "private"})["tasks"].([]any)); n != 0 {
		t.Fatalf("search leaked %d tasks", n)
	}
}

func TestWritesAreAuditedWithoutContent(t *testing.T) {
	e := newEnv(t)
	s := e.session("rw")
	made := mustCall(t, s, "create_task", map[string]any{"title": "secret-title-xyz", "description": "secret-body-xyz"})
	mustCall(t, s, "list_tasks", nil)
	rows, err := e.db.R.Query(`SELECT kind, detail FROM audit_events WHERE kind LIKE 'mcp.%'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var kind, detail string
		_ = rows.Scan(&kind, &detail)
		n++
		if strings.Contains(detail, "secret-") {
			t.Fatalf("audit detail carries task content: %s", detail)
		}
		var d map[string]any
		_ = json.Unmarshal([]byte(detail), &d)
		if kind != "mcp.write" || d["tool"] != "create_task" || d["id"] != made["id"] || d["grant_id"] != "g-rw" {
			t.Fatalf("audit: %s %s", kind, detail)
		}
	}
	if n != 1 {
		t.Fatalf("%d mcp audit rows, want 1 (reads are not audited)", n)
	}
}

func TestWriteRateLimit(t *testing.T) {
	e := newEnv(t)
	s := e.session("rw")
	limited := 0
	for range 40 {
		if _, errText := call(t, s, "create_task", map[string]any{"title": "x"}); strings.Contains(errText, "too many changes") {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("40 rapid writes were never limited")
	}
	// Reads have their own, larger budget.
	if _, errText := call(t, s, "list_tasks", nil); errText != "" {
		t.Fatalf("read after the write limit: %q", errText)
	}
}

// Behind a reverse proxy the peer is loopback while the Host header is the public name. The SDK's
// local-server guard would answer every request with 403.
func TestWorksBehindAReverseProxyWithAPublicHost(t *testing.T) {
	e := newEnv(t)
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	hostRT := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Host = "blair.example.com"
		r.Header.Set("Authorization", "Bearer rw")
		return http.DefaultTransport.RoundTrip(r)
	})
	s, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.srv.URL, HTTPClient: &http.Client{Transport: hostRT}}, nil)
	if err != nil {
		t.Fatalf("a public Host header on a loopback connection was refused: %v", err)
	}
	defer s.Close()
	mustCall(t, s, "list_projects", nil)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLabelsWithNonASCIINamesWork(t *testing.T) {
	e := newEnv(t)
	if _, err := e.svc.CreateLabel(context.Background(), e.user, service.LabelCreate{Name: "École"}); err != nil {
		t.Fatal(err)
	}
	s := e.session("rw")
	made := mustCall(t, s, "create_task", map[string]any{"title": "x", "labels": []string{"école"}})
	if l := made["labels"].([]any); len(l) != 1 || l[0] != "École" {
		t.Fatalf("labels: %v", made["labels"])
	}
	upd := mustCall(t, s, "update_task", map[string]any{"id": made["id"], "version": made["version"], "labels": []string{"ÉCOLE"}})
	if l := upd["labels"].([]any); len(l) != 1 || l[0] != "École" {
		t.Fatalf("updated labels: %v", upd["labels"])
	}
}
