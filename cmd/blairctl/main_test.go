package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/wbollock/blair/api"
	"github.com/wbollock/blair/internal/auth"
	"github.com/wbollock/blair/internal/httpapi"
	"github.com/wbollock/blair/internal/service"
	"github.com/wbollock/blair/internal/store"
)

// startServer runs a real Blair API on a temp database and returns an
// environment lookup that points blairctl at it.
func startServer(t *testing.T) func(string) string {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := auth.NewSecretBox(bytes.Repeat([]byte{9}, 32), "totp")
	svc := service.New(db, box, service.Options{Registration: "closed"})
	user, err := svc.CreateUser(ctx, service.NewUser{Email: "will@example.com", Password: "correct horse battery staple 42", Timezone: "UTC"}, true)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := svc.CreateToken(ctx, user, service.NewToken{Name: "blairctl-test", Scopes: []string{"tasks:read", "tasks:write"}})
	if err != nil {
		t.Fatal(err)
	}
	jsonDoc, _ := api.JSON()
	srv := httptest.NewServer(httpapi.New(svc, db, httpapi.Config{
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")},
		OpenAPIYAML:    api.YAML,
		OpenAPIJSON:    jsonDoc,
		Version:        "test",
	}))
	t.Cleanup(srv.Close)
	return func(k string) string {
		switch k {
		case "BLAIR_URL":
			return srv.URL
		case "BLAIR_TOKEN":
			return tok.Token
		}
		return ""
	}
}

func runOK(t *testing.T, env func(string) string, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := run(args, env, &out); err != nil {
		t.Fatalf("blairctl %s: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

func TestTaskLifecycle(t *testing.T) {
	env := startServer(t)

	var created struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
	}
	out := runOK(t, env, "task", "add", "Match donation", "--priority", "3", "--description", "https://example.test", "--json")
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("add output is not a task: %v\n%s", err, out)
	}
	if created.Title != "Match donation" || created.Priority != 3 || created.Description != "https://example.test" {
		t.Fatalf("created = %+v", created)
	}

	if list := runOK(t, env, "task", "list"); !strings.Contains(list, created.ID) || !strings.Contains(list, "Match donation") {
		t.Fatalf("open list misses the task:\n%s", list)
	}
	if list := runOK(t, env, "task", "list", "--search", "nomatch"); strings.Contains(list, created.ID) {
		t.Fatalf("search returned a non-matching task:\n%s", list)
	}

	runOK(t, env, "task", "edit", created.ID, "--title", "Match donation 2026", "--status", "doing")
	got := runOK(t, env, "task", "get", created.ID)
	if !strings.Contains(got, `"title": "Match donation 2026"`) || !strings.Contains(got, `"status": "doing"`) {
		t.Fatalf("edit did not apply:\n%s", got)
	}
	if !strings.Contains(got, `"description": "https://example.test"`) {
		t.Fatalf("edit without --description changed the description:\n%s", got)
	}

	runOK(t, env, "task", "done", created.ID)
	if list := runOK(t, env, "task", "list"); strings.Contains(list, created.ID) {
		t.Fatalf("done task still in open list:\n%s", list)
	}
	if list := runOK(t, env, "task", "list", "--status", "done"); !strings.Contains(list, created.ID) {
		t.Fatalf("done list misses the task:\n%s", list)
	}
}

func TestProjectList(t *testing.T) {
	runOK(t, startServer(t), "project", "list")
}

func TestMissingConfiguration(t *testing.T) {
	err := run([]string{"project", "list"}, func(string) string { return "" }, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "BLAIR_URL") {
		t.Fatalf("err = %v, want BLAIR_URL message", err)
	}
}

func TestAPIErrorIsReported(t *testing.T) {
	env := startServer(t)
	err := run([]string{"task", "get", "01a20ff8-c9e8-7321-be6a-4fd914c66faf"}, env, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v, want a 404 message", err)
	}
}
