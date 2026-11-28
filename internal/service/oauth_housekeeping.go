package service

import (
	"context"
	"errors"
	"time"

	"github.com/wbollock/blair/internal/store"
)

// oauthHousekeeping removes expired OAuth flow state (ADR-0016). Used refresh tokens stay
// until they expire so that replaying one is still detected.
func (s *Service) oauthHousekeeping(ctx context.Context, now string) error {
	_, e1 := s.db.W.ExecContext(ctx, `DELETE FROM oauth_requests WHERE expires_at <= ?`, now)
	_, e2 := s.db.W.ExecContext(ctx, `DELETE FROM oauth_codes WHERE expires_at <= ?`, store.FormatTime(s.clock().Add(-24*time.Hour)))
	_, e3 := s.db.W.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE expires_at <= ?`, now)
	// Revoked or fully expired grants are history we do not need to keep.
	_, e4 := s.db.W.ExecContext(ctx, `DELETE FROM oauth_grants WHERE (revoked_at IS NOT NULL AND revoked_at <= ?)
		OR NOT EXISTS (SELECT 1 FROM oauth_tokens t WHERE t.grant_id = oauth_grants.id)`, store.FormatTime(s.clock().Add(-7*24*time.Hour)))
	// Dynamic registration is open to anyone, so clients that never got used are pruned.
	_, e5 := s.db.W.ExecContext(ctx, `DELETE FROM oauth_clients WHERE created_at <= ?
		AND NOT EXISTS (SELECT 1 FROM oauth_grants g WHERE g.client_id = oauth_clients.id)
		AND NOT EXISTS (SELECT 1 FROM oauth_requests r WHERE r.client_id = oauth_clients.id)`, store.FormatTime(s.clock().Add(-24*time.Hour)))
	return errors.Join(e1, e2, e3, e4, e5)
}
