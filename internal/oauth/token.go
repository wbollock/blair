package oauth

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"mime"
	"net/http"
	"regexp"

	"github.com/wbollock/blair/internal/store"
)

var verifierRe = regexp.MustCompile(`^[A-Za-z0-9\-._~]{43,128}$`)

type grantError struct {
	status int
	code   string
	desc   string
}

func (e *grantError) Error() string { return e.code + ": " + e.desc }

func invalidGrant(desc string) *grantError {
	return &grantError{http.StatusBadRequest, "invalid_grant", desc}
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// token is the RFC 6749 token endpoint for public clients.
func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if ok, wait := s.tokenIP.Allow("token:" + s.cfg.ClientIP(r)); !ok {
		w.Header().Set("Retry-After", itoa(int(wait.Seconds())+1))
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "too many requests")
		return
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/x-www-form-urlencoded" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "send application/x-www-form-urlencoded")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "could not read the form")
		return
	}
	if r.Header.Get("Authorization") != "" {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_client"`)
		oauthError(w, http.StatusUnauthorized, "invalid_client", "this server only has public clients; send client_id in the body")
		return
	}
	c, err := s.client(r.Context(), r.PostFormValue("client_id"))
	if err != nil {
		oauthError(w, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		return
	}

	var resp *tokenResponse
	switch r.PostFormValue("grant_type") {
	case "authorization_code":
		resp, err = s.exchangeCode(r, c)
	case "refresh_token":
		resp, err = s.refresh(r, c)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "use authorization_code or refresh_token")
		return
	}
	if err != nil {
		var ge *grantError
		if errors.As(err, &ge) {
			oauthError(w, ge.status, ge.code, ge.desc)
			return
		}
		oauthError(w, http.StatusInternalServerError, "server_error", "could not issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) exchangeCode(r *http.Request, c *client) (*tokenResponse, error) {
	ctx := r.Context()
	code := r.PostFormValue("code")
	// One transaction on the single writer connection: consuming the code, spotting a replay,
	// reading the grant it created and issuing tokens cannot interleave with another exchange.
	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var userID, redirect, scope, resource, challenge, expires, clientID string
	var used, grantID sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT client_id, user_id, redirect_uri, scope, resource, code_challenge, expires_at, used_at, grant_id FROM oauth_codes WHERE code_hash = ?`,
		hash(code)).Scan(&clientID, &userID, &redirect, &scope, &resource, &challenge, &expires, &used, &grantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, invalidGrant("unknown or expired code")
	}
	if err != nil {
		return nil, err
	}
	if used.Valid {
		// A replay revokes what the first exchange issued.
		if grantID.Valid {
			if err := s.revokeGrantIn(ctx, tx, grantID.String, "code_reuse"); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		s.audit(r, userID, "oauth.code_reuse", map[string]any{"client_id": clientID})
		return nil, invalidGrant("the code was already used")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_codes SET used_at = ? WHERE code_hash = ?`, s.nowStr(), hash(code)); err != nil {
		return nil, err
	}
	// From here a failed check still burns the code, so the burn is committed.
	burned := func(e error) (*tokenResponse, error) {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, e
	}
	switch {
	case clientID != c.ID:
		return burned(invalidGrant("the code was issued to another client"))
	case s.nowStr() >= expires:
		return burned(invalidGrant("the code expired"))
	case r.PostFormValue("redirect_uri") != redirect:
		return burned(invalidGrant("redirect_uri does not match the authorization request"))
	}
	if rs := r.PostFormValue("resource"); rs != "" && rs != resource {
		return burned(&grantError{http.StatusBadRequest, "invalid_target", "resource does not match the authorization request"})
	}
	verifier := r.PostFormValue("code_verifier")
	if !verifierRe.MatchString(verifier) || subtle.ConstantTimeCompare([]byte(pkceChallenge(verifier)), []byte(challenge)) != 1 {
		return burned(invalidGrant("PKCE verification failed"))
	}
	if u, err := s.svc.GetUser(ctx, userID); err != nil || u.Disabled {
		return burned(invalidGrant("the account cannot connect apps"))
	}

	gid := newID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO oauth_grants (id, user_id, client_id, scope, resource, created_at) VALUES (?,?,?,?,?,?)`,
		gid, userID, c.ID, scope, resource, s.nowStr()); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_codes SET grant_id = ? WHERE code_hash = ?`, gid, hash(code)); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE oauth_clients SET approved_at = COALESCE(approved_at, ?) WHERE id = ?`, s.nowStr(), c.ID); err != nil {
		return nil, err
	}
	out, err := s.issue(ctx, tx, gid, scope)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.trimGrants(ctx, userID)
	s.audit(r, userID, "oauth.token_issued", map[string]any{"client_id": c.ID, "grant_id": gid})
	return out, nil
}

func (s *Server) refresh(r *http.Request, c *client) (*tokenResponse, error) {
	ctx := r.Context()
	tok := r.PostFormValue("refresh_token")
	var gid, exp, clientID, userID, scope string
	var used, revoked sql.NullString
	err := s.db.W.QueryRowContext(ctx, `SELECT t.grant_id, t.expires_at, t.used_at, g.client_id, g.user_id, g.scope, g.revoked_at
		FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id WHERE t.token_hash = ? AND t.kind = 'refresh'`, hash(tok)).
		Scan(&gid, &exp, &used, &clientID, &userID, &scope, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, invalidGrant("unknown refresh token")
	}
	if err != nil {
		return nil, err
	}
	if clientID != c.ID || revoked.Valid || s.nowStr() >= exp {
		return nil, invalidGrant("the refresh token is not valid")
	}
	if sc := r.PostFormValue("scope"); sc != "" && !subsetScope(sc, scope) {
		return nil, &grantError{http.StatusBadRequest, "invalid_scope", "scope exceeds what was granted"}
	}
	reuse := func() error {
		_ = s.revokeGrant(ctx, gid, "refresh_reuse")
		s.audit(r, userID, "oauth.refresh_reuse", map[string]any{"client_id": c.ID, "grant_id": gid})
		return invalidGrant("the refresh token was already used; the connection is revoked")
	}
	if used.Valid {
		return nil, reuse()
	}
	if u, err := s.svc.GetUser(ctx, userID); err != nil || u.Disabled {
		_ = s.revokeGrant(ctx, gid, "account_disabled")
		return nil, invalidGrant("the account cannot connect apps")
	}

	tx, err := s.db.W.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE oauth_tokens SET used_at = ? WHERE token_hash = ? AND used_at IS NULL`, s.nowStr(), hash(tok))
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		_ = tx.Rollback()
		return nil, reuse()
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE grant_id = ? AND kind = 'access'`, gid); err != nil {
		return nil, err
	}
	out, err := s.issue(ctx, tx, gid, scope)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	s.audit(r, userID, "oauth.token_refreshed", map[string]any{"client_id": c.ID, "grant_id": gid})
	return out, nil
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// issue stores a new access/refresh pair for a grant.
func (s *Server) issue(ctx context.Context, q execer, grantID, scope string) (*tokenResponse, error) {
	access, refresh := prefixAccess+random(43), prefixRefresh+random(43)
	now := s.now()
	for _, t := range []struct {
		tok, kind string
	}{{access, "access"}, {refresh, "refresh"}} {
		exp := store.FormatTime(now.Add(s.cfg.AccessTTL))
		if t.kind == "refresh" {
			exp = store.FormatTime(now.Add(s.cfg.RefreshTTL))
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO oauth_tokens (token_hash, grant_id, kind, expires_at, created_at) VALUES (?,?,?,?,?)`,
			hash(t.tok), grantID, t.kind, exp, s.nowStr()); err != nil {
			return nil, err
		}
	}
	return &tokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int(s.cfg.AccessTTL.Seconds()), RefreshToken: refresh, Scope: scope}, nil
}

// revoke is RFC 7009. It answers 200 whether or not the token existed, and revokes the
// whole grant (a client that discards one token has no use for the other).
func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	w.Header().Set("Cache-Control", "no-store")
	if ok, wait := s.revokeIP.Allow(s.cfg.ClientIP(r)); !ok {
		w.Header().Set("Retry-After", itoa(int(wait.Seconds())+1))
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "too many requests")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil || r.PostFormValue("token") == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "send token as a form field")
		return
	}
	var gid, clientID, userID string
	err := s.db.W.QueryRowContext(r.Context(), `SELECT g.id, g.client_id, g.user_id FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id WHERE t.token_hash = ?`,
		hash(r.PostFormValue("token"))).Scan(&gid, &clientID, &userID)
	if err == nil && clientID == r.PostFormValue("client_id") {
		if err := s.revokeGrant(r.Context(), gid, "client_revoked"); err == nil {
			s.audit(r, userID, "oauth.revoke", map[string]any{"client_id": clientID, "grant_id": gid, "by": "client"})
		}
	}
	w.WriteHeader(http.StatusOK)
}
