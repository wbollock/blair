package oauth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/wbollock/blair/internal/service"
	"github.com/wbollock/blair/internal/store"
)

// ErrInvalidToken is returned for unknown, expired, revoked or wrongly bound access tokens.
var ErrInvalidToken = errors.New("invalid token")

// Principal is who an access token speaks for.
type Principal struct {
	User     *service.User
	GrantID  string
	ClientID string
	Scopes   []string
	Expires  time.Time
}

// VerifyAccessToken resolves a bearer token for the MCP endpoint. A token is only valid
// when its grant is live, was issued for this resource, and its user is still enabled.
func (s *Server) VerifyAccessToken(ctx context.Context, tok string) (*Principal, error) {
	if !strings.HasPrefix(tok, prefixAccess) {
		return nil, ErrInvalidToken
	}
	var exp, gid, uid, cid, scope, resource string
	var revoked, lastUsed sql.NullString
	err := s.db.R.QueryRowContext(ctx, `SELECT t.expires_at, g.id, g.user_id, g.client_id, g.scope, g.resource, g.revoked_at, g.last_used_at
		FROM oauth_tokens t JOIN oauth_grants g ON g.id = t.grant_id WHERE t.token_hash = ? AND t.kind = 'access'`, hash(tok)).
		Scan(&exp, &gid, &uid, &cid, &scope, &resource, &revoked, &lastUsed)
	if err != nil {
		return nil, ErrInvalidToken
	}
	now := s.nowStr()
	if revoked.Valid || now >= exp || resource != s.Resource() {
		return nil, ErrInvalidToken
	}
	u, err := s.svc.GetUser(ctx, uid)
	if err != nil || u.Disabled {
		return nil, ErrInvalidToken
	}
	if !lastUsed.Valid || lastUsed.String < store.FormatTime(s.now().Add(-time.Minute)) {
		_, _ = s.db.W.ExecContext(ctx, `UPDATE oauth_grants SET last_used_at = ? WHERE id = ?`, now, gid)
	}
	et, _ := time.Parse(store.TimeFormat, exp)
	return &Principal{User: u, GrantID: gid, ClientID: cid, Scopes: strings.Fields(scope), Expires: et}, nil
}

// ConnectedApp is one live grant, as the Connected apps page shows it.
type ConnectedApp struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Scopes     []string `json:"scopes"`
	CreatedAt  string   `json:"created_at"`
	LastUsedAt *string  `json:"last_used_at"`
}

// ListGrants returns the user's connected apps, newest first.
func (s *Server) ListGrants(ctx context.Context, userID string) ([]ConnectedApp, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT g.id, c.name, g.scope, g.created_at, g.last_used_at FROM oauth_grants g JOIN oauth_clients c ON c.id = g.client_id
		WHERE g.user_id = ? AND g.revoked_at IS NULL
		AND EXISTS (SELECT 1 FROM oauth_tokens t WHERE t.grant_id = g.id AND t.kind = 'refresh' AND t.used_at IS NULL AND t.expires_at > ?)
		ORDER BY g.created_at DESC`, userID, s.nowStr())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ConnectedApp{}
	for rows.Next() {
		var a ConnectedApp
		var scope string
		var used sql.NullString
		if err := rows.Scan(&a.ID, &a.Name, &scope, &a.CreatedAt, &used); err != nil {
			return nil, err
		}
		a.Scopes = strings.Fields(scope)
		if used.Valid {
			a.LastUsedAt = &used.String
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RevokeGrant disconnects one of the user's apps. Its tokens stop working at once.
func (s *Server) RevokeGrant(ctx context.Context, userID, id string) error {
	var clientID string
	err := s.db.W.QueryRowContext(ctx, `SELECT client_id FROM oauth_grants WHERE id = ? AND user_id = ? AND revoked_at IS NULL`, id, userID).Scan(&clientID)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.revokeGrant(ctx, id, "user")
}

// IsOAuthToken reports whether tok looks like an OAuth access or refresh token. The REST
// API uses it to refuse them with a clear error instead of treating them as unknown PATs.
func IsOAuthToken(tok string) bool {
	return strings.HasPrefix(tok, prefixAccess) || strings.HasPrefix(tok, prefixRefresh)
}
