package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/mail"
	"slices"
	"strings"
	"time"

	"github.com/wbollock/blair/internal/auth"
	"github.com/wbollock/blair/internal/store"
)

// Session lifetimes (SPEC §9.4).
const (
	SessionSliding  = 30 * 24 * time.Hour
	SessionAbsolute = 90 * 24 * time.Hour
	PreAuthTTL      = 5 * time.Minute
	MaxMFAAttempts  = 5
	InviteTTL       = 7 * 24 * time.Hour
)

// Scopes for personal access tokens (SPEC §9.5).
var Scopes = []string{"tasks:read", "tasks:write", "tasks:delete", "notes:read", "notes:write", "export", "account:read"}

type User struct {
	ID                 string          `json:"id"`
	Email              string          `json:"email"`
	DisplayName        string          `json:"display_name"`
	Timezone           string          `json:"timezone"`
	WeekStart          string          `json:"week_start"`
	IsAdmin            bool            `json:"is_admin"`
	TOTPEnabled        bool            `json:"totp_enabled"`
	MustChangePassword bool            `json:"must_change_password"`
	Disabled           bool            `json:"disabled"`
	Settings           json.RawMessage `json:"settings"`
	CreatedAt          string          `json:"created_at"`

	passwordHash string
	totpSecret   []byte
	totpLastStep int64
}

// Location returns the user's timezone, defaulting to UTC.
func (u *User) Location() *time.Location {
	if loc, err := time.LoadLocation(u.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

const userCols = `id, email, display_name, password_hash, timezone, week_start, is_admin, disabled_at,
	must_change_password, totp_secret_enc, totp_enabled_at, totp_last_step, settings, created_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	var u User
	var disabled, totpEnabled sql.NullString
	var settings string
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.passwordHash, &u.Timezone, &u.WeekStart, &u.IsAdmin,
		&disabled, &u.MustChangePassword, &u.totpSecret, &totpEnabled, &u.totpLastStep, &settings, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Disabled = disabled.Valid
	u.TOTPEnabled = totpEnabled.Valid
	u.Settings = json.RawMessage(settings)
	return &u, nil
}

func (s *Service) userByID(ctx context.Context, q querier, id string) (*User, error) {
	return scanUser(q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
}

// GetUser loads a user by id.
func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
	return s.userByID(ctx, s.db.R, id)
}

func (s *Service) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.R.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func normalizeEmail(e string) (string, error) {
	e = strings.TrimSpace(e)
	addr, err := mail.ParseAddress(e)
	if err != nil || addr.Address != e || len(e) > 254 {
		return "", invalid("email", "invalid", "enter a valid email address")
	}
	return strings.ToLower(e), nil
}

// NewUser is the input for account creation.
type NewUser struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
	Timezone    string `json:"timezone"`
}

func (s *Service) createUser(ctx context.Context, q querier, in NewUser, admin bool) (*User, error) {
	email, err := normalizeEmail(in.Email)
	if err != nil {
		return nil, err
	}
	if err := auth.CheckPasswordPolicy(in.Password); err != nil {
		return nil, invalid("password", "weak", err.Error())
	}
	tz := in.Timezone
	if tz == "" {
		tz = "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return nil, invalid("timezone", "invalid", "unknown IANA timezone")
	}
	name := strings.TrimSpace(in.DisplayName)
	if len(name) > 100 {
		return nil, invalid("display_name", "too_long", "at most 100 characters")
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}
	id, now := newID(), s.now()
	_, err = q.ExecContext(ctx, `INSERT INTO users (id, email, display_name, password_hash, timezone, is_admin, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, id, email, name, hash, tz, admin, now, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, conflict("an account with that email already exists")
		}
		return nil, err
	}
	return s.userByID(ctx, q, id)
}

// CreateUser is used by the CLI and admin API.
func (s *Service) CreateUser(ctx context.Context, in NewUser, admin bool) (*User, error) {
	return s.createUser(ctx, s.db.W, in, admin)
}

// ---- first-run setup (SPEC §9.1) ----

// SetupToken returns a one-time token valid for 1 hour, minted only while no users exist.
func (s *Service) SetupToken(ctx context.Context) (string, error) {
	n, err := s.CountUsers(ctx)
	if err != nil || n > 0 {
		return "", err
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()
	if s.setupToken == "" || s.clock().After(s.setupUntil) {
		s.setupToken = auth.NewSessionToken()
		s.setupUntil = s.clock().Add(time.Hour)
	}
	return s.setupToken, nil
}

// SetupRequired reports whether no account exists yet.
func (s *Service) SetupRequired(ctx context.Context) bool {
	n, err := s.CountUsers(ctx)
	return err == nil && n == 0
}

// CompleteSetup creates the first (admin) user with the setup token.
func (s *Service) CompleteSetup(ctx context.Context, token string, in NewUser) (*User, error) {
	s.setupMu.Lock()
	valid := s.setupToken != "" && s.clock().Before(s.setupUntil) &&
		subtle.ConstantTimeCompare([]byte(token), []byte(s.setupToken)) == 1
	s.setupMu.Unlock()
	if !valid {
		return nil, ErrForbidden
	}
	var u *User
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrForbidden
		}
		var err error
		u, err = s.createUser(ctx, tx, in, true)
		return err
	})
	if err == nil {
		s.setupMu.Lock()
		s.setupToken = ""
		s.setupMu.Unlock()
	}
	return u, err
}

// ---- invites & registration ----

type Invite struct {
	ID        string  `json:"id"`
	Email     *string `json:"email"`
	ExpiresAt string  `json:"expires_at"`
	UsedAt    *string `json:"used_at"`
	CreatedAt string  `json:"created_at"`
	Token     string  `json:"token,omitempty"` // only on create
}

func (s *Service) CreateInvite(ctx context.Context, admin *User, email string) (*Invite, error) {
	var emailP *string
	if email != "" {
		e, err := normalizeEmail(email)
		if err != nil {
			return nil, err
		}
		emailP = &e
	}
	tok := auth.NewSessionToken()
	inv := &Invite{ID: newID(), Email: emailP, ExpiresAt: store.FormatTime(s.clock().Add(InviteTTL)), CreatedAt: s.now(), Token: tok}
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO invites (id, token_hash, email, created_by, expires_at, created_at) VALUES (?,?,?,?,?,?)`,
		inv.ID, auth.HashToken(tok), nullStr(emailP), admin.ID, inv.ExpiresAt, inv.CreatedAt)
	return inv, err
}

func (s *Service) ListInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, email, expires_at, used_at, created_at FROM invites ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		var i Invite
		var email, used sql.NullString
		if err := rows.Scan(&i.ID, &email, &i.ExpiresAt, &used, &i.CreatedAt); err != nil {
			return nil, err
		}
		i.Email, i.UsedAt = strPtr(email), strPtr(used)
		out = append(out, i)
	}
	return out, rows.Err()
}

func (s *Service) DeleteInvite(ctx context.Context, id string) error {
	res, err := s.db.W.ExecContext(ctx, `DELETE FROM invites WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Register creates an account under the configured registration mode.
func (s *Service) Register(ctx context.Context, inviteToken string, in NewUser) (*User, error) {
	switch s.opts.Registration {
	case "open":
		return s.createUser(ctx, s.db.W, in, false)
	case "invite":
	default:
		return nil, ErrForbidden
	}
	var u *User
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var id string
		var email sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT id, email FROM invites WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
			auth.HashToken(inviteToken), s.now()).Scan(&id, &email)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForbidden
		}
		if err != nil {
			return err
		}
		if email.Valid && !strings.EqualFold(email.String, strings.TrimSpace(in.Email)) {
			return invalid("email", "invite_mismatch", "this invite is for a different email address")
		}
		u, err = s.createUser(ctx, tx, in, false)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE invites SET used_at = ?, used_by = ? WHERE id = ?`, s.now(), u.ID, id)
		return err
	})
	return u, err
}

// ---- login & sessions ----

type Session struct {
	ID         string `json:"id"`
	UserID     string `json:"-"`
	MFAPending bool   `json:"-"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent"`
	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`
	ExpiresAt  string `json:"expires_at"`
	Current    bool   `json:"current"`

	mfaAttempts int
}

// ClientInfo is request metadata recorded on sessions and audit events.
type ClientInfo struct {
	IP        string
	UserAgent string
}

// Authenticate checks email + password. Unknown emails burn equal time.
func (s *Service) Authenticate(ctx context.Context, email, password string) (*User, error) {
	ctx, sp := span(ctx, "Authenticate")
	defer sp.End()
	u, err := scanUser(s.db.R.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, strings.TrimSpace(email)))
	if errors.Is(err, ErrNotFound) {
		auth.BurnPasswordTime(password)
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	ok, rehash, err := auth.VerifyPassword(password, u.passwordHash)
	if err != nil || !ok {
		return nil, ErrUnauthorized
	}
	if u.Disabled {
		return nil, ErrUnauthorized
	}
	if rehash {
		if h, err := auth.HashPassword(password); err == nil {
			_, _ = s.db.W.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, h, u.ID)
		}
	}
	return u, nil
}

// CreateSession issues a session token. mfaPending sessions expire in 5 minutes.
func (s *Service) CreateSession(ctx context.Context, userID string, mfaPending bool, ci ClientInfo) (string, error) {
	tok := auth.NewSessionToken()
	now := s.clock()
	exp, abs := now.Add(SessionSliding), now.Add(SessionAbsolute)
	if mfaPending {
		exp, abs = now.Add(PreAuthTTL), now.Add(PreAuthTTL)
	}
	ua := ci.UserAgent
	if len(ua) > 300 {
		ua = ua[:300]
	}
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO sessions (id, user_id, token_hash, mfa_pending, ip, user_agent, created_at, last_seen_at, expires_at, absolute_expires_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, newID(), userID, auth.HashToken(tok), mfaPending, ci.IP, ua,
		store.FormatTime(now), store.FormatTime(now), store.FormatTime(exp), store.FormatTime(abs))
	return tok, err
}

// LookupSession resolves a session token. Expired or disabled users yield ErrUnauthorized.
func (s *Service) LookupSession(ctx context.Context, token string) (*Session, *User, error) {
	var sess Session
	var abs string
	err := s.db.R.QueryRowContext(ctx, `SELECT id, user_id, mfa_pending, mfa_attempts, ip, user_agent, created_at, last_seen_at, expires_at, absolute_expires_at
		FROM sessions WHERE token_hash = ?`, auth.HashToken(token)).Scan(&sess.ID, &sess.UserID, &sess.MFAPending, &sess.mfaAttempts,
		&sess.IP, &sess.UserAgent, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt, &abs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrUnauthorized
	}
	if err != nil {
		return nil, nil, err
	}
	now := s.now()
	if now >= sess.ExpiresAt || now >= abs {
		_, _ = s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sess.ID)
		return nil, nil, ErrUnauthorized
	}
	u, err := s.userByID(ctx, s.db.R, sess.UserID)
	if err != nil || u.Disabled {
		return nil, nil, ErrUnauthorized
	}
	// Slide the expiry at most once a minute to limit writes.
	if !sess.MFAPending {
		last, _ := time.Parse(store.TimeFormat, sess.LastSeenAt)
		if s.clock().Sub(last) > time.Minute {
			exp := s.clock().Add(SessionSliding)
			if a, _ := time.Parse(store.TimeFormat, abs); exp.After(a) {
				exp = a
			}
			sess.ExpiresAt = store.FormatTime(exp)
			_, _ = s.db.W.ExecContext(ctx, `UPDATE sessions SET last_seen_at = ?, expires_at = ? WHERE id = ?`, now, sess.ExpiresAt, sess.ID)
		}
	}
	return &sess, u, nil
}

// CompleteMFA verifies a TOTP or recovery code for a pre-auth session, deletes
// it, and returns a fresh full session token (rotation on privilege change).
func (s *Service) CompleteMFA(ctx context.Context, preAuth *Session, code, recovery string, ci ClientInfo) (string, *User, error) {
	if !preAuth.MFAPending {
		return "", nil, ErrForbidden
	}
	if preAuth.mfaAttempts >= MaxMFAAttempts {
		_, _ = s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, preAuth.ID)
		return "", nil, ErrUnauthorized
	}
	_, _ = s.db.W.ExecContext(ctx, `UPDATE sessions SET mfa_attempts = mfa_attempts + 1 WHERE id = ?`, preAuth.ID)
	u, err := s.userByID(ctx, s.db.R, preAuth.UserID)
	if err != nil {
		return "", nil, ErrUnauthorized
	}
	if err := s.verifySecondFactor(ctx, u, code, recovery); err != nil {
		return "", nil, err
	}
	_, _ = s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, preAuth.ID)
	tok, err := s.CreateSession(ctx, u.ID, false, ci)
	return tok, u, err
}

// VerifySecondFactor checks a TOTP or recovery code for a user who has just passed the
// password step outside the browser-session flow (the OAuth authorize page).
func (s *Service) VerifySecondFactor(ctx context.Context, userID, code, recovery string) error {
	u, err := s.userByID(ctx, s.db.R, userID)
	if err != nil || u.Disabled {
		return ErrUnauthorized
	}
	return s.verifySecondFactor(ctx, u, code, recovery)
}

func hashRecovery(code string) string {
	sum := sha256.Sum256([]byte("blair-recovery:" + auth.NormalizeRecoveryCode(code)))
	return hex.EncodeToString(sum[:])
}

func (s *Service) verifySecondFactor(ctx context.Context, u *User, code, recovery string) error {
	if !u.TOTPEnabled {
		return ErrForbidden
	}
	if recovery != "" {
		res, err := s.db.W.ExecContext(ctx, `UPDATE recovery_codes SET used_at = ? WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`,
			s.now(), u.ID, hashRecovery(recovery))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrUnauthorized
		}
		return nil
	}
	secret, err := s.box.Open(u.totpSecret)
	if err != nil {
		return err
	}
	step, ok := auth.VerifyTOTP(string(secret), code, s.clock(), u.totpLastStep)
	if !ok {
		return ErrUnauthorized
	}
	// Conditional update makes concurrent replays of one code fail.
	res, err := s.db.W.ExecContext(ctx, `UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_last_step < ?`, step, u.ID, step)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrUnauthorized
	}
	return nil
}

func (s *Service) DeleteSession(ctx context.Context, userID, sessionID string) error {
	res, err := s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE id = ? AND user_id = ?`, sessionID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeOtherSessions signs out everywhere except keepID (may be empty).
func (s *Service) RevokeOtherSessions(ctx context.Context, userID, keepID string) error {
	_, err := s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ? AND id != ?`, userID, keepID)
	return err
}

func (s *Service) ListSessions(ctx context.Context, userID, currentID string) ([]Session, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, ip, user_agent, created_at, last_seen_at, expires_at FROM sessions
		WHERE user_id = ? AND mfa_pending = 0 AND expires_at > ? ORDER BY last_seen_at DESC`, userID, s.now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var x Session
		if err := rows.Scan(&x.ID, &x.IP, &x.UserAgent, &x.CreatedAt, &x.LastSeenAt, &x.ExpiresAt); err != nil {
			return nil, err
		}
		x.Current = x.ID == currentID
		out = append(out, x)
	}
	return out, rows.Err()
}

// ---- profile & password ----

type ProfilePatch struct {
	DisplayName Opt[string]          `json:"display_name"`
	Timezone    Opt[string]          `json:"timezone"`
	WeekStart   Opt[string]          `json:"week_start"`
	Settings    Opt[json.RawMessage] `json:"settings"`
}

func (s *Service) UpdateProfile(ctx context.Context, u *User, p ProfilePatch) (*User, error) {
	if p.DisplayName.Set {
		n := strings.TrimSpace(p.DisplayName.V)
		if len(n) > 100 {
			return nil, invalid("display_name", "too_long", "at most 100 characters")
		}
		u.DisplayName = n
	}
	if p.Timezone.Set {
		if _, err := time.LoadLocation(p.Timezone.V); err != nil || p.Timezone.V == "" {
			return nil, invalid("timezone", "invalid", "unknown IANA timezone")
		}
		u.Timezone = p.Timezone.V
	}
	if p.WeekStart.Set {
		if p.WeekStart.V != "monday" && p.WeekStart.V != "sunday" {
			return nil, invalid("week_start", "invalid", "monday or sunday")
		}
		u.WeekStart = p.WeekStart.V
	}
	settings := string(u.Settings)
	if p.Settings.Set && !p.Settings.Null {
		var m map[string]any
		if err := json.Unmarshal(p.Settings.V, &m); err != nil || len(p.Settings.V) > 8192 {
			return nil, invalid("settings", "invalid", "settings must be a JSON object under 8 KB")
		}
		settings = string(p.Settings.V)
	}
	_, err := s.db.W.ExecContext(ctx, `UPDATE users SET display_name = ?, timezone = ?, week_start = ?, settings = ?, updated_at = ? WHERE id = ?`,
		u.DisplayName, u.Timezone, u.WeekStart, settings, s.now(), u.ID)
	if err != nil {
		return nil, err
	}
	return s.userByID(ctx, s.db.R, u.ID)
}

// ChangePassword verifies the current password and revokes all other sessions.
func (s *Service) ChangePassword(ctx context.Context, u *User, current, next, keepSession string) error {
	ok, _, err := auth.VerifyPassword(current, u.passwordHash)
	if err != nil || !ok {
		return invalid("current_password", "incorrect", "current password is incorrect")
	}
	if err := auth.CheckPasswordPolicy(next); err != nil {
		return invalid("new_password", "weak", err.Error())
	}
	h, err := auth.HashPassword(next)
	if err != nil {
		return err
	}
	if _, err := s.db.W.ExecContext(ctx, `UPDATE users SET password_hash = ?, must_change_password = 0, updated_at = ? WHERE id = ?`, h, s.now(), u.ID); err != nil {
		return err
	}
	return s.RevokeOtherSessions(ctx, u.ID, keepSession)
}

// VerifyPassword re-checks a password for sensitive actions.
func (s *Service) VerifyPassword(u *User, pw string) bool {
	ok, _, err := auth.VerifyPassword(pw, u.passwordHash)
	return err == nil && ok
}

// ---- TOTP enrollment ----

type TOTPSetup struct {
	Secret string `json:"secret"`
	URI    string `json:"otpauth_uri"`
}

// BeginTOTP stores a new (not yet enabled) secret.
func (s *Service) BeginTOTP(ctx context.Context, u *User) (*TOTPSetup, error) {
	if u.TOTPEnabled {
		return nil, conflict("two-factor authentication is already enabled")
	}
	secret, err := auth.NewTOTPSecret()
	if err != nil {
		return nil, err
	}
	if _, err := s.db.W.ExecContext(ctx, `UPDATE users SET totp_secret_enc = ?, totp_last_step = 0 WHERE id = ?`, s.box.Seal([]byte(secret)), u.ID); err != nil {
		return nil, err
	}
	return &TOTPSetup{Secret: secret, URI: auth.TOTPURI(secret, s.opts.Issuer, u.Email)}, nil
}

// ConfirmTOTP enables 2FA after a valid code and returns fresh recovery codes.
func (s *Service) ConfirmTOTP(ctx context.Context, u *User, code string) ([]string, error) {
	if u.TOTPEnabled {
		return nil, conflict("two-factor authentication is already enabled")
	}
	if len(u.totpSecret) == 0 {
		return nil, conflict("start enrollment first")
	}
	secret, err := s.box.Open(u.totpSecret)
	if err != nil {
		return nil, err
	}
	step, ok := auth.VerifyTOTP(string(secret), code, s.clock(), u.totpLastStep)
	if !ok {
		return nil, invalid("code", "incorrect", "that code is not valid; check your device clock")
	}
	var codes []string
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_enabled_at = ?, totp_last_step = ? WHERE id = ?`, s.now(), step, u.ID); err != nil {
			return err
		}
		var err error
		codes, err = s.replaceRecoveryCodes(ctx, tx, u.ID)
		return err
	})
	return codes, err
}

func (s *Service) replaceRecoveryCodes(ctx context.Context, q querier, userID string) ([]string, error) {
	if _, err := q.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	codes := make([]string, 10)
	for i := range codes {
		codes[i] = auth.NewRecoveryCode()
		if _, err := q.ExecContext(ctx, `INSERT INTO recovery_codes (id, user_id, code_hash) VALUES (?,?,?)`, newID(), userID, hashRecovery(codes[i])); err != nil {
			return nil, err
		}
	}
	return codes, nil
}

// RegenerateRecoveryCodes requires the password.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, u *User, password string) ([]string, error) {
	if !u.TOTPEnabled {
		return nil, conflict("two-factor authentication is not enabled")
	}
	if !s.VerifyPassword(u, password) {
		return nil, invalid("password", "incorrect", "password is incorrect")
	}
	var codes []string
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		codes, err = s.replaceRecoveryCodes(ctx, tx, u.ID)
		return err
	})
	return codes, err
}

// RecoveryCodesRemaining counts unused codes.
func (s *Service) RecoveryCodesRemaining(ctx context.Context, userID string) int {
	var n int
	_ = s.db.R.QueryRowContext(ctx, `SELECT count(*) FROM recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&n)
	return n
}

// DisableTOTP needs the password and a current code.
func (s *Service) DisableTOTP(ctx context.Context, u *User, password, code string) error {
	if !u.TOTPEnabled {
		return conflict("two-factor authentication is not enabled")
	}
	if s.opts.Require2FA {
		return ErrForbidden
	}
	if !s.VerifyPassword(u, password) {
		return invalid("password", "incorrect", "password is incorrect")
	}
	if err := s.verifySecondFactor(ctx, u, code, ""); err != nil {
		return invalid("code", "incorrect", "that code is not valid")
	}
	return s.resetTOTP(ctx, u.ID)
}

func (s *Service) resetTOTP(ctx context.Context, userID string) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET totp_secret_enc = NULL, totp_enabled_at = NULL, totp_last_step = 0 WHERE id = ?`, userID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID)
		return err
	})
}

// DeleteAccount removes the user and all their data after re-authentication.
func (s *Service) DeleteAccount(ctx context.Context, u *User, password, code string) error {
	if !s.VerifyPassword(u, password) {
		return invalid("password", "incorrect", "password is incorrect")
	}
	if u.TOTPEnabled {
		if err := s.verifySecondFactor(ctx, u, code, ""); err != nil {
			return invalid("code", "incorrect", "that code is not valid")
		}
	}
	if u.IsAdmin {
		var admins int
		_ = s.db.R.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE is_admin = 1 AND disabled_at IS NULL`).Scan(&admins)
		if admins <= 1 {
			return conflict("you are the only admin; promote another admin first")
		}
	}
	_, err := s.db.W.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, u.ID)
	return err
}

// ---- API tokens ----

type APIToken struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Prefix     string   `json:"prefix"`
	Scopes     []string `json:"scopes"`
	ExpiresAt  *string  `json:"expires_at"`
	LastUsedAt *string  `json:"last_used_at"`
	CreatedAt  string   `json:"created_at"`
	Token      string   `json:"token,omitempty"` // only on create
}

type NewToken struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresAt *string  `json:"expires_at"`
}

func (s *Service) CreateToken(ctx context.Context, u *User, in NewToken) (*APIToken, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 100 {
		return nil, invalid("name", "required", "name is required (max 100 characters)")
	}
	if len(in.Scopes) == 0 {
		return nil, invalid("scopes", "required", "choose at least one scope")
	}
	for _, sc := range in.Scopes {
		if !slices.Contains(Scopes, sc) {
			return nil, invalid("scopes", "invalid", "unknown scope "+sc)
		}
	}
	var exp *string
	if in.ExpiresAt != nil {
		v, ok := validTimestamp(*in.ExpiresAt)
		if !ok || v <= s.now() {
			return nil, invalid("expires_at", "invalid", "must be a future RFC 3339 timestamp")
		}
		exp = &v
	}
	tok, prefix := auth.NewPAT()
	t := &APIToken{ID: newID(), Name: name, Prefix: prefix, Scopes: in.Scopes, ExpiresAt: exp, CreatedAt: s.now(), Token: tok}
	_, err := s.db.W.ExecContext(ctx, `INSERT INTO api_tokens (id, user_id, name, token_hash, prefix, scopes, expires_at, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		t.ID, u.ID, name, auth.HashToken(tok), prefix, strings.Join(in.Scopes, " "), nullStr(exp), t.CreatedAt)
	return t, err
}

func (s *Service) ListTokens(ctx context.Context, userID string) ([]APIToken, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, name, prefix, scopes, expires_at, last_used_at, created_at FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIToken{}
	for rows.Next() {
		var t APIToken
		var scopes string
		var exp, used sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &t.Prefix, &scopes, &exp, &used, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Scopes, t.ExpiresAt, t.LastUsedAt = strings.Fields(scopes), strPtr(exp), strPtr(used)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Service) DeleteToken(ctx context.Context, userID, id string) error {
	res, err := s.db.W.ExecContext(ctx, `DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// LookupToken resolves a bearer token to its user and scopes.
func (s *Service) LookupToken(ctx context.Context, token string) (*User, []string, error) {
	if !strings.HasPrefix(token, auth.PATPrefix) {
		return nil, nil, ErrUnauthorized
	}
	var id, userID, scopes string
	var exp, used sql.NullString
	err := s.db.R.QueryRowContext(ctx, `SELECT id, user_id, scopes, expires_at, last_used_at FROM api_tokens WHERE token_hash = ?`,
		auth.HashToken(token)).Scan(&id, &userID, &scopes, &exp, &used)
	if err != nil {
		return nil, nil, ErrUnauthorized
	}
	if exp.Valid && s.now() >= exp.String {
		return nil, nil, ErrUnauthorized
	}
	u, err := s.userByID(ctx, s.db.R, userID)
	if err != nil || u.Disabled {
		return nil, nil, ErrUnauthorized
	}
	if !used.Valid || used.String < store.FormatTime(s.clock().Add(-time.Minute)) {
		_, _ = s.db.W.ExecContext(ctx, `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, s.now(), id)
	}
	return u, strings.Fields(scopes), nil
}

// ---- audit log ----

type AuditEvent struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	IP        string          `json:"ip"`
	UserAgent string          `json:"user_agent"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt string          `json:"created_at"`
}

// Audit records a security event. It never fails the request.
func (s *Service) Audit(ctx context.Context, userID, kind string, ci ClientInfo, detail map[string]any) {
	d := []byte("{}")
	if detail != nil {
		d, _ = json.Marshal(detail)
	}
	var uid any
	if userID != "" {
		uid = userID
	}
	_, _ = s.db.W.ExecContext(ctx, `INSERT INTO audit_events (id, user_id, kind, ip, user_agent, detail, created_at) VALUES (?,?,?,?,?,?,?)`,
		newID(), uid, kind, ci.IP, truncUA(ci.UserAgent), string(d), s.now())
	s.notifySecurity(ctx, userID, kind, ci, detail)
}

func (s *Service) ListAudit(ctx context.Context, userID string, limit int) ([]AuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, kind, ip, user_agent, detail, created_at FROM audit_events WHERE user_id = ? ORDER BY created_at DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEvent{}
	for rows.Next() {
		var e AuditEvent
		var d string
		if err := rows.Scan(&e.ID, &e.Kind, &e.IP, &e.UserAgent, &d, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Detail = json.RawMessage(d)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- admin ----

func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

type AdminUserPatch struct {
	Disabled Opt[bool]   `json:"disabled"`
	IsAdmin  Opt[bool]   `json:"is_admin"`
	Reset2FA Opt[bool]   `json:"reset_2fa"`
	Password Opt[string] `json:"password"`
}

// AdminUpdateUser applies admin changes. An admin can't demote or disable themselves.
func (s *Service) AdminUpdateUser(ctx context.Context, admin *User, id string, p AdminUserPatch) (*User, error) {
	u, err := s.userByID(ctx, s.db.R, id)
	if err != nil {
		return nil, err
	}
	if id == admin.ID && ((p.Disabled.Set && p.Disabled.V) || (p.IsAdmin.Set && !p.IsAdmin.V)) {
		return nil, conflict("you cannot disable or demote yourself")
	}
	if p.Disabled.Set {
		var v any
		if p.Disabled.V {
			v = s.now()
		}
		if _, err := s.db.W.ExecContext(ctx, `UPDATE users SET disabled_at = ? WHERE id = ?`, v, id); err != nil {
			return nil, err
		}
		if p.Disabled.V {
			_ = s.RevokeOtherSessions(ctx, id, "")
		}
	}
	if p.IsAdmin.Set {
		if _, err := s.db.W.ExecContext(ctx, `UPDATE users SET is_admin = ? WHERE id = ?`, p.IsAdmin.V, id); err != nil {
			return nil, err
		}
	}
	if p.Reset2FA.Set && p.Reset2FA.V {
		if err := s.resetTOTP(ctx, id); err != nil {
			return nil, err
		}
	}
	if p.Password.Set {
		if err := auth.CheckPasswordPolicy(p.Password.V); err != nil {
			return nil, invalid("password", "weak", err.Error())
		}
		h, err := auth.HashPassword(p.Password.V)
		if err != nil {
			return nil, err
		}
		if _, err := s.db.W.ExecContext(ctx, `UPDATE users SET password_hash = ?, must_change_password = 1 WHERE id = ?`, h, id); err != nil {
			return nil, err
		}
		_ = s.RevokeOtherSessions(ctx, id, "")
	}
	_ = u
	return s.userByID(ctx, s.db.R, id)
}

// AdminDeleteUser removes another user and all their data.
func (s *Service) AdminDeleteUser(ctx context.Context, admin *User, id string) error {
	if id == admin.ID {
		return conflict("use account deletion to delete yourself")
	}
	res, err := s.db.W.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Housekeeping deletes expired sessions and used/expired invites.
func (s *Service) Housekeeping(ctx context.Context) error {
	now := s.now()
	_, err1 := s.db.W.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ? OR absolute_expires_at <= ?`, now, now)
	_, err2 := s.db.W.ExecContext(ctx, `DELETE FROM invites WHERE expires_at <= ?`, now)
	purge := store.FormatTime(s.clock().Add(-30 * 24 * time.Hour))
	_, err3 := s.db.W.ExecContext(ctx, `DELETE FROM tasks WHERE deleted_at IS NOT NULL AND deleted_at <= ?`, purge)
	_, err4 := s.db.W.ExecContext(ctx, `DELETE FROM notes WHERE deleted_at IS NOT NULL AND deleted_at <= ?`, purge)
	cutoff := store.FormatTime(s.clock().Add(-365 * 24 * time.Hour))
	_, err5 := s.db.W.ExecContext(ctx, `DELETE FROM audit_events WHERE created_at <= ?`, cutoff)
	return errors.Join(err1, err2, err3, err4, err5, s.oauthHousekeeping(ctx, now))
}
