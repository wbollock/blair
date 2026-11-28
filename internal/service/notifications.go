package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Notification is an in-app notification (SPEC §8). Reminder notifications
// carry the task's current title; security ones carry their text.
type Notification struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	TaskID    *string `json:"task_id"`
	TaskTitle *string `json:"task_title"`
	Text      string  `json:"text"`
	CreatedAt string  `json:"created_at"`
	ReadAt    *string `json:"read_at"`
}

type NotificationPage struct {
	Data        []Notification `json:"data"`
	UnreadCount int            `json:"unread_count"`
}

// securityText says what happened for audit kinds worth a notification.
// login.success is handled separately: only a new browser is notable.
var securityText = map[string]string{
	"login.mfa_failure":        "Someone entered your password correctly but failed the 2FA check. If this wasn't you, change your password.",
	"password.changed":         "Your password was changed.",
	"2fa.enabled":              "Two-factor authentication was turned on.",
	"2fa.disabled":             "Two-factor authentication was turned off.",
	"2fa.recovery_regenerated": "New 2FA recovery codes were generated. The old ones no longer work.",
	"token.created":            "A new API token was created.",
	"oauth.client_registered":  "An app registered itself with your account. If you did not just add one in Claude, ignore any sign-in link for it; it cannot connect without your password and 2FA.",
	"oauth.authorize":          "An app was connected to your account.",
	"oauth.refresh_reuse":      "A connected app reused an old token, so its access was revoked.",
}

// notifySecurity adds a security notification for an audit event when it is
// notable. It runs after the audit row is written, so a first sign-in from a
// browser sees exactly one matching login.success row.
func (s *Service) notifySecurity(ctx context.Context, userID, kind string, ci ClientInfo, detail map[string]any) {
	if userID == "" {
		return
	}
	text, ok := securityText[kind]
	switch {
	case kind == "login.success":
		var n int
		if err := s.db.W.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE user_id = ? AND kind = 'login.success' AND user_agent = ?`,
			userID, truncUA(ci.UserAgent)).Scan(&n); err != nil || n != 1 {
			return
		}
		text = fmt.Sprintf("New sign-in from %s (%s).", browserName(ci.UserAgent), ci.IP)
		if rc, _ := detail["recovery_code"].(bool); rc {
			text = fmt.Sprintf("Signed in with a recovery code from %s (%s).", browserName(ci.UserAgent), ci.IP)
		}
	case !ok:
		return
	}
	_, _ = s.db.W.ExecContext(ctx, `INSERT INTO notifications (id, owner_id, kind, text, created_at) VALUES (?,?,?,?,?)`,
		newID(), userID, "security."+kind, text, s.now())
}

func truncUA(ua string) string {
	if len(ua) > 300 {
		return ua[:300]
	}
	return ua
}

// browserName turns a User-Agent into "Firefox on Android" for notification text.
func browserName(ua string) string {
	pick := func(pairs ...string) string {
		for i := 0; i < len(pairs); i += 2 {
			if strings.Contains(ua, pairs[i]) {
				return pairs[i+1]
			}
		}
		return ""
	}
	b := pick("Firefox/", "Firefox", "Edg/", "Edge", "Chrome/", "Chrome", "Safari/", "Safari", "curl/", "curl")
	o := pick("iPhone", "iOS", "iPad", "iPadOS", "Android", "Android", "Mac OS X", "macOS", "Windows", "Windows", "Linux", "Linux")
	switch {
	case b == "":
		return "an unknown browser"
	case o == "":
		return b
	}
	return b + " on " + o
}

// FireReminders turns due reminders into notifications. The UPDATE … RETURNING
// and the inserts share one transaction, so each reminder fires exactly once.
func (s *Service) FireReminders(ctx context.Context) (int, error) {
	ctx, sp := span(ctx, "FireReminders")
	defer sp.End()
	n := 0
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		now := s.now()
		rows, err := tx.QueryContext(ctx, `UPDATE tasks SET reminded_at = ?
			WHERE remind_at IS NOT NULL AND reminded_at IS NULL AND remind_at <= ? AND deleted_at IS NULL AND status NOT IN ('done','cancelled')
			RETURNING id, owner_id`, now, now)
		if err != nil {
			return err
		}
		type due struct{ task, owner string }
		var fired []due
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.task, &d.owner); err != nil {
				rows.Close()
				return err
			}
			fired = append(fired, d)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, d := range fired {
			if _, err := tx.ExecContext(ctx, `INSERT INTO notifications (id, owner_id, kind, task_id, created_at) VALUES (?,?,'reminder',?,?)`,
				newID(), d.owner, d.task, now); err != nil {
				return err
			}
		}
		n = len(fired)
		return nil
	})
	return n, err
}

func (s *Service) ListNotifications(ctx context.Context, u *User, unreadOnly bool, limit int) (*NotificationPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `SELECT n.id, n.kind, n.task_id, t.title, n.text, n.created_at, n.read_at FROM notifications n
		LEFT JOIN tasks t ON t.id = n.task_id AND t.owner_id = n.owner_id WHERE n.owner_id = ?`
	if unreadOnly {
		q += ` AND n.read_at IS NULL`
	}
	rows, err := s.db.R.QueryContext(ctx, q+` ORDER BY n.created_at DESC, n.id DESC LIMIT ?`, u.ID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &NotificationPage{Data: []Notification{}}
	for rows.Next() {
		var n Notification
		var tid, title, read sql.NullString
		if err := rows.Scan(&n.ID, &n.Kind, &tid, &title, &n.Text, &n.CreatedAt, &read); err != nil {
			return nil, err
		}
		n.TaskID, n.TaskTitle, n.ReadAt = strPtr(tid), strPtr(title), strPtr(read)
		page.Data = append(page.Data, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	err = s.db.R.QueryRowContext(ctx, `SELECT count(*) FROM notifications WHERE owner_id = ? AND read_at IS NULL`, u.ID).Scan(&page.UnreadCount)
	return page, err
}

// MarkNotificationsRead marks the given notifications read, or all of them when ids is empty.
func (s *Service) MarkNotificationsRead(ctx context.Context, u *User, ids []string) error {
	if len(ids) > 500 {
		return invalid("ids", "too_many", "at most 500 ids")
	}
	q, args := `UPDATE notifications SET read_at = ? WHERE owner_id = ? AND read_at IS NULL`, []any{s.now(), u.ID}
	if len(ids) > 0 {
		q += ` AND id IN (` + placeholders(len(ids)) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	_, err := s.db.W.ExecContext(ctx, q, args...)
	return err
}
