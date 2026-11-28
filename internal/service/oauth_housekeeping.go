package service

import (
	"context"
	"database/sql"
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
	// Only registrations that were never approved are pruned. An approved client stays so the app
	// can reconnect with the id it already has.
	_, e5 := s.db.W.ExecContext(ctx, `DELETE FROM oauth_clients WHERE created_at <= ? AND approved_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM oauth_requests r WHERE r.client_id = oauth_clients.id)`, store.FormatTime(s.clock().Add(-24*time.Hour)))
	return errors.Join(e1, e2, e3, e4, e5)
}

// RevokeOAuthGrants disconnects every app for a user. It runs when the password changes or
// 2FA is turned off or reset, because those are the checks that let the apps in.
func (s *Service) RevokeOAuthGrants(ctx context.Context, userID, reason string) error {
	// One writer transaction: a code exchange or a consent either finishes before this runs, and
	// its grant is revoked here, or starts after, and finds its code and sign-in already gone.
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_tokens WHERE kind = 'access' AND grant_id IN (SELECT id FROM oauth_grants WHERE user_id = ? AND revoked_at IS NULL)`, userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE oauth_grants SET revoked_at = ?, revoked_reason = ? WHERE user_id = ? AND revoked_at IS NULL`, s.now(), reason, userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_requests WHERE user_id = ?`, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM oauth_codes WHERE user_id = ? AND used_at IS NULL`, userID)
		return err
	})
}
