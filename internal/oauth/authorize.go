package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/wbollock/blair/internal/service"
	"github.com/wbollock/blair/internal/store"
)

const pageStyle = `
:root{color-scheme:light dark;--bg:#f6f7f8;--card:#fff;--fg:#1b1f23;--mute:#5d6670;--line:#d8dde2;--accent:#1f7a4f;--accent-fg:#fff;--bad:#b3261e}
@media (prefers-color-scheme:dark){:root{--bg:#14171a;--card:#1d2125;--fg:#e8ebee;--mute:#9aa4ae;--line:#32383e;--accent:#3fb37f;--accent-fg:#06180f;--bad:#f2837c}}
*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:16px;background:var(--bg);color:var(--fg);font:16px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}
main{width:100%;max-width:26rem;background:var(--card);border:1px solid var(--line);border-radius:12px;padding:24px}
h1{font-size:1.25rem;margin:0 0 4px}p{margin:8px 0}.mute{color:var(--mute);font-size:.9rem}
label{display:block;margin:14px 0 4px;font-weight:600;font-size:.9rem}
input[type=text],input[type=email],input[type=password]{width:100%;padding:10px 12px;font:inherit;color:inherit;background:var(--bg);border:1px solid var(--line);border-radius:8px}
input:focus-visible,button:focus-visible{outline:3px solid var(--accent);outline-offset:2px}
.row{display:flex;gap:8px;margin-top:20px}.row button{flex:1}
button{padding:10px 14px;font:inherit;font-weight:600;border-radius:8px;border:1px solid var(--line);background:transparent;color:inherit;cursor:pointer}
button.primary{background:var(--accent);color:var(--accent-fg);border-color:var(--accent)}
.err{color:var(--bad);font-weight:600}ul{padding-left:1.1rem;margin:8px 0}li{margin:6px 0}code{font-size:.85rem;word-break:break-all}
`

var pageCSP = func() string {
	sum := sha256.Sum256([]byte(pageStyle))
	// Chrome applies form-action to the redirect after a form POST, so the callbacks are listed.
	return "default-src 'none'; style-src 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) +
		"'; form-action 'self' https://claude.ai https://claude.com; frame-ancestors 'none'; base-uri 'none'"
}()

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="robots" content="noindex"><title>{{.Title}} · Blair</title><style>` + pageStyle + `</style></head>
<body><main>
{{- if eq .Stage "error"}}
<h1>{{.Title}}</h1><p>{{.Message}}</p>
{{- else}}
<h1>{{.Title}}</h1>
{{- if .Error}}<p class="err" role="alert">{{.Error}}</p>{{end}}
<form method="post" action="/oauth/authorize" autocomplete="on">
<input type="hidden" name="req" value="{{.Req}}"><input type="hidden" name="csrf" value="{{.CSRF}}">
{{- if eq .Stage "login"}}
<p class="mute"><strong>{{.ClientName}}</strong> wants to connect to your Blair tasks. Sign in to continue.</p>
<label for="email">Email</label><input id="email" name="email" type="email" autocomplete="username" required autofocus>
<label for="password">Password</label><input id="password" name="password" type="password" autocomplete="current-password" required>
<div class="row"><button class="primary" name="action" value="login">Sign in</button></div>
{{- else if eq .Stage "mfa"}}
<p class="mute">Enter the 6-digit code from your authenticator app, or a recovery code.</p>
<label for="code">Code</label><input id="code" name="code" type="text" inputmode="numeric" autocomplete="one-time-code" autofocus>
<label for="recovery">Recovery code <span class="mute">(instead of the code above)</span></label><input id="recovery" name="recovery_code" type="text" autocomplete="off">
<div class="row"><button class="primary" name="action" value="mfa">Verify</button></div>
{{- else}}
<p><strong>{{.ClientName}}</strong> would like to use your Blair account <strong>{{.Email}}</strong>.</p>
<p class="mute">This name was supplied by the app when it registered; Blair has not verified it. After you approve, it returns to <code>{{.RedirectHost}}</code>.</p>
<ul>{{range .Grants}}<li>{{.}}</li>{{end}}</ul>
<p class="mute">It cannot delete anything, see your notes, or change your account. You can disconnect it any time under Settings → Connected apps.</p>
<div class="row"><button name="action" value="deny">Cancel</button><button class="primary" name="action" value="approve">Allow</button></div>
{{- end}}
</form>
{{- end}}
</main></body></html>`))

type pageData struct {
	Stage, Title, Message, Error, Req, CSRF, ClientName, Email, RedirectHost string
	Grants                                                                   []string
}

var scopeText = map[string]string{
	"tasks:read":  "See your tasks, projects and daily plan",
	"tasks:write": "Create tasks, edit them, complete them and move them between projects and columns",
}

func (s *Server) page(w http.ResponseWriter, status int, d pageData) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", pageCSP)
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	w.WriteHeader(status)
	_ = pageTmpl.Execute(w, d)
}

func (s *Server) errorPage(w http.ResponseWriter, status int, title, msg string) {
	s.page(w, status, pageData{Stage: "error", Title: title, Message: msg})
}

var challengeRe = regexp.MustCompile(`^[A-Za-z0-9\-._~]{43,128}$`)

type flow struct {
	ID, ClientID, RedirectURI, Scope, State, Challenge, Resource, Stage string
	UserID                                                              sql.NullString
	Attempts                                                            int
	Binding, CSRF                                                       []byte
	ExpiresAt                                                           string
}

func (s *Server) cookieName() string {
	if s.cfg.Secure {
		return "__Host-blair_oauth"
	}
	return "blair_oauth"
}

func (s *Server) redirectError(w http.ResponseWriter, r *http.Request, redirect, state, code, desc string) {
	u, err := url.Parse(redirect)
	if err != nil {
		s.errorPage(w, http.StatusBadRequest, "Cannot continue", "The request was not valid.")
		return
	}
	q := u.Query()
	q.Set("error", code)
	q.Set("error_description", desc)
	if state != "" {
		q.Set("state", state)
	}
	q.Set("iss", s.Issuer())
	u.RawQuery = q.Encode()
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// duplicated reports whether any of the keys appears more than once.
func duplicated(q url.Values, keys ...string) bool {
	for _, k := range keys {
		if len(q[k]) > 1 {
			return true
		}
	}
	return false
}

func (s *Server) authorizeGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if ok, _ := s.tokenIP.Allow("authorize:" + s.cfg.ClientIP(r)); !ok {
		s.errorPage(w, http.StatusTooManyRequests, "Too many requests", "Wait a minute and try again from the app.")
		return
	}
	q := r.URL.Query()
	if duplicated(q, "client_id", "redirect_uri", "response_type", "code_challenge", "code_challenge_method", "scope", "resource", "state") {
		s.errorPage(w, http.StatusBadRequest, "Cannot continue", "The request repeats a parameter.")
		return
	}
	c, err := s.client(ctx, q.Get("client_id"))
	if err != nil {
		s.errorPage(w, http.StatusBadRequest, "Unknown app", "This app is not registered. Remove the connector and add it again.")
		return
	}
	redirect := q.Get("redirect_uri")
	if !slices.Contains(c.RedirectURIs, redirect) || !s.allowedRedirect(redirect) {
		s.errorPage(w, http.StatusBadRequest, "Cannot continue", "The callback address does not match the app's registration.")
		return
	}
	state := q.Get("state")
	if len(state) > 512 {
		s.errorPage(w, http.StatusBadRequest, "Cannot continue", "The state value is too long.")
		return
	}
	if q.Get("response_type") != "code" {
		s.redirectError(w, r, redirect, state, "unsupported_response_type", "only response_type=code is supported")
		return
	}
	if q.Get("code_challenge_method") != "S256" || !challengeRe.MatchString(q.Get("code_challenge")) {
		s.redirectError(w, r, redirect, state, "invalid_request", "PKCE with code_challenge_method=S256 is required")
		return
	}
	scope, ok := parseScope(q.Get("scope"))
	if !ok {
		s.redirectError(w, r, redirect, state, "invalid_scope", "only "+strings.Join(Scopes, " ")+" can be granted")
		return
	}
	resource := q.Get("resource")
	if resource == "" {
		resource = s.Resource()
	}
	if resource != s.Resource() {
		s.redirectError(w, r, redirect, state, "invalid_target", "resource must be "+s.Resource())
		return
	}

	binding, csrf, id := random(43), random(43), random(32)
	if _, err := s.db.W.ExecContext(ctx, `INSERT INTO oauth_requests (id, client_id, redirect_uri, scope, state, code_challenge, resource, stage, binding_hash, csrf_hash, created_at, expires_at)
		VALUES (?,?,?,?,?,?,?,'login',?,?,?,?)`, id, c.ID, redirect, scope, state, q.Get("code_challenge"), resource,
		hash(binding), hash(csrf), s.nowStr(), store.FormatTime(s.now().Add(s.cfg.RequestTTL))); err != nil {
		s.errorPage(w, http.StatusInternalServerError, "Something went wrong", "Try again from the app.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: binding, Path: "/", MaxAge: int(s.cfg.RequestTTL.Seconds()),
		HttpOnly: true, Secure: s.cfg.Secure, SameSite: http.SameSiteLaxMode})
	s.page(w, http.StatusOK, pageData{Stage: "login", Title: "Connect to Blair", Req: id, CSRF: csrf, ClientName: c.Name})
}

func (s *Server) loadFlow(r *http.Request, id string) (*flow, bool) {
	var f flow
	err := s.db.W.QueryRowContext(r.Context(), `SELECT id, client_id, redirect_uri, scope, state, code_challenge, resource, stage, user_id, attempts, binding_hash, csrf_hash, expires_at
		FROM oauth_requests WHERE id = ?`, id).Scan(&f.ID, &f.ClientID, &f.RedirectURI, &f.Scope, &f.State, &f.Challenge, &f.Resource, &f.Stage, &f.UserID, &f.Attempts, &f.Binding, &f.CSRF, &f.ExpiresAt)
	if err != nil || s.nowStr() >= f.ExpiresAt {
		return nil, false
	}
	// The flow belongs to the browser that started it, and the form must carry its CSRF token.
	c, err := r.Cookie(s.cookieName())
	if err != nil || subtle.ConstantTimeCompare(hash(c.Value), f.Binding) != 1 ||
		subtle.ConstantTimeCompare(hash(r.PostFormValue("csrf")), f.CSRF) != 1 {
		return nil, false
	}
	return &f, true
}

func (s *Server) authorizePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		s.errorPage(w, http.StatusBadRequest, "Cannot continue", "The form was not valid.")
		return
	}
	f, ok := s.loadFlow(r, r.PostFormValue("req"))
	if !ok {
		s.errorPage(w, http.StatusBadRequest, "This sign-in expired", "Start again from the app that sent you here.")
		return
	}
	c, err := s.client(r.Context(), f.ClientID)
	if err != nil {
		s.errorPage(w, http.StatusBadRequest, "Unknown app", "This app is no longer registered.")
		return
	}
	action := r.PostFormValue("action")
	switch {
	case f.Stage == "login" && action == "login":
		s.stepLogin(w, r, f, c)
	case f.Stage == "mfa" && action == "mfa":
		s.stepMFA(w, r, f, c)
	case f.Stage == "consent" && (action == "approve" || action == "deny"):
		s.stepConsent(w, r, f, c, action == "approve")
	default:
		s.errorPage(w, http.StatusBadRequest, "Cannot continue", "That step is not available. Start again from the app.")
	}
}

// advance moves the flow to a new stage and issues a fresh CSRF token for the next form.
func (s *Server) advance(r *http.Request, f *flow, stage, userID string) (string, error) {
	csrf := random(43)
	_, err := s.db.W.ExecContext(r.Context(), `UPDATE oauth_requests SET stage = ?, user_id = ?, attempts = 0, csrf_hash = ? WHERE id = ?`,
		stage, nullable(userID), hash(csrf), f.ID)
	return csrf, err
}

// rotateCSRF issues a fresh token for re-rendering the same step; attempts are kept.
func (s *Server) rotateCSRF(r *http.Request, f *flow) (string, error) {
	csrf := random(43)
	_, err := s.db.W.ExecContext(r.Context(), `UPDATE oauth_requests SET csrf_hash = ? WHERE id = ?`, hash(csrf), f.ID)
	return csrf, err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Server) redo(w http.ResponseWriter, r *http.Request, f *flow, c *client, stage, msg string, status int) {
	csrf, err := s.rotateCSRF(r, f)
	if err != nil {
		s.errorPage(w, http.StatusInternalServerError, "Something went wrong", "Try again from the app.")
		return
	}
	title := "Connect to Blair"
	if stage == "mfa" {
		title = "Two-factor code"
	}
	s.page(w, status, pageData{Stage: stage, Title: title, Error: msg, Req: f.ID, CSRF: csrf, ClientName: c.Name})
}

func (s *Server) stepLogin(w http.ResponseWriter, r *http.Request, f *flow, c *client) {
	email := strings.ToLower(strings.TrimSpace(r.PostFormValue("email")))
	if ok, _ := s.loginIP.Allow(s.cfg.ClientIP(r)); !ok {
		s.redo(w, r, f, c, "login", "Too many attempts. Wait a minute and try again.", http.StatusTooManyRequests)
		return
	}
	if ok, _ := s.loginAcct.Allow(email); !ok {
		s.redo(w, r, f, c, "login", "Too many attempts for this account. Try again later.", http.StatusTooManyRequests)
		return
	}
	u, err := s.svc.Authenticate(r.Context(), email, r.PostFormValue("password"))
	if err != nil {
		if errors.Is(err, service.ErrUnauthorized) {
			s.audit(r, "", "oauth.login_failure", map[string]any{"client_id": c.ID})
			s.redo(w, r, f, c, "login", "Invalid email or password.", http.StatusUnauthorized)
			return
		}
		s.errorPage(w, http.StatusInternalServerError, "Something went wrong", "Try again from the app.")
		return
	}
	switch {
	case u.ID != c.OwnerID:
		// Only the account that opened registration may approve this app.
		s.audit(r, u.ID, "oauth.owner_mismatch", map[string]any{"client_id": c.ID})
		s.redo(w, r, f, c, "login", "This connection was started from a different account.", http.StatusForbidden)
		return
	case u.MustChangePassword:
		s.redo(w, r, f, c, "login", "Sign in to Blair and change your password first.", http.StatusForbidden)
		return
	case !u.TOTPEnabled:
		// Connected apps keep working for 30 days, so a password alone is not enough to create one.
		s.redo(w, r, f, c, "login", "Turn on two-factor authentication in Blair before connecting apps.", http.StatusForbidden)
		return
	}
	csrf, err := s.advance(r, f, "mfa", u.ID)
	if err != nil {
		s.errorPage(w, http.StatusInternalServerError, "Something went wrong", "Try again from the app.")
		return
	}
	s.render(w, r, f, c, "mfa", csrf, u.ID, "")
}

// render shows the form for stage (mfa or consent).
func (s *Server) render(w http.ResponseWriter, r *http.Request, f *flow, c *client, stage, csrf, userID, errMsg string) {
	d := pageData{Stage: stage, Title: "Two-factor code", Error: errMsg, Req: f.ID, CSRF: csrf, ClientName: c.Name}
	if stage == "consent" {
		u, err := s.svc.GetUser(r.Context(), userID)
		if err != nil {
			s.errorPage(w, http.StatusInternalServerError, "Something went wrong", "Try again from the app.")
			return
		}
		d.Title, d.Email = "Allow access?", u.Email
		if ru, err := url.Parse(f.RedirectURI); err == nil {
			d.RedirectHost = ru.Host
		}
		for _, sc := range strings.Fields(f.Scope) {
			d.Grants = append(d.Grants, scopeText[sc])
		}
	}
	s.page(w, http.StatusOK, d)
}

const maxMFAAttempts = 5

func (s *Server) stepMFA(w http.ResponseWriter, r *http.Request, f *flow, c *client) {
	uid := f.UserID.String
	if f.Attempts >= maxMFAAttempts {
		_, _ = s.db.W.ExecContext(r.Context(), `DELETE FROM oauth_requests WHERE id = ?`, f.ID)
		s.errorPage(w, http.StatusTooManyRequests, "Too many attempts", "Start again from the app.")
		return
	}
	_, _ = s.db.W.ExecContext(r.Context(), `UPDATE oauth_requests SET attempts = attempts + 1 WHERE id = ?`, f.ID)
	if ok, _ := s.mfaIP.Allow(s.cfg.ClientIP(r)); !ok {
		s.redo(w, r, f, c, "mfa", "Too many attempts. Wait a minute and try again.", http.StatusTooManyRequests)
		return
	}
	if ok, _ := s.mfaAcct.Allow(uid); !ok {
		s.redo(w, r, f, c, "mfa", "Too many attempts for this account. Try again later.", http.StatusTooManyRequests)
		return
	}
	code, recovery := strings.TrimSpace(r.PostFormValue("code")), strings.TrimSpace(r.PostFormValue("recovery_code"))
	if err := s.svc.VerifySecondFactor(r.Context(), uid, code, recovery); err != nil {
		s.audit(r, uid, "oauth.mfa_failure", map[string]any{"client_id": c.ID})
		s.redo(w, r, f, c, "mfa", "That code didn't work.", http.StatusUnauthorized)
		return
	}
	csrf, err := s.advance(r, f, "consent", uid)
	if err != nil {
		s.errorPage(w, http.StatusInternalServerError, "Something went wrong", "Try again from the app.")
		return
	}
	s.render(w, r, f, c, "consent", csrf, uid, "")
}

func (s *Server) stepConsent(w http.ResponseWriter, r *http.Request, f *flow, c *client, approve bool) {
	ctx := r.Context()
	uid := f.UserID.String
	_, _ = s.db.W.ExecContext(ctx, `DELETE FROM oauth_requests WHERE id = ?`, f.ID)
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.cfg.Secure, SameSite: http.SameSiteLaxMode})
	if !approve {
		s.audit(r, uid, "oauth.authorize_denied", map[string]any{"client_id": c.ID})
		s.redirectError(w, r, f.RedirectURI, f.State, "access_denied", "the user declined")
		return
	}
	if u, err := s.svc.GetUser(ctx, uid); err != nil || u.Disabled {
		s.errorPage(w, http.StatusForbidden, "Cannot continue", "This account cannot connect apps.")
		return
	}
	code := prefixCode + random(43)
	if _, err := s.db.W.ExecContext(ctx, `INSERT INTO oauth_codes (code_hash, client_id, user_id, redirect_uri, scope, resource, code_challenge, expires_at) VALUES (?,?,?,?,?,?,?,?)`,
		hash(code), c.ID, uid, f.RedirectURI, f.Scope, f.Resource, f.Challenge, store.FormatTime(s.now().Add(s.cfg.CodeTTL))); err != nil {
		s.errorPage(w, http.StatusInternalServerError, "Something went wrong", "Try again from the app.")
		return
	}
	s.audit(r, uid, "oauth.authorize", map[string]any{"client_id": c.ID, "scope": f.Scope})
	u, _ := url.Parse(f.RedirectURI)
	q := u.Query()
	q.Set("code", code)
	if f.State != "" {
		q.Set("state", f.State)
	}
	q.Set("iss", s.Issuer())
	u.RawQuery = q.Encode()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, u.String(), http.StatusFound)
}
