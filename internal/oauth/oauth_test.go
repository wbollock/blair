package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/wbollock/blair/internal/auth"
	"github.com/wbollock/blair/internal/ratelimit"
	"github.com/wbollock/blair/internal/service"
	"github.com/wbollock/blair/internal/store"
)

const (
	testPW   = "plum-orbit-kettle-91"
	callback = "https://claude.ai/api/mcp/auth_callback"
)

type env struct {
	t    *testing.T
	srv  *httptest.Server
	o    *Server
	svc  *service.Service
	db   *store.DB
	user *service.User
	now  time.Time
	totp string // the user's 2FA secret; empty when the env was built without 2FA
}

func newEnv(t *testing.T) *env { return newEnvWith(t, true) }

func newEnvWith(t *testing.T, withTOTP bool) *env {
	t.Helper()
	db, err := store.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := auth.NewSecretBox(bytes.Repeat([]byte{7}, 32), "totp")
	svc := service.New(db, box, service.Options{Registration: "closed", Issuer: "Blair"})
	e := &env{t: t, svc: svc, db: db, now: time.Now()}
	svc.SetClock(func() time.Time { return e.now })

	router := chi.NewRouter()
	e.srv = httptest.NewServer(router)
	t.Cleanup(e.srv.Close)
	base, _ := url.Parse(e.srv.URL)
	e.o = New(db, svc, Config{BaseURL: base, ClientIP: func(*http.Request) string { return "203.0.113.5" }, Now: func() time.Time { return e.now }})
	e.o.Routes(router)

	e.user, err = svc.CreateUser(context.Background(), service.NewUser{Email: "will@example.com", Password: testPW, Timezone: "UTC"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if withTOTP {
		ctx := context.Background()
		setup, err := svc.BeginTOTP(ctx, e.user)
		if err != nil {
			t.Fatal(err)
		}
		u, _ := svc.GetUser(ctx, e.user.ID)
		cc, _ := auth.TOTPCodeAt(setup.Secret, e.now)
		if _, err := svc.ConfirmTOTP(ctx, u, cc); err != nil {
			t.Fatal(err)
		}
		e.totp = setup.Secret
		e.user, _ = svc.GetUser(ctx, e.user.ID)
	}
	return e
}

// code returns a fresh TOTP code. Codes cannot be replayed, so the clock moves to the next step.
func (e *env) code() string {
	e.now = e.now.Add(31 * time.Second)
	c, _ := auth.TOTPCodeAt(e.totp, e.now)
	return c
}

func (e *env) browser() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func (e *env) postJSON(path string, body any) (int, map[string]any) {
	e.t.Helper()
	raw, _ := json.Marshal(body)
	res, err := http.Post(e.srv.URL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func (e *env) register(redirect string) string {
	e.t.Helper()
	if _, err := e.o.OpenRegistration(context.Background(), e.user.ID); err != nil {
		e.t.Fatal(err)
	}
	code, out := e.postJSON("/oauth/register", map[string]any{"client_name": "Claude", "redirect_uris": []string{redirect}, "token_endpoint_auth_method": "none"})
	if code != 201 {
		e.t.Fatalf("register: %d %v", code, out)
	}
	return out["client_id"].(string)
}

func (e *env) form(path string, v url.Values) (int, map[string]any, http.Header) {
	e.t.Helper()
	res, err := http.PostForm(e.srv.URL+path, v)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out, res.Header
}

const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

func challengeOf(v string) string { return pkceChallenge(v) }

func (e *env) authorizeURL(clientID string, extra url.Values) string {
	q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {callback}, "state": {"xyz"},
		"code_challenge": {challengeOf(verifier)}, "code_challenge_method": {"S256"}, "resource": {e.o.Resource()}}
	for k, v := range extra {
		q[k] = v
	}
	return e.srv.URL + "/oauth/authorize?" + q.Encode()
}

var hidden = regexp.MustCompile(`name="(req|csrf)" value="([^"]+)"`)

func fields(t *testing.T, body string) url.Values {
	t.Helper()
	v := url.Values{}
	for _, m := range hidden.FindAllStringSubmatch(body, -1) {
		v.Set(m[1], m[2])
	}
	if v.Get("req") == "" || v.Get("csrf") == "" {
		t.Fatalf("no form in page: %s", regexp.MustCompile(`<main>.*</main>`).FindString(strings.ReplaceAll(body, "\n", " ")))
	}
	return v
}

func read(res *http.Response) string {
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

func post(t *testing.T, c *http.Client, e *env, v url.Values) *http.Response {
	t.Helper()
	res, err := c.PostForm(e.srv.URL+"/oauth/authorize", v)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// connect runs the whole browser flow and returns the authorization code.
func (e *env) connect(clientID string, extra url.Values) string {
	e.t.Helper()
	c := e.browser()
	res, err := c.Get(e.authorizeURL(clientID, extra))
	if err != nil {
		e.t.Fatal(err)
	}
	f := fields(e.t, read(res))
	f.Set("action", "login")
	f.Set("email", "will@example.com")
	f.Set("password", testPW)
	res = post(e.t, c, e, f)
	f = fields(e.t, read(res))
	f.Set("action", "mfa")
	f.Set("code", e.code())
	res = post(e.t, c, e, f)
	f = fields(e.t, read(res))
	f.Set("action", "approve")
	res = post(e.t, c, e, f)
	loc, _ := url.Parse(res.Header.Get("Location"))
	if res.StatusCode != 302 || loc.Query().Get("code") == "" {
		e.t.Fatalf("no code: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	if loc.Query().Get("state") != "xyz" || loc.Query().Get("iss") != e.o.Issuer() {
		e.t.Fatalf("state/iss missing: %s", loc)
	}
	return loc.Query().Get("code")
}

func (e *env) exchange(clientID, code, ver string) (int, map[string]any) {
	st, out, _ := e.form("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {code},
		"redirect_uri": {callback}, "code_verifier": {ver}})
	return st, out
}

func (e *env) refresh(clientID, tok string) (int, map[string]any) {
	st, out, _ := e.form("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {tok}})
	return st, out
}

func TestMetadata(t *testing.T) {
	e := newEnv(t)
	get := func(p string) map[string]any {
		res, err := http.Get(e.srv.URL + p)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("%s: %v %v", p, err, res)
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(read(res)), &m)
		return m
	}
	as := get("/.well-known/oauth-authorization-server")
	if as["issuer"] != e.o.Issuer() || as["registration_endpoint"] != e.o.Issuer()+"/oauth/register" {
		t.Fatalf("metadata: %v", as)
	}
	if m := as["code_challenge_methods_supported"].([]any); len(m) != 1 || m[0] != "S256" {
		t.Fatalf("PKCE methods: %v", m)
	}
	if m := as["token_endpoint_auth_methods_supported"].([]any); len(m) != 1 || m[0] != "none" {
		t.Fatalf("auth methods: %v", m)
	}
	for _, p := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		pr := get(p)
		if pr["resource"] != e.o.Resource() || pr["authorization_servers"].([]any)[0] != e.o.Issuer() {
			t.Fatalf("%s: %v", p, pr)
		}
	}
	if e.o.MetadataURL() != e.o.Issuer()+"/.well-known/oauth-protected-resource/mcp" {
		t.Fatal(e.o.MetadataURL())
	}
}

func TestRegistrationOnlyAcceptsClaudeCallbacks(t *testing.T) {
	e := newEnv(t)
	e.o.regIP = ratelimit.New(time.Hour, 1000) // this test sends many requests from one address
	e.openWindow()
	for _, ok := range []string{"https://claude.ai/api/mcp/auth_callback", "https://claude.com/api/mcp/auth_callback"} {
		e.openWindow()
		e.register(ok)
	}
	e.openWindow()
	for _, bad := range []string{"https://evil.example/cb", "http://claude.ai/api/mcp/auth_callback", "https://claude.ai/api/mcp/auth_callback/",
		"https://claude.ai.evil.example/api/mcp/auth_callback", "https://claude.ai/api/mcp/auth_callback?x=1", "http://localhost:6274/oauth/callback"} {
		if st, out := e.postJSON("/oauth/register", map[string]any{"redirect_uris": []string{bad}}); st != 400 || out["error"] != "invalid_redirect_uri" {
			t.Errorf("%s: %d %v", bad, st, out)
		}
	}
	if st, out := e.postJSON("/oauth/register", map[string]any{"redirect_uris": []string{callback, "https://evil.example/cb"}}); st != 400 {
		t.Errorf("one bad uri among good ones: %d %v", st, out)
	}
	if st, _ := e.postJSON("/oauth/register", map[string]any{"redirect_uris": []string{callback}, "token_endpoint_auth_method": "client_secret_basic"}); st != 400 {
		t.Error("confidential clients must be refused")
	}
	if st, _ := e.postJSON("/oauth/register", map[string]any{"redirect_uris": []string{callback}, "scope": "tasks:delete"}); st != 400 {
		t.Error("tasks:delete must not be registrable")
	}
}

func TestRegistrationRateLimit(t *testing.T) {
	e := newEnv(t)
	for i := range 10 {
		e.openWindow()
		if st, _ := e.postJSON("/oauth/register", map[string]any{"redirect_uris": []string{callback}}); st != 201 {
			t.Fatalf("registration %d: %d", i, st)
		}
	}
	e.openWindow()
	if st, _ := e.postJSON("/oauth/register", map[string]any{"redirect_uris": []string{callback}}); st != 429 {
		t.Fatalf("11th registration: %d, want 429", st)
	}
}

func (e *env) openWindow() {
	e.t.Helper()
	if _, err := e.o.OpenRegistration(context.Background(), e.user.ID); err != nil {
		e.t.Fatal(err)
	}
}

// Nobody can register a client unless the owner opened the window, and a window holds three uses
// and lasts ten minutes. Otherwise a stranger could create an app for the owner to approve.
func TestRegistrationNeedsAnOpenWindow(t *testing.T) {
	e := newEnv(t)
	body := map[string]any{"redirect_uris": []string{callback}}
	if st, out := e.postJSON("/oauth/register", body); st != 403 || out["error"] != "access_denied" {
		t.Fatalf("no window: %d %v", st, out)
	}
	e.openWindow()
	for i := range 3 {
		if st, _ := e.postJSON("/oauth/register", body); st != 201 {
			t.Fatalf("use %d: %d", i, st)
		}
	}
	if st, _ := e.postJSON("/oauth/register", body); st != 403 {
		t.Fatalf("4th use: %d, want 403", st)
	}
	e.openWindow()
	e.now = e.now.Add(11 * time.Minute)
	if st, _ := e.postJSON("/oauth/register", body); st != 403 {
		t.Fatalf("expired window: %d, want 403", st)
	}
}

// Only the account that opened registration can approve the client.
func TestOnlyTheOwnerCanApproveTheClient(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	other, err := e.svc.CreateUser(context.Background(), service.NewUser{Email: "other@example.com", Password: "plum-orbit-kettle-92", Timezone: "UTC"}, false)
	if err != nil {
		t.Fatal(err)
	}
	c := e.browser()
	res, _ := c.Get(e.authorizeURL(cid, nil))
	f := fields(t, read(res))
	f.Set("action", "login")
	f.Set("email", other.Email)
	f.Set("password", "plum-orbit-kettle-92")
	res = post(t, c, e, f)
	body := read(res)
	if res.StatusCode != 403 || !strings.Contains(body, "different account") || strings.Contains(body, `name="code"`) {
		t.Fatalf("another account got past login: %d %.200s", res.StatusCode, body)
	}
}

func TestAConnectionNeedsTwoFactor(t *testing.T) {
	e := newEnvWith(t, false)
	cid := e.register(callback)
	c := e.browser()
	res, _ := c.Get(e.authorizeURL(cid, nil))
	f := fields(t, read(res))
	f.Set("action", "login")
	f.Set("email", "will@example.com")
	f.Set("password", testPW)
	res = post(t, c, e, f)
	body := read(res)
	if res.StatusCode != 403 || !strings.Contains(body, "two-factor") || strings.Contains(body, "Allow") {
		t.Fatalf("an account without 2FA got through: %d %.200s", res.StatusCode, body)
	}
}

func TestPasswordChangeAnd2FAResetDisconnectApps(t *testing.T) {
	for _, tc := range []string{"password", "2fa", "admin password reset"} {
		e := newEnv(t)
		cid := e.register(callback)
		_, tok := e.exchange(cid, e.connect(cid, nil), verifier)
		ctx := context.Background()
		u, _ := e.svc.GetUser(ctx, e.user.ID)
		var err error
		switch tc {
		case "password":
			err = e.svc.ChangePassword(ctx, u, testPW, "plum-orbit-kettle-93", "")
		case "2fa":
			_, err = e.svc.AdminUpdateUser(ctx, &service.User{ID: "cli"}, u.ID, service.AdminUserPatch{Reset2FA: service.Some(true)})
		default:
			_, err = e.svc.AdminUpdateUser(ctx, &service.User{ID: "cli"}, u.ID, service.AdminUserPatch{Password: service.Some("plum-orbit-kettle-94")})
		}
		if err != nil {
			t.Fatalf("%s: %v", tc, err)
		}
		if _, err := e.o.VerifyAccessToken(ctx, tok["access_token"].(string)); err == nil {
			t.Errorf("%s: access token survived", tc)
		}
		if st, _ := e.refresh(cid, tok["refresh_token"].(string)); st != 400 {
			t.Errorf("%s: refresh token survived", tc)
		}
	}
}

func TestOnlyTenConnectionsPerAccount(t *testing.T) {
	e := newEnv(t)
	for _, l := range []**ratelimit.Limiter{&e.o.loginIP, &e.o.loginAcct, &e.o.mfaIP, &e.o.mfaAcct, &e.o.regIP} {
		*l = ratelimit.New(time.Hour, 1000) // eleven sign-ins from one address in a second
	}
	var first map[string]any
	for i := range 11 {
		cid := e.register(callback)
		_, tok := e.exchange(cid, e.connect(cid, nil), verifier)
		if i == 0 {
			first = tok
		}
	}
	if _, err := e.o.VerifyAccessToken(context.Background(), first["access_token"].(string)); err == nil {
		t.Fatal("the 11th connection did not push out the oldest")
	}
	if apps, _ := e.o.ListGrants(context.Background(), e.user.ID); len(apps) != 10 {
		t.Fatalf("%d live connections, want 10", len(apps))
	}
}

func TestFullFlowRefreshRotationAndReuse(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	code := e.connect(cid, nil)
	st, tok := e.exchange(cid, code, verifier)
	if st != 200 || tok["token_type"] != "Bearer" || tok["expires_in"] != float64(3600) || tok["scope"] != "tasks:read tasks:write" {
		t.Fatalf("token response: %d %v", st, tok)
	}
	access, refresh := tok["access_token"].(string), tok["refresh_token"].(string)
	if !strings.HasPrefix(access, "blair_oat_") || !strings.HasPrefix(refresh, "blair_ort_") {
		t.Fatal("token prefixes")
	}
	p, err := e.o.VerifyAccessToken(context.Background(), access)
	if err != nil || p.User.ID != e.user.ID || p.ClientID != cid || strings.Join(p.Scopes, " ") != "tasks:read tasks:write" {
		t.Fatalf("verify: %v %+v", err, p)
	}
	if time.Until(p.Expires) > time.Hour+time.Minute {
		t.Fatalf("access token lives %v", time.Until(p.Expires))
	}

	// Rotation: a new pair, and the old access token stops working.
	st, tok2 := e.refresh(cid, refresh)
	if st != 200 || tok2["refresh_token"] == refresh || tok2["access_token"] == access {
		t.Fatalf("refresh: %d %v", st, tok2)
	}
	if _, err := e.o.VerifyAccessToken(context.Background(), access); err == nil {
		t.Fatal("old access token still valid after rotation")
	}
	if _, err := e.o.VerifyAccessToken(context.Background(), tok2["access_token"].(string)); err != nil {
		t.Fatal(err)
	}

	// Reusing the old refresh token revokes the whole grant, including the new tokens.
	if st, out := e.refresh(cid, refresh); st != 400 || out["error"] != "invalid_grant" {
		t.Fatalf("reuse: %d %v", st, out)
	}
	if _, err := e.o.VerifyAccessToken(context.Background(), tok2["access_token"].(string)); err == nil {
		t.Fatal("grant survived refresh-token reuse")
	}
	if st, _ := e.refresh(cid, tok2["refresh_token"].(string)); st != 400 {
		t.Fatal("refresh token from a revoked grant still works")
	}
	apps, _ := e.o.ListGrants(context.Background(), e.user.ID)
	if len(apps) != 0 {
		t.Fatalf("revoked grant still listed: %+v", apps)
	}
}

func TestRefreshExpiresAfter30Days(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	_, tok := e.exchange(cid, e.connect(cid, nil), verifier)
	e.now = e.now.Add(29 * 24 * time.Hour)
	st, tok2 := e.refresh(cid, tok["refresh_token"].(string))
	if st != 200 {
		t.Fatalf("day 29: %d", st)
	}
	e.now = e.now.Add(31 * 24 * time.Hour)
	if st, _ := e.refresh(cid, tok2["refresh_token"].(string)); st != 400 {
		t.Fatalf("day 60: %d, want 400", st)
	}
}

func TestAccessTokenExpiresAfterAnHour(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	_, tok := e.exchange(cid, e.connect(cid, nil), verifier)
	e.now = e.now.Add(59 * time.Minute)
	if _, err := e.o.VerifyAccessToken(context.Background(), tok["access_token"].(string)); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(2 * time.Minute)
	if _, err := e.o.VerifyAccessToken(context.Background(), tok["access_token"].(string)); err == nil {
		t.Fatal("access token valid after 61 minutes")
	}
}

func TestCodeIsSingleUseAndShortLived(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)

	code := e.connect(cid, nil)
	e.now = e.now.Add(61 * time.Second)
	if st, out := e.exchange(cid, code, verifier); st != 400 || out["error"] != "invalid_grant" {
		t.Fatalf("expired code: %d %v", st, out)
	}

	code = e.connect(cid, nil)
	st, tok := e.exchange(cid, code, verifier)
	if st != 200 {
		t.Fatal(st)
	}
	if st, _ := e.exchange(cid, code, verifier); st != 400 {
		t.Fatal("code accepted twice")
	}
	// A replay revokes what the first exchange issued.
	if _, err := e.o.VerifyAccessToken(context.Background(), tok["access_token"].(string)); err == nil {
		t.Fatal("tokens survived a code replay")
	}
}

func TestPKCE(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	if st, out := e.exchange(cid, e.connect(cid, nil), "wrong-verifier-wrong-verifier-wrong-verifier-123"); st != 400 || out["error"] != "invalid_grant" {
		t.Fatalf("wrong verifier: %d %v", st, out)
	}
	if st, _ := e.exchange(cid, e.connect(cid, nil), ""); st != 400 {
		t.Fatal("missing verifier accepted")
	}

	c := e.browser()
	for name, q := range map[string]url.Values{
		"plain":     {"code_challenge_method": {"plain"}},
		"no method": {"code_challenge_method": {""}},
		"no chal":   {"code_challenge": {""}},
		"short":     {"code_challenge": {"abc"}},
	} {
		res, err := c.Get(e.authorizeURL(cid, q))
		if err != nil {
			t.Fatal(err)
		}
		loc, _ := url.Parse(res.Header.Get("Location"))
		if res.StatusCode != 302 || loc.Query().Get("error") != "invalid_request" || loc.Query().Get("state") != "xyz" {
			t.Errorf("%s: %d %s", name, res.StatusCode, res.Header.Get("Location"))
		}
	}
}

func TestAuthorizeRequestValidation(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	c := e.browser()
	redirects := map[string]url.Values{
		"delete scope":     {"scope": {"tasks:read tasks:delete"}},
		"other scope":      {"scope": {"notes:read"}},
		"wrong resource":   {"resource": {"https://blair.example/api/v1"}},
		"token response":   {"response_type": {"token"}},
		"duplicate params": nil,
	}
	delete(redirects, "duplicate params")
	want := map[string]string{"delete scope": "invalid_scope", "other scope": "invalid_scope", "wrong resource": "invalid_target", "token response": "unsupported_response_type"}
	for name, q := range redirects {
		res, _ := c.Get(e.authorizeURL(cid, q))
		loc, _ := url.Parse(res.Header.Get("Location"))
		if res.StatusCode != 302 || loc.Query().Get("error") != want[name] {
			t.Errorf("%s: %d %s", name, res.StatusCode, res.Header.Get("Location"))
		}
		if !strings.HasPrefix(res.Header.Get("Location"), callback) {
			t.Errorf("%s: error redirected somewhere else: %s", name, res.Header.Get("Location"))
		}
	}
	// Errors that must NOT redirect: the redirect URI cannot be trusted.
	for name, u := range map[string]string{
		"bad redirect":   e.authorizeURL(cid, url.Values{"redirect_uri": {"https://evil.example/cb"}}),
		"unknown client": e.authorizeURL("blair_c_nope", nil),
		"dup redirect":   e.authorizeURL(cid, nil) + "&redirect_uri=https://evil.example/cb",
	} {
		res, _ := c.Get(u)
		if res.StatusCode != 400 || res.Header.Get("Location") != "" {
			t.Errorf("%s: %d %q", name, res.StatusCode, res.Header.Get("Location"))
		}
	}
}

func TestConsentShowsEscapedClientNameAndSetsSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	e.openWindow()
	st, out := e.postJSON("/oauth/register", map[string]any{"client_name": `<script>alert(1)</script>`, "redirect_uris": []string{callback}})
	if st != 201 {
		t.Fatal(out)
	}
	c := e.browser()
	res, _ := c.Get(e.authorizeURL(out["client_id"].(string), nil))
	body := read(res)
	if strings.Contains(body, "<script>alert") || !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("client name not escaped: %.400s", body)
	}
	csp := res.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "unsafe-inline") || !strings.Contains(csp, "frame-ancestors 'none'") ||
		!strings.Contains(csp, "form-action 'self' https://claude.ai https://claude.com") {
		t.Fatalf("CSP: %s", csp)
	}
	if res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("headers: %v", res.Header)
	}
}

func TestCSRFAndBrowserBinding(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	c := e.browser()
	res, _ := c.Get(e.authorizeURL(cid, nil))
	f := fields(t, read(res))
	f.Set("action", "login")
	f.Set("email", "will@example.com")
	f.Set("password", testPW)

	bad := url.Values{}
	for k, v := range f {
		bad[k] = v
	}
	bad.Set("csrf", "wrong")
	if res := post(t, c, e, bad); res.StatusCode != 400 {
		t.Fatalf("wrong csrf: %d", res.StatusCode)
	}
	// Another browser (no binding cookie) cannot continue someone else's flow, even with both tokens.
	if res := post(t, e.browser(), e, f); res.StatusCode != 400 {
		t.Fatalf("no binding cookie: %d", res.StatusCode)
	}
	if res := post(t, c, e, f); res.StatusCode != 200 {
		t.Fatalf("legit post: %d", res.StatusCode)
	}
	// The consent step cannot be reached without passing login first.
	f2 := url.Values{"req": {f.Get("req")}, "csrf": {f.Get("csrf")}, "action": {"approve"}}
	if res := post(t, c, e, f2); res.StatusCode != 400 {
		t.Fatalf("skipping steps: %d", res.StatusCode)
	}
}

func TestLoginFailureAndExpiry(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	c := e.browser()
	res, _ := c.Get(e.authorizeURL(cid, nil))
	f := fields(t, read(res))
	f.Set("action", "login")
	f.Set("email", "will@example.com")
	f.Set("password", "not-the-password")
	res = post(t, c, e, f)
	body := read(res)
	if res.StatusCode != 401 || !strings.Contains(body, "Invalid email or password") {
		t.Fatalf("bad password: %d", res.StatusCode)
	}
	f2 := fields(t, body) // fresh csrf each time
	f2.Set("action", "login")
	f2.Set("email", "will@example.com")
	f2.Set("password", testPW)
	e.now = e.now.Add(11 * time.Minute)
	if res := post(t, c, e, f2); res.StatusCode != 400 {
		t.Fatalf("expired flow: %d", res.StatusCode)
	}
}

func (e *env) toMFA(c *http.Client, cid string) (url.Values, string) {
	e.t.Helper()
	res, _ := c.Get(e.authorizeURL(cid, nil))
	f := fields(e.t, read(res))
	f.Set("action", "login")
	f.Set("email", "will@example.com")
	f.Set("password", testPW)
	body := read(post(e.t, c, e, f))
	if !strings.Contains(body, `name="code"`) {
		e.t.Fatalf("expected the 2FA step: %.300s", body)
	}
	return fields(e.t, body), body
}

func TestTwoFactorStep(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	c := e.browser()
	f, _ := e.toMFA(c, cid)
	f.Set("action", "mfa")
	f.Set("code", "000000")
	res := post(t, c, e, f)
	body := read(res)
	if res.StatusCode != 401 {
		t.Fatalf("wrong code: %d", res.StatusCode)
	}
	f = fields(t, body)
	f.Set("action", "mfa")
	f.Set("code", e.code())
	res = post(t, c, e, f)
	body = read(res)
	if res.StatusCode != 200 || !strings.Contains(body, "Allow") {
		t.Fatalf("expected consent: %d %.300s", res.StatusCode, body)
	}
	f = fields(t, body)
	f.Set("action", "approve")
	if res = post(t, c, e, f); res.StatusCode != 302 || !strings.Contains(res.Header.Get("Location"), "code=") {
		t.Fatalf("approve: %d", res.StatusCode)
	}
}

func TestTwoFactorAttemptCap(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	c := e.browser()
	f, body := e.toMFA(c, cid)
	last := 0
	for range 6 {
		f = fields(t, body)
		f.Set("action", "mfa")
		f.Set("code", "000000")
		res := post(t, c, e, f)
		last = res.StatusCode
		body = read(res)
		if last == 400 || last == 429 {
			break
		}
	}
	if last != 429 && last != 400 {
		t.Fatalf("flow still open after repeated bad codes: %d", last)
	}
}

func TestDeny(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	c := e.browser()
	res, _ := c.Get(e.authorizeURL(cid, nil))
	f := fields(t, read(res))
	f.Set("action", "login")
	f.Set("email", "will@example.com")
	f.Set("password", testPW)
	f = fields(t, read(post(t, c, e, f)))
	f.Set("action", "mfa")
	f.Set("code", e.code())
	f = fields(t, read(post(t, c, e, f)))
	f.Set("action", "deny")
	res = post(t, c, e, f)
	loc, _ := url.Parse(res.Header.Get("Location"))
	if loc.Query().Get("error") != "access_denied" || loc.Query().Get("code") != "" {
		t.Fatal(res.Header.Get("Location"))
	}
}

func TestTokenEndpointRules(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	other := e.register("https://claude.com/api/mcp/auth_callback")
	code := e.connect(cid, nil)

	if st, _, _ := e.form("/oauth/token", url.Values{"grant_type": {"password"}, "client_id": {cid}}); st != 400 {
		t.Error("password grant accepted")
	}
	if st, out := e.exchange(other, code, verifier); st != 400 || out["error"] != "invalid_grant" {
		t.Errorf("code used by another client: %d %v", st, out)
	}
	code = e.connect(cid, nil)
	st, out, _ := e.form("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {cid}, "code": {code}, "redirect_uri": {"https://claude.com/api/mcp/auth_callback"}, "code_verifier": {verifier}})
	if st != 400 || out["error"] != "invalid_grant" {
		t.Errorf("redirect_uri mismatch: %d %v", st, out)
	}
	code = e.connect(cid, nil)
	if st, out, _ := e.form("/oauth/token", url.Values{"grant_type": {"authorization_code"}, "client_id": {cid}, "code": {code}, "redirect_uri": {callback}, "code_verifier": {verifier}, "resource": {"https://x.example/mcp"}}); st != 400 || out["error"] != "invalid_target" {
		t.Errorf("resource mismatch: %d %v", st, out)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+"/oauth/token", strings.NewReader(`{"grant_type":"refresh_token"}`))
	req.Header.Set("Content-Type", "application/json")
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 400 {
		t.Errorf("JSON body: %d", res.StatusCode)
	}
	req, _ = http.NewRequest("POST", e.srv.URL+"/oauth/token", strings.NewReader("grant_type=refresh_token&client_id="+cid))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(cid, "secret")
	if res, _ := http.DefaultClient.Do(req); res.StatusCode != 401 {
		t.Errorf("client authentication: %d", res.StatusCode)
	}
	if st, _, h := e.form("/oauth/token", url.Values{"grant_type": {"refresh_token"}, "client_id": {cid}, "refresh_token": {"nope"}}); st != 400 || h.Get("Cache-Control") != "no-store" {
		t.Errorf("unknown refresh token: %d %v", st, h)
	}
}

func TestRefreshCannotBeUsedByAnotherClient(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	other := e.register("https://claude.com/api/mcp/auth_callback")
	_, tok := e.exchange(cid, e.connect(cid, nil), verifier)
	if st, _ := e.refresh(other, tok["refresh_token"].(string)); st != 400 {
		t.Fatal("refresh token accepted for the wrong client")
	}
}

func TestRevokeEndpointAndConnectedApps(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	_, tok := e.exchange(cid, e.connect(cid, nil), verifier)
	apps, err := e.o.ListGrants(context.Background(), e.user.ID)
	if err != nil || len(apps) != 1 || apps[0].Name != "Claude" || strings.Join(apps[0].Scopes, " ") != "tasks:read tasks:write" {
		t.Fatalf("apps: %v %+v", err, apps)
	}

	// Revocation by a different client changes nothing (and still answers 200).
	other := e.register("https://claude.com/api/mcp/auth_callback")
	if st, _, _ := e.form("/oauth/revoke", url.Values{"token": {tok["refresh_token"].(string)}, "client_id": {other}}); st != 200 {
		t.Fatal(st)
	}
	if _, err := e.o.VerifyAccessToken(context.Background(), tok["access_token"].(string)); err != nil {
		t.Fatal("another client revoked this grant")
	}
	if st, _, _ := e.form("/oauth/revoke", url.Values{"token": {"garbage"}, "client_id": {cid}}); st != 200 {
		t.Fatal("unknown token must still be 200")
	}
	if st, _, _ := e.form("/oauth/revoke", url.Values{"token": {tok["refresh_token"].(string)}, "client_id": {cid}}); st != 200 {
		t.Fatal(st)
	}
	if _, err := e.o.VerifyAccessToken(context.Background(), tok["access_token"].(string)); err == nil {
		t.Fatal("access token survived revocation")
	}

	// Revocation from the Connected apps page.
	_, tok = e.exchange(cid, e.connect(cid, nil), verifier)
	apps, _ = e.o.ListGrants(context.Background(), e.user.ID)
	if len(apps) != 1 {
		t.Fatalf("apps: %+v", apps)
	}
	if err := e.o.RevokeGrant(context.Background(), "someone-else", apps[0].ID); err == nil {
		t.Fatal("revoked another user's grant")
	}
	if err := e.o.RevokeGrant(context.Background(), e.user.ID, apps[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.o.VerifyAccessToken(context.Background(), tok["access_token"].(string)); err == nil {
		t.Fatal("access token survived revoke from settings")
	}
	if st, _ := e.refresh(cid, tok["refresh_token"].(string)); st != 400 {
		t.Fatal("refresh token survived revoke from settings")
	}
}

func TestDisabledAccountTokensStopWorking(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	_, tok := e.exchange(cid, e.connect(cid, nil), verifier)
	if _, err := e.db.W.Exec(`UPDATE users SET disabled_at = ? WHERE id = ?`, store.FormatTime(e.now), e.user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.o.VerifyAccessToken(context.Background(), tok["access_token"].(string)); err == nil {
		t.Fatal("disabled account's token still valid")
	}
}

func TestPATsAreNotAccessTokens(t *testing.T) {
	e := newEnv(t)
	pat, _ := e.svc.CreateToken(context.Background(), e.user, service.NewToken{Name: "x", Scopes: []string{"tasks:read"}})
	if _, err := e.o.VerifyAccessToken(context.Background(), pat.Token); err == nil {
		t.Fatal("a PAT was accepted as an OAuth access token")
	}
}

func TestHousekeeping(t *testing.T) {
	e := newEnv(t)
	cid := e.register(callback)
	e.connect(cid, nil) // leaves a used code behind
	e.now = e.now.Add(40 * 24 * time.Hour)
	if err := e.svc.Housekeeping(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	for _, tbl := range []string{"oauth_codes", "oauth_requests", "oauth_tokens"} {
		if err := e.db.R.QueryRow(`SELECT count(*) FROM ` + tbl).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: %d rows left (%v)", tbl, n, err)
		}
	}
}
