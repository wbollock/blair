// Package oauth is Blair's OAuth 2.1 authorization server for the MCP connector (ADR-0016).
//
// It serves RFC 8414 and RFC 9728 metadata, RFC 7591 dynamic client registration for
// public clients, an authorization-code flow with PKCE (S256 only) behind Blair's own
// login and 2FA, rotating refresh tokens, and RFC 7009 revocation. Tokens are bound to the
// /mcp resource and can only carry tasks:read and tasks:write.
package oauth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"

	"github.com/wbollock/blair/internal/auth"
	"github.com/wbollock/blair/internal/ratelimit"
	"github.com/wbollock/blair/internal/service"
	"github.com/wbollock/blair/internal/store"
	"github.com/wbollock/blair/internal/telemetry"
)

// Scopes an OAuth client can be granted. tasks:delete is never grantable (ADR-0015).
var Scopes = []string{"tasks:read", "tasks:write"}

// DefaultRedirectURIs are the only callbacks dynamic registration accepts.
var DefaultRedirectURIs = []string{
	"https://claude.ai/api/mcp/auth_callback",
	"https://claude.com/api/mcp/auth_callback",
}

// ResourcePath is the one resource tokens are valid for.
const ResourcePath = "/mcp"

const (
	tokenAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	prefixAccess  = "blair_oat_"
	prefixRefresh = "blair_ort_"
	prefixCode    = "blair_oac_"
	maxGrants     = 10 // live connections per account; the oldest is revoked past this
	windowTTL     = 10 * time.Minute
	windowUses    = 3
)

// Config is what the server needs from the app config.
type Config struct {
	BaseURL      *url.URL // public https origin, also the issuer
	Secure       bool     // use __Host- cookies
	RedirectURIs []string // allowed callbacks; DefaultRedirectURIs when empty
	ClientIP     func(*http.Request) string
	Now          func() time.Time
	AccessTTL    time.Duration
	RefreshTTL   time.Duration
	CodeTTL      time.Duration
	RequestTTL   time.Duration
}

// Server is the authorization server.
type Server struct {
	cfg  Config
	db   *store.DB
	svc  *service.Service
	base string

	regIP, loginIP, loginAcct, mfaIP, mfaAcct, tokenIP, revokeIP *ratelimit.Limiter
}

// New builds a Server. Zero durations get their defaults (60 s codes, 1 h access, 30 d refresh).
func New(db *store.DB, svc *service.Service, cfg Config) *Server {
	if len(cfg.RedirectURIs) == 0 {
		cfg.RedirectURIs = DefaultRedirectURIs
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ClientIP == nil {
		cfg.ClientIP = func(r *http.Request) string { return r.RemoteAddr }
	}
	for _, d := range []struct {
		p *time.Duration
		v time.Duration
	}{{&cfg.CodeTTL, time.Minute}, {&cfg.AccessTTL, time.Hour}, {&cfg.RefreshTTL, 30 * 24 * time.Hour}, {&cfg.RequestTTL, 10 * time.Minute}} {
		if *d.p == 0 {
			*d.p = d.v
		}
	}
	return &Server{
		cfg: cfg, db: db, svc: svc,
		base:      strings.TrimRight(cfg.BaseURL.String(), "/"),
		regIP:     ratelimit.New(time.Hour, 10),
		loginIP:   ratelimit.New(time.Minute, 5),
		loginAcct: ratelimit.New(time.Hour, 10),
		mfaIP:     ratelimit.New(time.Minute, 5),
		mfaAcct:   ratelimit.New(time.Hour, 10),
		tokenIP:   ratelimit.New(time.Minute, 60),
		revokeIP:  ratelimit.New(time.Minute, 30),
	}
}

// Issuer is the authorization server identifier.
func (s *Server) Issuer() string { return s.base }

// Resource is the canonical URI of the MCP endpoint that tokens are bound to.
func (s *Server) Resource() string { return s.base + ResourcePath }

// MetadataURL is the RFC 9728 metadata document for the MCP endpoint.
func (s *Server) MetadataURL() string {
	return s.base + "/.well-known/oauth-protected-resource" + ResourcePath
}

// Routes mounts the metadata and /oauth endpoints.
func (s *Server) Routes(r chi.Router) {
	r.Get("/.well-known/oauth-protected-resource", s.protectedResource)
	r.Get("/.well-known/oauth-protected-resource"+ResourcePath, s.protectedResource)
	r.Get("/.well-known/oauth-authorization-server", s.authServerMetadata)
	r.Options("/.well-known/*", s.preflight)
	r.Post("/oauth/register", s.register)
	r.Options("/oauth/register", s.preflight)
	r.Get("/oauth/authorize", s.authorizeGet)
	r.With(http.NewCrossOriginProtection().Handler).Post("/oauth/authorize", s.authorizePost)
	r.Post("/oauth/token", s.token)
	r.Options("/oauth/token", s.preflight)
	r.Post("/oauth/revoke", s.revoke)
	r.Options("/oauth/revoke", s.preflight)
}

// ---- helpers ----

func itoa(n int) string { return strconv.Itoa(n) }

func (s *Server) now() time.Time { return s.cfg.Now() }
func (s *Server) nowStr() string { return store.FormatTime(s.now()) }

func random(n int) string { return auth.RandomString(n, tokenAlphabet) }

func hash(v string) []byte {
	sum := sha256.Sum256([]byte(v))
	return sum[:]
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// parseScope validates a space-separated scope list against Scopes and returns it in
// canonical order. An empty list means everything that can be granted.
func parseScope(raw string) (string, bool) {
	want := strings.Fields(raw)
	if len(want) == 0 {
		return strings.Join(Scopes, " "), true
	}
	for _, w := range want {
		if !slices.Contains(Scopes, w) {
			return "", false
		}
	}
	var out []string
	for _, sc := range Scopes {
		if slices.Contains(want, sc) {
			out = append(out, sc)
		}
	}
	return strings.Join(out, " "), true
}

func subsetScope(sub, of string) bool {
	have := strings.Fields(of)
	for _, s := range strings.Fields(sub) {
		if !slices.Contains(have, s) {
			return false
		}
	}
	return true
}

// suspicious events are logged at WARN with the client address, so they can be alerted on.
var suspicious = map[string]bool{
	"oauth.login_failure": true, "oauth.mfa_failure": true, "oauth.refresh_reuse": true, "oauth.code_reuse": true,
	"oauth.owner_mismatch": true, "oauth.registration_denied": true,
}

// audit records a security event three ways: the user's audit log, a structured log line and a
// counter. None of them carries a token, code, password, or task text.
func (s *Server) audit(r *http.Request, userID, kind string, detail map[string]any) {
	ip := s.cfg.ClientIP(r)
	s.svc.Audit(r.Context(), userID, kind, service.ClientInfo{IP: ip, UserAgent: r.UserAgent()}, detail)
	attrs := []any{"event", kind}
	if id, ok := detail["client_id"].(string); ok {
		attrs = append(attrs, "client_id", id)
	}
	if id, ok := detail["grant_id"].(string); ok {
		attrs = append(attrs, "grant_id", id)
	}
	if userID != "" {
		attrs = append(attrs, "user_id", userID)
	}
	if suspicious[kind] {
		slog.WarnContext(r.Context(), "oauth security event", append(attrs, "ip", ip)...)
	} else {
		slog.InfoContext(r.Context(), "oauth event", attrs...)
	}
	telemetry.Count(r.Context(), "blair.oauth.events", attribute.String("event", strings.TrimPrefix(kind, "oauth.")))
}

func (s *Server) allowedRedirect(uri string) bool { return slices.Contains(s.cfg.RedirectURIs, uri) }

// corsOrigins are the extra origins (derived from dev-only redirect URIs) allowed to call
// the registration, token and revocation endpoints from a browser.
func (s *Server) corsOrigin(r *http.Request) string {
	o := r.Header.Get("Origin")
	if o == "" {
		return ""
	}
	for _, ru := range s.cfg.RedirectURIs {
		if slices.Contains(DefaultRedirectURIs, ru) {
			continue
		}
		if u, err := url.Parse(ru); err == nil && u.Scheme+"://"+u.Host == o {
			return o
		}
	}
	return ""
}

func (s *Server) cors(w http.ResponseWriter, r *http.Request) {
	if o := s.corsOrigin(r); o != "" {
		w.Header().Set("Access-Control-Allow-Origin", o)
		w.Header().Add("Vary", "Origin")
	}
}

func (s *Server) preflight(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/.well-known/") {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else if o := s.corsOrigin(r); o != "" {
		w.Header().Set("Access-Control-Allow-Origin", o)
		w.Header().Add("Vary", "Origin")
	} else {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, MCP-Protocol-Version")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
}

// ---- stored shapes ----

type client struct {
	ID           string
	OwnerID      string
	Name         string
	RedirectURIs []string
}

func (s *Server) client(ctx context.Context, id string) (*client, error) {
	var c client
	var uris string
	err := s.db.R.QueryRowContext(ctx, `SELECT id, owner_id, name, redirect_uris FROM oauth_clients WHERE id = ?`, id).Scan(&c.ID, &c.OwnerID, &c.Name, &uris)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	c.RedirectURIs = strings.Fields(uris)
	return &c, nil
}

// revokeGrant marks a grant revoked and drops its access tokens. Refresh tokens stay so
// a later replay still maps to the grant.
func (s *Server) revokeGrant(ctx context.Context, grantID, reason string) error {
	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_grants SET revoked_at = ?, revoked_reason = ? WHERE id = ? AND revoked_at IS NULL`, s.nowStr(), reason, grantID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE grant_id = ? AND kind = 'access'`, grantID); err != nil {
		return err
	}
	return tx.Commit()
}

func newID() string { return uuid.Must(uuid.NewV7()).String() }

// OpenRegistration lets one app register itself for the next few minutes. Registration is
// otherwise refused, so a stranger cannot create a client that the owner might later approve.
func (s *Server) OpenRegistration(ctx context.Context, userID string) (time.Time, error) {
	exp := s.now().Add(windowTTL)
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO oauth_windows (user_id, expires_at, remaining) VALUES (?,?,?)
		ON CONFLICT(user_id) DO UPDATE SET expires_at = excluded.expires_at, remaining = excluded.remaining`, userID, store.FormatTime(exp), windowUses)
	return exp, err
}

// takeWindow consumes one registration from an open window and returns whose it was.
func (s *Server) takeWindow(ctx context.Context) (string, bool) {
	var uid string
	err := s.db.W.QueryRowContext(ctx, `UPDATE oauth_windows SET remaining = remaining - 1
		WHERE user_id = (SELECT user_id FROM oauth_windows WHERE remaining > 0 AND expires_at > ? ORDER BY expires_at LIMIT 1)
		RETURNING user_id`, s.nowStr()).Scan(&uid)
	return uid, err == nil
}

// trimGrants keeps an account to maxGrants live connections by revoking the oldest.
func (s *Server) trimGrants(ctx context.Context, userID string) {
	rows, err := s.db.W.QueryContext(ctx, `SELECT id FROM oauth_grants WHERE user_id = ? AND revoked_at IS NULL ORDER BY created_at DESC LIMIT -1 OFFSET ?`, userID, maxGrants)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_ = s.revokeGrant(ctx, id, "too_many_connections")
	}
}
