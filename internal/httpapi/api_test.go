package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/wbollock/blair/api"
	"github.com/wbollock/blair/internal/auth"
	"github.com/wbollock/blair/internal/service"
	"github.com/wbollock/blair/internal/store"
)

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	svc    *service.Service
	router routers.Router
	now    time.Time
}

func newHarness(t *testing.T, opts service.Options) *harness {
	t.Helper()
	db, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := auth.NewSecretBox(bytes.Repeat([]byte{9}, 32), "totp")
	svc := service.New(db, box, opts)
	h := &harness{t: t, svc: svc, now: time.Now()}
	svc.SetClock(func() time.Time { return h.now })
	jsonDoc, _ := api.JSON()
	handler := New(svc, db, Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}, OpenAPIYAML: api.YAML, OpenAPIJSON: jsonDoc, Version: "test"})
	h.srv = httptest.NewServer(handler)
	t.Cleanup(h.srv.Close)

	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	doc.Servers = openapi3.Servers{{URL: h.srv.URL + "/api/v1"}}
	if h.router, err = gorillamux.NewRouter(doc); err != nil {
		t.Fatal(err)
	}
	return h
}

type client struct {
	h      *harness
	c      *http.Client
	bearer string
	header map[string]string
}

func (h *harness) browser() *client {
	jar, _ := cookiejar.New(nil)
	return &client{h: h, c: &http.Client{Jar: jar}, header: map[string]string{"X-Requested-With": "blair"}}
}

func (h *harness) token(tok string) *client {
	return &client{h: h, c: &http.Client{}, bearer: tok, header: map[string]string{}}
}

type resp struct {
	status int
	header http.Header
	raw    []byte
	body   map[string]any
}

func (r resp) str(k string) string { s, _ := r.body[k].(string); return s }

// do sends a request and validates request and response against the OpenAPI contract.
func (c *client) do(method, path string, body any) resp {
	c.h.t.Helper()
	var rd io.Reader
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, c.h.srv.URL+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	res, err := c.c.Do(req)
	if err != nil {
		c.h.t.Fatal(err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	r := resp{status: res.StatusCode, header: res.Header, raw: out}
	_ = json.Unmarshal(out, &r.body)

	if strings.HasPrefix(path, "/api/v1/") {
		c.h.validate(method, path, raw, req.Header, res.StatusCode, res.Header, out)
	}
	return r
}

func (h *harness) validate(method, path string, reqBody []byte, reqHeader http.Header, status int, resHeader http.Header, resBody []byte) {
	h.t.Helper()
	vreq, _ := http.NewRequest(method, h.srv.URL+path, bytes.NewReader(reqBody))
	vreq.Header = reqHeader.Clone()
	route, params, err := h.router.FindRoute(vreq)
	if err != nil {
		h.t.Fatalf("%s %s: not in OpenAPI contract: %v", method, path, err)
	}
	opts := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, MultiError: true}
	in := &openapi3filter.RequestValidationInput{Request: vreq, PathParams: params, Route: route, Options: opts}
	if status < 400 && len(reqBody) > 0 {
		if err := openapi3filter.ValidateRequest(context.Background(), in); err != nil {
			h.t.Errorf("%s %s: request does not match contract: %v", method, path, err)
		}
	}
	if status >= 200 && status < 300 && route.Operation.Responses.Status(status) == nil {
		h.t.Errorf("%s %s: status %d is not documented", method, path, status)
	}
	out := &openapi3filter.ResponseValidationInput{RequestValidationInput: in, Status: status, Header: resHeader, Body: io.NopCloser(bytes.NewReader(resBody)), Options: opts}
	if err := openapi3filter.ValidateResponse(context.Background(), out); err != nil {
		h.t.Errorf("%s %s -> %d: response does not match contract: %v\n%s", method, path, status, err, resBody)
	}
}

func expect(t *testing.T, r resp, status int) {
	t.Helper()
	if r.status != status {
		t.Fatalf("status %d, want %d: %s", r.status, status, r.raw)
	}
}

const pw = "plum-orbit-kettle-91"

// signedInAdmin completes first-run setup and returns the admin's browser client.
func (h *harness) signedInAdmin() *client {
	h.t.Helper()
	tok, err := h.svc.SetupToken(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	b := h.browser()
	expect(h.t, b.do("POST", "/api/v1/auth/setup", map[string]any{"token": tok, "email": "will@example.com", "password": pw, "timezone": "America/New_York"}), 201)
	return b
}

func TestEndToEndContract(t *testing.T) {
	h := newHarness(t, service.Options{Registration: "invite"})
	anon := h.browser()
	st := anon.do("GET", "/api/v1/auth/status", nil)
	expect(t, st, 200)
	if st.body["setup_required"] != true {
		t.Fatal("setup should be required")
	}
	b := h.signedInAdmin()
	if b.do("GET", "/api/v1/auth/status", nil).body["authenticated"] != true {
		t.Fatal("not signed in after setup")
	}
	expect(t, b.do("GET", "/api/v1/me", nil), 200)
	expect(t, b.do("PATCH", "/api/v1/me", map[string]any{"display_name": "Will", "settings": map[string]any{"theme": "system"}}), 200)

	// Projects, columns, labels.
	p := b.do("POST", "/api/v1/projects", map[string]any{"name": "Homelab", "color": "#2E9E6A"})
	expect(t, p, 201)
	pid := p.str("id")
	cols := p.body["columns"].([]any)
	doing := cols[1].(map[string]any)["id"].(string)
	col := b.do("POST", "/api/v1/projects/"+pid+"/columns", map[string]any{"name": "Waiting", "status": "waiting", "after_id": doing})
	expect(t, col, 201)
	expect(t, b.do("PATCH", "/api/v1/columns/"+col.str("id"), map[string]any{"name": "Blocked", "wip_limit": 3}), 200)
	expect(t, b.do("GET", "/api/v1/projects", nil), 200)
	expect(t, b.do("POST", "/api/v1/labels", map[string]any{"name": "house"}), 201)
	expect(t, b.do("GET", "/api/v1/labels", nil), 200)

	// Quick-add creates with parsed tokens.
	tk := b.do("POST", "/api/v1/tasks", map[string]any{"quick_add": "Fix gutter #homelab /doing !high tomorrow at 9am @house"})
	expect(t, tk, 201)
	if tk.str("title") != "Fix gutter" || tk.str("column_id") != doing || tk.body["parsed"] == nil {
		t.Fatalf("quick-add result: %s", tk.raw)
	}
	tid := tk.str("id")
	etag := tk.header.Get("ETag")
	if etag != `"v1"` {
		t.Fatalf("etag = %q", etag)
	}
	// If-Match: good then stale.
	b.header["If-Match"] = etag
	expect(t, b.do("PATCH", "/api/v1/tasks/"+tid, map[string]any{"description": "Use the **tall** ladder"}), 200)
	stale := b.do("PATCH", "/api/v1/tasks/"+tid, map[string]any{"priority": 1})
	expect(t, stale, 412)
	if stale.body["current"] == nil {
		t.Fatal("412 should include current")
	}
	delete(b.header, "If-Match")

	// Reminders and notifications.
	rem := b.do("PATCH", "/api/v1/tasks/"+tid, map[string]any{"remind_at": "2020-01-01T09:00:00Z"})
	expect(t, rem, 200)
	if rem.str("remind_at") != "2020-01-01T09:00:00.000Z" {
		t.Fatalf("remind_at = %s", rem.raw)
	}
	if _, err := h.svc.FireReminders(context.Background()); err != nil {
		t.Fatal(err)
	}
	notes := b.do("GET", "/api/v1/notifications?unread=true", nil)
	expect(t, notes, 200)
	if notes.body["unread_count"].(float64) < 1 {
		t.Fatalf("no reminder notification: %s", notes.raw)
	}
	expect(t, b.do("POST", "/api/v1/me/notifications/read", map[string]any{}), 204)
	expect(t, b.do("PATCH", "/api/v1/tasks/"+tid, map[string]any{"remind_at": nil}), 200)

	inbox := b.do("POST", "/api/v1/tasks", map[string]any{"title": "Inbox thing"})
	expect(t, inbox, 201)
	expect(t, b.do("POST", "/api/v1/tasks/"+inbox.str("id")+"/move", map[string]any{"project_id": pid}), 200)
	moved := b.do("POST", "/api/v1/tasks/"+inbox.str("id")+"/move", map[string]any{"column_id": doing, "before_id": tid})
	expect(t, moved, 200)
	if moved.str("status") != "doing" || moved.str("position") >= done0(b, tid) {
		t.Fatalf("drag result: %s", moved.raw)
	}
	expect(t, b.do("GET", "/api/v1/tasks?project_id="+pid+"&status=todo,doing&sort=due", nil), 200)
	expect(t, b.do("GET", "/api/v1/tasks/"+tid, nil), 200)

	// Idempotent replay.
	id := "01936b8e-0000-7000-8000-000000000001"
	expect(t, b.do("POST", "/api/v1/tasks", map[string]any{"id": id, "title": "Captured offline"}), 201)
	expect(t, b.do("POST", "/api/v1/tasks", map[string]any{"id": id, "title": "Captured offline"}), 200)

	// Checklist.
	ci := b.do("POST", "/api/v1/tasks/"+tid+"/checklist", map[string]any{"text": "Buy brackets"})
	expect(t, ci, 201)
	expect(t, b.do("PATCH", "/api/v1/checklist/"+ci.str("id"), map[string]any{"done": true}), 200)
	expect(t, b.do("GET", "/api/v1/tasks/"+tid+"/checklist", nil), 200)
	expect(t, b.do("DELETE", "/api/v1/checklist/"+ci.str("id"), nil), 204)

	// Complete -> Done column.
	done := b.do("PATCH", "/api/v1/tasks/"+tid, map[string]any{"status": "done"})
	expect(t, done, 200)
	if done.str("completed_at") == "" {
		t.Fatal("completed_at not set")
	}

	// Trash and restore.
	expect(t, b.do("DELETE", "/api/v1/tasks/"+tid, nil), 204)
	expect(t, b.do("GET", "/api/v1/tasks?deleted=true", nil), 200)
	expect(t, b.do("POST", "/api/v1/tasks/"+tid+"/restore", nil), 200)

	// Notes.
	n := b.do("POST", "/api/v1/notes", map[string]any{"body": "# Router upgrade\nCheck firmware first"})
	expect(t, n, 201)
	expect(t, b.do("PATCH", "/api/v1/notes/"+n.str("id"), map[string]any{"pinned": true}), 200)
	expect(t, b.do("GET", "/api/v1/notes", nil), 200)
	conv := b.do("POST", "/api/v1/notes/"+n.str("id")+"/convert", map[string]any{"delete_note": true})
	expect(t, conv, 201)
	if conv.str("title") != "Router upgrade" {
		t.Fatalf("convert title = %q", conv.str("title"))
	}

	// Search, parse, batch, export.
	expect(t, b.do("GET", "/api/v1/search?q=gutter", nil), 200)
	expect(t, b.do("POST", "/api/v1/quick-add/parse", map[string]any{"text": "Call dentist tomorrow #homelab"}), 200)
	batch := b.do("POST", "/api/v1/batch", map[string]any{"ops": []any{
		map[string]any{"op": "upsert", "type": "project", "ref": "p", "data": map[string]any{"name": "Work", "external_source": "vikunja", "external_id": "9"}},
		map[string]any{"op": "upsert", "type": "task", "data": map[string]any{"title": "Old", "project_ref": "p", "status": "done",
			"completed_at": "2025-01-02T03:04:05Z", "external_source": "vikunja", "external_id": "1"}},
	}})
	expect(t, batch, 200)
	bad := b.do("POST", "/api/v1/batch?dry_run=true", map[string]any{"ops": []any{map[string]any{"op": "create", "type": "task", "data": map[string]any{"title": ""}}}})
	expect(t, bad, 422)
	expect(t, b.do("GET", "/api/v1/export", nil), 200)
	csv := b.do("GET", "/api/v1/export?format=csv", nil)
	expect(t, csv, 200)
	if !strings.HasPrefix(string(csv.raw), "id,title") {
		t.Fatal("csv header missing")
	}

	// Sessions, audit, logout.
	expect(t, b.do("GET", "/api/v1/me/sessions", nil), 200)
	audit := b.do("GET", "/api/v1/me/audit", nil)
	expect(t, audit, 200)
	expect(t, b.do("DELETE", "/api/v1/projects/"+pid, nil), 204)
	expect(t, b.do("POST", "/api/v1/auth/logout", nil), 204)
	expect(t, b.do("GET", "/api/v1/me", nil), 401)
}

func TestTOTPLoginOverHTTP(t *testing.T) {
	h := newHarness(t, service.Options{Registration: "closed"})
	b := h.signedInAdmin()
	setup := b.do("POST", "/api/v1/me/2fa/totp", nil)
	expect(t, setup, 200)
	code, _ := auth.TOTPCodeAt(setup.str("secret"), h.now)
	conf := b.do("POST", "/api/v1/me/2fa/totp/confirm", map[string]any{"code": code})
	expect(t, conf, 200)
	codes := conf.body["recovery_codes"].([]any)
	expect(t, b.do("POST", "/api/v1/auth/logout", nil), 204)

	c := h.browser()
	login := c.do("POST", "/api/v1/auth/login", map[string]any{"email": "will@example.com", "password": pw})
	expect(t, login, 200)
	if login.body["mfa_required"] != true {
		t.Fatal("expected mfa_required")
	}
	expect(t, c.do("GET", "/api/v1/me", nil), 401) // pre-auth cookie grants nothing
	if c.do("GET", "/api/v1/auth/status", nil).body["mfa_pending"] != true {
		t.Fatal("status should show mfa_pending")
	}
	expect(t, c.do("POST", "/api/v1/auth/login/mfa", map[string]any{"code": "000000"}), 401)
	expect(t, c.do("POST", "/api/v1/auth/login/mfa", map[string]any{"recovery_code": codes[0]}), 200)
	expect(t, c.do("GET", "/api/v1/me", nil), 200)
	me := c.do("GET", "/api/v1/me", nil)
	if me.body["recovery_codes_remaining"].(float64) != 9 {
		t.Fatalf("remaining = %v", me.body["recovery_codes_remaining"])
	}
}

func TestTokensAndScopes(t *testing.T) {
	h := newHarness(t, service.Options{Registration: "closed"})
	b := h.signedInAdmin()
	ro := b.do("POST", "/api/v1/me/tokens", map[string]any{"name": "reader", "scopes": []string{"tasks:read"}})
	expect(t, ro, 201)
	tok := ro.str("token")
	if !strings.HasPrefix(tok, "blair_pat_") {
		t.Fatalf("token = %q", tok)
	}
	expect(t, b.do("GET", "/api/v1/me/tokens", nil), 200)

	r := h.token(tok)
	expect(t, r.do("GET", "/api/v1/tasks", nil), 200)
	expect(t, r.do("POST", "/api/v1/tasks", map[string]any{"title": "nope"}), 403)
	expect(t, r.do("GET", "/api/v1/notes", nil), 403)
	expect(t, r.do("GET", "/api/v1/me", nil), 403)          // needs account:read
	expect(t, r.do("GET", "/api/v1/me/sessions", nil), 403) // session only
	expect(t, r.do("POST", "/api/v1/me/tokens", map[string]any{"name": "x", "scopes": []string{"tasks:read"}}), 403)
	expect(t, r.do("GET", "/api/v1/admin/users", nil), 403) // admin never via token

	rw := b.do("POST", "/api/v1/me/tokens", map[string]any{"name": "writer", "scopes": []string{"tasks:read", "tasks:write"}})
	w := h.token(rw.str("token"))
	expect(t, w.do("POST", "/api/v1/tasks", map[string]any{"title": "via token"}), 201) // no CSRF header needed for bearer
	notes := w.do("POST", "/api/v1/batch", map[string]any{"ops": []any{map[string]any{"op": "create", "type": "note", "data": map[string]any{"body": "x"}}}})
	expect(t, notes, 403)

	expect(t, b.do("DELETE", "/api/v1/me/tokens/"+ro.str("id"), nil), 204)
	expect(t, r.do("GET", "/api/v1/tasks", nil), 401)
	expect(t, h.token("blair_pat_garbage").do("GET", "/api/v1/tasks", nil), 401)
}

func TestCSRFAndInputHardening(t *testing.T) {
	h := newHarness(t, service.Options{Registration: "closed"})
	b := h.signedInAdmin()

	// Missing custom header on a cookie request.
	delete(b.header, "X-Requested-With")
	expect(t, b.do("POST", "/api/v1/tasks", map[string]any{"title": "x"}), 403)
	b.header["X-Requested-With"] = "blair"

	// Cross-site browser request.
	b.header["Sec-Fetch-Site"] = "cross-site"
	b.header["Origin"] = "https://evil.example"
	expect(t, b.do("POST", "/api/v1/tasks", map[string]any{"title": "x"}), 403)
	delete(b.header, "Sec-Fetch-Site")
	delete(b.header, "Origin")

	// Unknown fields are rejected.
	expect(t, b.do("POST", "/api/v1/tasks", map[string]any{"title": "x", "titel": "typo"}), 400)
	// Validation errors carry field paths.
	v := b.do("POST", "/api/v1/tasks", map[string]any{"title": ""})
	expect(t, v, 422)
	if !strings.Contains(string(v.raw), `"field":"title"`) {
		t.Fatalf("validation body: %s", v.raw)
	}
	// Unknown API route is a JSON 404.
	if res, err := http.Get(h.srv.URL + "/api/v1/nope"); err != nil || res.StatusCode != 404 || !strings.Contains(res.Header.Get("Content-Type"), "problem+json") {
		t.Fatalf("unknown route: %v %v", res, err)
	}
	// Anonymous access.
	expect(t, h.browser().do("GET", "/api/v1/tasks", nil), 401)
	// Setup is single-use.
	tok, _ := h.svc.SetupToken(context.Background())
	expect(t, h.browser().do("POST", "/api/v1/auth/setup", map[string]any{"token": tok, "email": "x@example.com", "password": pw}), 403)
	// Registration closed.
	expect(t, h.browser().do("POST", "/api/v1/auth/register", map[string]any{"email": "y@example.com", "password": pw}), 403)
}

func TestLoginRateLimit(t *testing.T) {
	h := newHarness(t, service.Options{Registration: "closed"})
	h.signedInAdmin()
	c := h.browser()
	var last resp
	for i := 0; i < 7; i++ {
		last = c.do("POST", "/api/v1/auth/login", map[string]any{"email": "will@example.com", "password": "wrong password!!"})
	}
	expect(t, last, 429)
	if last.header.Get("Retry-After") == "" {
		t.Fatal("missing Retry-After")
	}
}

func TestSecurityHeadersAndSPA(t *testing.T) {
	h := newHarness(t, service.Options{Registration: "closed"})
	res, err := http.Get(h.srv.URL + "/today")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "same-origin",
	} {
		if got := res.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	csp := res.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "unsafe-eval") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
		t.Errorf("weak CSP: %s", csp)
	}
	res, _ = http.Get(h.srv.URL + "/api/v1/auth/status")
	res.Body.Close()
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Error("API responses must be no-store")
	}
	res, _ = http.Get(h.srv.URL + "/missing.js")
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Errorf("missing asset = %d, want 404", res.StatusCode)
	}
	res, _ = http.Get(h.srv.URL + "/api/openapi.json")
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Errorf("openapi.json = %d", res.StatusCode)
	}
}

func TestSPAHashesInlineScripts(t *testing.T) {
	s := newSPA(nil).(*spa)
	if strings.Contains(s.csp, "sha256-") {
		t.Fatal("placeholder page has no inline scripts")
	}
	html := []byte(`<html><script>console.log(1)</script><script src="/a.js"></script></html>`)
	s2 := &spa{index: html}
	_ = s2
	m := inlineScript.FindAllSubmatch(html, -1)
	if len(m) != 2 || string(m[0][1]) != "console.log(1)" {
		t.Fatalf("matches: %q", m)
	}
}

func TestInvitesAndAdmin(t *testing.T) {
	h := newHarness(t, service.Options{Registration: "invite"})
	b := h.signedInAdmin()
	inv := b.do("POST", "/api/v1/admin/invites", map[string]any{"email": "friend@example.com"})
	expect(t, inv, 201)
	expect(t, b.do("GET", "/api/v1/admin/invites", nil), 200)
	f := h.browser()
	expect(t, f.do("POST", "/api/v1/auth/register", map[string]any{"invite_token": inv.str("token"), "email": "friend@example.com", "password": pw}), 201)
	expect(t, f.do("GET", "/api/v1/admin/users", nil), 403)
	users := b.do("GET", "/api/v1/admin/users", nil)
	expect(t, users, 200)
	var friendID string
	for _, u := range users.body["data"].([]any) {
		if m := u.(map[string]any); m["email"] == "friend@example.com" {
			friendID = m["id"].(string)
		}
	}
	expect(t, b.do("PATCH", "/api/v1/admin/users/"+friendID, map[string]any{"disabled": true}), 200)
	expect(t, f.do("GET", "/api/v1/tasks", nil), 401) // sessions revoked on disable
	expect(t, b.do("DELETE", "/api/v1/admin/users/"+friendID, nil), 204)
}

func TestTrustedProxyClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	var got string
	h := realIP(trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = clientIP(r.Context()) }))
	cases := []struct{ remote, xff, want string }{
		{"10.0.0.5:1234", "203.0.113.9", "203.0.113.9"},
		{"10.0.0.5:1234", "198.51.100.1, 203.0.113.9", "203.0.113.9"},
		{"10.0.0.5:1234", "203.0.113.9, 10.0.0.7", "203.0.113.9"},
		{"192.0.2.1:1234", "203.0.113.9", "192.0.2.1"}, // untrusted peer: header ignored
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		r.Header.Set("X-Forwarded-For", c.xff)
		h.ServeHTTP(httptest.NewRecorder(), r)
		if got != c.want {
			t.Errorf("remote %s xff %q: got %s want %s", c.remote, c.xff, got, c.want)
		}
	}
}

// done0 returns a task's current position (helper for ordering assertions).
func done0(c *client, id string) string {
	return c.do("GET", "/api/v1/tasks/"+id, nil).str("position")
}
