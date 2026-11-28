package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wbollock/blair/api"
	"github.com/wbollock/blair/internal/auth"
	"github.com/wbollock/blair/internal/service"
)

const (
	mcpCallback = "https://claude.ai/api/mcp/auth_callback"
	mcpVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
)

var formFields = regexp.MustCompile(`name="(req|csrf)" value="([^"]+)"`)

// mcpUser is an admin with 2FA on, which connecting an app requires.
type mcpUser struct {
	b      *client
	secret string
}

func (h *harness) mcpAdmin() *mcpUser {
	h.t.Helper()
	b := h.signedInAdmin()
	setup := b.do("POST", "/api/v1/me/2fa/totp", nil)
	expect(h.t, setup, 200)
	code, _ := auth.TOTPCodeAt(setup.str("secret"), h.now)
	expect(h.t, b.do("POST", "/api/v1/me/2fa/totp/confirm", map[string]any{"code": code}), 200)
	return &mcpUser{b: b, secret: setup.str("secret")}
}

func (u *mcpUser) code(h *harness) string {
	h.now = h.now.Add(31 * time.Second) // a code cannot be used twice
	c, _ := auth.TOTPCodeAt(u.secret, h.now)
	return c
}

// connectMCP runs the real OAuth flow (register, sign in, consent, token) and returns the token response.
func connectMCP(t *testing.T, h *harness, u *mcpUser, scope string) map[string]any {
	t.Helper()
	expect(t, u.b.do("POST", "/api/v1/me/connected-apps/registration", nil), 200)
	post := func(path string, body any) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		res, err := http.Post(h.srv.URL+path, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	st, reg := post("/oauth/register", map[string]any{"client_name": "Claude", "redirect_uris": []string{mcpCallback}, "token_endpoint_auth_method": "none"})
	if st != 201 {
		t.Fatalf("register: %d %v", st, reg)
	}
	cid := reg["client_id"].(string)

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	sum := sha256.Sum256([]byte(mcpVerifier))
	q := url.Values{"response_type": {"code"}, "client_id": {cid}, "redirect_uri": {mcpCallback}, "state": {"s"}, "resource": {h.srv.URL + "/mcp"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	if scope != "" {
		q.Set("scope", scope)
	}
	step := func(res *http.Response, err error, action string, extra url.Values) *http.Response {
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		f := url.Values{"action": {action}}
		for _, m := range formFields.FindAllStringSubmatch(string(b), -1) {
			f.Set(m[1], m[2])
		}
		for k, v := range extra {
			f[k] = v
		}
		out, err := c.PostForm(h.srv.URL+"/oauth/authorize", f)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	res, err := c.Get(h.srv.URL + "/oauth/authorize?" + q.Encode())
	res = step(res, err, "login", url.Values{"email": {"will@example.com"}, "password": {pw}})
	res = step(res, nil, "mfa", url.Values{"code": {u.code(h)}})
	res = step(res, nil, "approve", nil)
	loc, _ := url.Parse(res.Header.Get("Location"))
	code := loc.Query().Get("code")
	if res.StatusCode != 302 || code == "" {
		t.Fatalf("authorize: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	tr, err := http.PostForm(h.srv.URL+"/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {cid}, "code": {code},
		"redirect_uri": {mcpCallback}, "code_verifier": {mcpVerifier}, "resource": {h.srv.URL + "/mcp"}})
	if err != nil {
		t.Fatal(err)
	}
	defer tr.Body.Close()
	var tok map[string]any
	_ = json.NewDecoder(tr.Body).Decode(&tok)
	if tr.StatusCode != 200 {
		t.Fatalf("token: %d %v", tr.StatusCode, tok)
	}
	tok["client_id"] = cid
	return tok
}

// rpc posts one JSON-RPC request to /mcp.
func rpc(t *testing.T, h *harness, bearer, method string, params any) (int, map[string]any, http.Header) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequest("POST", h.srv.URL+"/mcp", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, out, res.Header
}

func TestMCPEndToEnd(t *testing.T) {
	h := newHarnessWith(t, service.Options{Registration: "closed"}, true)
	u := h.mcpAdmin()
	b := u.b
	tok := connectMCP(t, h, u, "")
	access := tok["access_token"].(string)

	// Discovery documents point at each other.
	pr, _ := http.Get(h.srv.URL + "/.well-known/oauth-protected-resource/mcp")
	var prm map[string]any
	_ = json.NewDecoder(pr.Body).Decode(&prm)
	if prm["resource"] != h.srv.URL+"/mcp" {
		t.Fatalf("protected resource metadata: %v", prm)
	}

	// The challenge a client gets without a token.
	st, _, hdr := rpc(t, h, "", "tools/list", nil)
	if want := `resource_metadata="` + h.srv.URL + `/.well-known/oauth-protected-resource/mcp"`; st != 401 || !strings.Contains(hdr.Get("WWW-Authenticate"), want) {
		t.Fatalf("challenge: %d %q", st, hdr.Get("WWW-Authenticate"))
	}

	st, out, _ := rpc(t, h, access, "tools/call", map[string]any{"name": "create_task", "arguments": map[string]any{"title": "from claude.ai"}})
	if st != 200 || out["error"] != nil || out["result"].(map[string]any)["isError"] == true {
		t.Fatalf("create_task: %d %v", st, out)
	}
	list := b.do("GET", "/api/v1/tasks?q=claude", nil)
	expect(t, list, 200)
	if !strings.Contains(string(list.raw), "from claude.ai") {
		t.Fatalf("task created over MCP is not in the app: %s", list.raw)
	}
	// The write is in the user's audit log, without the title.
	audit := b.do("GET", "/api/v1/me/audit", nil)
	for _, want := range []string{"oauth.authorize", "oauth.token_issued", "mcp.write"} {
		if !strings.Contains(string(audit.raw), want) {
			t.Errorf("audit log has no %s event", want)
		}
	}
	if strings.Contains(string(audit.raw), "from claude.ai") {
		t.Error("audit log carries task content")
	}
	// A PAT is not an MCP credential.
	pat := b.do("POST", "/api/v1/me/tokens", map[string]any{"name": "p", "scopes": []string{"tasks:read"}}).str("token")
	if st, _, _ := rpc(t, h, pat, "tools/list", nil); st != 401 {
		t.Errorf("PAT at /mcp: %d", st)
	}
}

// The access token and the refresh token must be useless against the REST API, in
// particular on every DELETE route.
func TestMCPTokensAreRefusedByTheRESTAPI(t *testing.T) {
	h := newHarnessWith(t, service.Options{Registration: "closed"}, true)
	u := h.mcpAdmin()
	b := u.b
	tok := connectMCP(t, h, u, "")
	made := b.do("POST", "/api/v1/tasks", map[string]any{"title": "keep me"})
	expect(t, made, 201)

	for _, bearer := range []string{tok["access_token"].(string), tok["refresh_token"].(string)} {
		c := h.token(bearer)
		for _, p := range deleteRoutes(t) {
			expect(t, c.do("DELETE", p, nil), 403)
		}
		expect(t, c.do("DELETE", "/api/v1/tasks/"+made.str("id")+"?permanent=true", nil), 403)
		expect(t, c.do("POST", "/api/v1/batch", map[string]any{"ops": []any{map[string]any{"op": "delete", "type": "task", "id": made.str("id")}}}), 403)
		expect(t, c.do("GET", "/api/v1/tasks", nil), 403)
		expect(t, c.do("POST", "/api/v1/tasks", map[string]any{"title": "x"}), 403)
		expect(t, c.do("GET", "/api/v1/me/connected-apps", nil), 403)
		r := c.do("GET", "/api/v1/tasks", nil)
		if !strings.Contains(string(r.raw), "oauth_token_not_accepted") {
			t.Errorf("unhelpful refusal: %s", r.raw)
		}
	}
	// And every path in the contract, with every method it documents.
	doc, _ := api.Load()
	c := h.token(tok["access_token"].(string))
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			if strings.HasPrefix(path, "/auth/") || path == "/healthz" || path == "/readyz" {
				continue
			}
			p := "/api/v1" + pathParam.ReplaceAllString(path, "0198b2a0-0000-7000-8000-000000000000")
			if r := c.do(method, p, nil); r.status != 403 && r.status != 404 {
				t.Errorf("%s %s with an OAuth token: %d, want 403", method, p, r.status)
			}
		}
	}
	expect(t, b.do("GET", "/api/v1/tasks/"+made.str("id"), nil), 200)
}

func TestMCPCannotDeleteAndHonoursReadOnlyGrant(t *testing.T) {
	h := newHarnessWith(t, service.Options{Registration: "closed"}, true)
	u := h.mcpAdmin()
	b := u.b
	made := b.do("POST", "/api/v1/tasks", map[string]any{"title": "precious"})
	tok := connectMCP(t, h, u, "tasks:read")
	access := tok["access_token"].(string)
	if tok["scope"] != "tasks:read" {
		t.Fatalf("scope: %v", tok["scope"])
	}
	_, out, _ := rpc(t, h, access, "tools/call", map[string]any{"name": "create_task", "arguments": map[string]any{"title": "nope"}})
	if out["result"].(map[string]any)["isError"] != true {
		t.Fatalf("read-only grant created a task: %v", out)
	}
	_, out, _ = rpc(t, h, access, "tools/call", map[string]any{"name": "delete_task", "arguments": map[string]any{"id": made.str("id")}})
	if out["error"] == nil && out["result"].(map[string]any)["isError"] != true {
		t.Fatalf("a delete tool exists: %v", out)
	}
	expect(t, b.do("GET", "/api/v1/tasks/"+made.str("id"), nil), 200)
}

func TestConnectedAppsRevokeCutsOffMCP(t *testing.T) {
	h := newHarnessWith(t, service.Options{Registration: "closed"}, true)
	u := h.mcpAdmin()
	b := u.b
	tok := connectMCP(t, h, u, "")
	access := tok["access_token"].(string)

	apps := b.do("GET", "/api/v1/me/connected-apps", nil)
	expect(t, apps, 200)
	data := apps.body["data"].([]any)
	if len(data) != 1 || data[0].(map[string]any)["name"] != "Claude" {
		t.Fatalf("connected apps: %s", apps.raw)
	}
	id := data[0].(map[string]any)["id"].(string)
	if st, _, _ := rpc(t, h, access, "tools/list", nil); st != 200 {
		t.Fatalf("before revoke: %d", st)
	}
	expect(t, b.do("DELETE", "/api/v1/me/connected-apps/"+id, nil), 204)
	if st, _, _ := rpc(t, h, access, "tools/list", nil); st != 401 {
		t.Fatalf("after revoke: %d, want 401", st)
	}
	expect(t, b.do("DELETE", "/api/v1/me/connected-apps/"+id, nil), 404)
	if r := b.do("GET", "/api/v1/me/audit", nil); !strings.Contains(string(r.raw), "oauth.revoke") {
		t.Error("revocation is not in the audit log")
	}
	r := h.token(b.do("POST", "/api/v1/me/tokens", map[string]any{"name": "p", "scopes": []string{"tasks:read"}}).str("token"))
	expect(t, r.do("GET", "/api/v1/me/connected-apps", nil), 403) // browser session only
}

func TestMCPIsOffByDefault(t *testing.T) {
	h := newHarness(t, service.Options{Registration: "closed"})
	for _, c := range []struct{ method, path string }{
		{"POST", "/mcp"}, {"GET", "/.well-known/oauth-authorization-server"}, {"GET", "/.well-known/oauth-protected-resource/mcp"},
		{"POST", "/oauth/register"}, {"GET", "/oauth/authorize"}, {"POST", "/oauth/token"},
	} {
		req, _ := http.NewRequest(c.method, h.srv.URL+c.path, strings.NewReader("{}"))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 404 {
			t.Errorf("%s %s with BLAIR_MCP_ENABLED off: %d, want 404", c.method, c.path, res.StatusCode)
		}
	}
	u := h.mcpAdmin()
	b := u.b
	r := b.do("GET", "/api/v1/me/connected-apps", nil)
	expect(t, r, 200)
}

func TestRegistrationEndpointNeedsA2FASession(t *testing.T) {
	h := newHarnessWith(t, service.Options{Registration: "closed"}, true)
	b := h.signedInAdmin() // no 2FA yet
	expect(t, b.do("POST", "/api/v1/me/connected-apps/registration", nil), 403)
	tok := h.token(b.do("POST", "/api/v1/me/tokens", map[string]any{"name": "t", "scopes": []string{"tasks:write"}}).str("token"))
	expect(t, tok.do("POST", "/api/v1/me/connected-apps/registration", nil), 403) // tokens never open registration

	// Without an open window the connector cannot register at all.
	post, _ := http.Post(h.srv.URL+"/oauth/register", "application/json", strings.NewReader(`{"redirect_uris":["`+mcpCallback+`"]}`))
	if post.StatusCode != 403 {
		t.Fatalf("register without a window: %d", post.StatusCode)
	}
}

// With an address list, the connector's endpoints refuse everyone else; the sign-in page stays open.
func TestMCPAddressAllowList(t *testing.T) {
	h := newHarnessCfg(t, service.Options{Registration: "closed"}, true, func(c *Config) {
		c.MCPCIDRs = []netip.Prefix{netip.MustParsePrefix("160.79.104.0/21")}
	})
	srv := h.srv
	for _, c := range []struct{ method, path string }{{"POST", "/mcp"}, {"POST", "/oauth/token"}, {"POST", "/oauth/register"}, {"POST", "/oauth/revoke"}, {"GET", "/.well-known/oauth-authorization-server"}} {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, strings.NewReader("{}"))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 403 {
			t.Errorf("%s %s from outside the list: %d, want 403", c.method, c.path, res.StatusCode)
		}
	}
	res, _ := http.Get(srv.URL + "/oauth/authorize")
	if res.StatusCode == 403 {
		t.Error("the sign-in page must stay reachable from a browser anywhere")
	}
	req, _ := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader("{}"))
	req.Header.Set("X-Forwarded-For", "160.79.104.9")
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 401 { // reaches the auth check
		t.Errorf("an address inside the list: %d, want 401", res.StatusCode)
	}
}

// Logs, audit rows and metrics must never carry tokens, codes, passwords or task text.
func TestMCPAndOAuthLogsCarryNoSecretsOrContent(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	h := newHarnessWith(t, service.Options{Registration: "closed"}, true)
	u := h.mcpAdmin()
	tok := connectMCP(t, h, u, "")
	access, refresh := tok["access_token"].(string), tok["refresh_token"].(string)
	rpc(t, h, access, "tools/call", map[string]any{"name": "create_task", "arguments": map[string]any{"title": "secret-title-xyz", "description": "secret-body-xyz"}})
	rpc(t, h, "blair_oat_wrongwrongwrongwrongwrongwrongwrongwrongwrong", "tools/list", nil)
	// A replayed refresh token is the event most worth alerting on; it must be visible but not leak.
	cid := tok["client_id"].(string)
	for range 2 {
		resp, _ := http.PostForm(h.srv.URL+"/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {refresh}})
		resp.Body.Close()
	}

	out := buf.String()
	for _, secret := range []string{access, refresh, "blair_oat_", "blair_ort_", "blair_oac_", pw, "secret-title-xyz", "secret-body-xyz", u.secret, "wrongwrong"} {
		if strings.Contains(out, secret) {
			t.Fatalf("log output contains %q", secret)
		}
	}
	for _, want := range []string{`"msg":"oauth security event"`, `"event":"oauth.refresh_reuse"`, `"msg":"mcp tool"`, `"msg":"mcp request refused"`, `"outcome":"unauthorized"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected log line missing: %s", want)
		}
	}
}
