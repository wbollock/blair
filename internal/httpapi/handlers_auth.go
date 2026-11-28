package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/wbollock/blair/internal/service"
)

func (a *API) setCookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: maxAge, HttpOnly: true, Secure: a.cfg.Secure, SameSite: http.SameSiteLaxMode})
}

func (a *API) startSession(w http.ResponseWriter, r *http.Request, u *service.User) error {
	tok, err := a.svc.CreateSession(r.Context(), u.ID, false, clientInfo(r))
	if err != nil {
		return err
	}
	a.setCookie(w, a.cookieName, tok, int(service.SessionAbsolute.Seconds()))
	a.setCookie(w, a.preName, "", -1)
	return nil
}

type authStatus struct {
	SetupRequired bool          `json:"setup_required"`
	Registration  string        `json:"registration"`
	Require2FA    bool          `json:"require_2fa"`
	Authenticated bool          `json:"authenticated"`
	MFAPending    bool          `json:"mfa_pending"`
	User          *service.User `json:"user"`
	Version       string        `json:"version"`
}

func (a *API) authStatus(w http.ResponseWriter, r *http.Request) {
	st := authStatus{
		SetupRequired: a.svc.SetupRequired(r.Context()),
		Registration:  a.svc.Options().Registration,
		Require2FA:    a.svc.Options().Require2FA,
		Version:       a.cfg.Version,
	}
	if u := userFrom(r.Context()); u != nil {
		st.Authenticated, st.User = true, u
	} else if c, err := r.Cookie(a.preName); err == nil {
		if sess, _, err := a.svc.LookupSession(r.Context(), c.Value); err == nil && sess.MFAPending {
			st.MFAPending = true
		}
	}
	writeJSON(w, http.StatusOK, st)
}

type setupRequest struct {
	Token string `json:"token"`
	service.NewUser
}

func (a *API) setup(w http.ResponseWriter, r *http.Request) {
	if ok, wait := a.loginIPLimit.Allow("setup:" + clientIP(r.Context())); !ok {
		tooMany(w, r, wait)
		return
	}
	var in setupRequest
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	u, err := a.svc.CompleteSetup(r.Context(), in.Token, in.NewUser)
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeProblem(w, r, http.StatusForbidden, "forbidden", "Setup unavailable", "the setup token is invalid or expired, or setup is already done")
			return
		}
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "account.setup", clientInfo(r), nil)
	if err := a.startSession(w, r, u); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

type registerRequest struct {
	InviteToken string `json:"invite_token"`
	service.NewUser
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	if ok, wait := a.loginIPLimit.Allow("register:" + clientIP(r.Context())); !ok {
		tooMany(w, r, wait)
		return
	}
	var in registerRequest
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	u, err := a.svc.Register(r.Context(), in.InviteToken, in.NewUser)
	if err != nil {
		if errors.Is(err, service.ErrForbidden) {
			writeProblem(w, r, http.StatusForbidden, "forbidden", "Registration closed", "registration needs a valid invite")
			return
		}
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "account.register", clientInfo(r), nil)
	if err := a.startSession(w, r, u); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	MFARequired bool          `json:"mfa_required"`
	User        *service.User `json:"user,omitempty"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if ok, wait := a.loginIPLimit.Allow("login:" + clientIP(r.Context())); !ok {
		tooMany(w, r, wait)
		return
	}
	if ok, wait := a.loginEmailLimit.Allow(strings.ToLower(strings.TrimSpace(in.Email))); !ok {
		tooMany(w, r, wait)
		return
	}
	u, err := a.svc.Authenticate(r.Context(), in.Email, in.Password)
	if err != nil {
		if errors.Is(err, service.ErrUnauthorized) {
			a.svc.Audit(r.Context(), "", "login.failure", clientInfo(r), nil)
			writeProblem(w, r, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password", "")
			return
		}
		writeError(w, r, err)
		return
	}
	if u.TOTPEnabled {
		tok, err := a.svc.CreateSession(r.Context(), u.ID, true, clientInfo(r))
		if err != nil {
			writeError(w, r, err)
			return
		}
		a.setCookie(w, a.preName, tok, int(service.PreAuthTTL.Seconds()))
		a.svc.Audit(r.Context(), u.ID, "login.password_ok", clientInfo(r), nil)
		writeJSON(w, http.StatusOK, loginResponse{MFARequired: true})
		return
	}
	if err := a.startSession(w, r, u); err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "login.success", clientInfo(r), map[string]any{"mfa": false})
	writeJSON(w, http.StatusOK, loginResponse{User: u})
}

type mfaRequest struct {
	Code         string `json:"code"`
	RecoveryCode string `json:"recovery_code"`
}

func (a *API) loginMFA(w http.ResponseWriter, r *http.Request) {
	if ok, wait := a.loginIPLimit.Allow("mfa:" + clientIP(r.Context())); !ok {
		tooMany(w, r, wait)
		return
	}
	var in mfaRequest
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	c, err := r.Cookie(a.preName)
	if err != nil {
		writeProblem(w, r, http.StatusUnauthorized, "mfa_expired", "Sign-in expired", "enter your password again")
		return
	}
	sess, _, err := a.svc.LookupSession(r.Context(), c.Value)
	if err != nil || !sess.MFAPending {
		writeProblem(w, r, http.StatusUnauthorized, "mfa_expired", "Sign-in expired", "enter your password again")
		return
	}
	tok, u, err := a.svc.CompleteMFA(r.Context(), sess, in.Code, in.RecoveryCode, clientInfo(r))
	if err != nil {
		a.svc.Audit(r.Context(), sess.UserID, "login.mfa_failure", clientInfo(r), nil)
		writeProblem(w, r, http.StatusUnauthorized, "invalid_code", "That code didn't work", "")
		return
	}
	a.setCookie(w, a.cookieName, tok, int(service.SessionAbsolute.Seconds()))
	a.setCookie(w, a.preName, "", -1)
	a.svc.Audit(r.Context(), u.ID, "login.success", clientInfo(r), map[string]any{"mfa": true, "recovery_code": in.RecoveryCode != ""})
	writeJSON(w, http.StatusOK, loginResponse{User: u})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if s := sessionFrom(r.Context()); s != nil {
		_ = a.svc.DeleteSession(r.Context(), s.UserID, s.ID)
		a.svc.Audit(r.Context(), s.UserID, "logout", clientInfo(r), nil)
	}
	a.setCookie(w, a.cookieName, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

// ---- me ----

type meResponse struct {
	*service.User
	RecoveryCodesRemaining int `json:"recovery_codes_remaining"`
}

func (a *API) getMe(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	writeJSON(w, http.StatusOK, meResponse{User: u, RecoveryCodesRemaining: a.svc.RecoveryCodesRemaining(r.Context(), u.ID)})
}

func (a *API) patchMe(w http.ResponseWriter, r *http.Request) {
	var in service.ProfilePatch
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	u, err := a.svc.UpdateProfile(r.Context(), userFrom(r.Context()), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, meResponse{User: u, RecoveryCodesRemaining: a.svc.RecoveryCodesRemaining(r.Context(), u.ID)})
}

type passwordConfirm struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (a *API) deleteMe(w http.ResponseWriter, r *http.Request) {
	var in passwordConfirm
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := a.svc.DeleteAccount(r.Context(), userFrom(r.Context()), in.Password, in.Code); err != nil {
		writeError(w, r, err)
		return
	}
	a.setCookie(w, a.cookieName, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (a *API) changePassword(w http.ResponseWriter, r *http.Request) {
	var in changePasswordRequest
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	u, s := userFrom(r.Context()), sessionFrom(r.Context())
	if err := a.svc.ChangePassword(r.Context(), u, in.CurrentPassword, in.NewPassword, s.ID); err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "password.changed", clientInfo(r), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) beginTOTP(w http.ResponseWriter, r *http.Request) {
	setup, err := a.svc.BeginTOTP(r.Context(), userFrom(r.Context()))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, setup)
}

type codeRequest struct {
	Code string `json:"code"`
}

type recoveryCodes struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

func (a *API) confirmTOTP(w http.ResponseWriter, r *http.Request) {
	var in codeRequest
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	u := userFrom(r.Context())
	codes, err := a.svc.ConfirmTOTP(r.Context(), u, in.Code)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "2fa.enabled", clientInfo(r), nil)
	writeJSON(w, http.StatusOK, recoveryCodes{codes})
}

func (a *API) disableTOTP(w http.ResponseWriter, r *http.Request) {
	var in passwordConfirm
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	u := userFrom(r.Context())
	if err := a.svc.DisableTOTP(r.Context(), u, in.Password, in.Code); err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "2fa.disabled", clientInfo(r), nil)
	w.WriteHeader(http.StatusNoContent)
}

type passwordOnly struct {
	Password string `json:"password"`
}

func (a *API) regenRecovery(w http.ResponseWriter, r *http.Request) {
	var in passwordOnly
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	u := userFrom(r.Context())
	codes, err := a.svc.RegenerateRecoveryCodes(r.Context(), u, in.Password)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "2fa.recovery_regenerated", clientInfo(r), nil)
	writeJSON(w, http.StatusOK, recoveryCodes{codes})
}

func (a *API) listSessions(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.ListSessions(r.Context(), userFrom(r.Context()).ID, sessionFrom(r.Context()).ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (a *API) deleteSession(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if err := a.svc.DeleteSession(r.Context(), u.ID, chi.URLParam(r, "id")); err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "session.revoked", clientInfo(r), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if err := a.svc.RevokeOtherSessions(r.Context(), u.ID, sessionFrom(r.Context()).ID); err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "session.revoked_others", clientInfo(r), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listTokens(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.ListTokens(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (a *API) createToken(w http.ResponseWriter, r *http.Request) {
	var in service.NewToken
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	u := userFrom(r.Context())
	t, err := a.svc.CreateToken(r.Context(), u, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "token.created", clientInfo(r), map[string]any{"token_id": t.ID, "scopes": t.Scopes})
	writeJSON(w, http.StatusCreated, t)
}

func (a *API) deleteToken(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	id := chi.URLParam(r, "id")
	if err := a.svc.DeleteToken(r.Context(), u.ID, id); err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "token.revoked", clientInfo(r), map[string]any{"token_id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listConnectedApps(w http.ResponseWriter, r *http.Request) {
	if a.oauth == nil {
		writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
		return
	}
	out, err := a.oauth.ListGrants(r.Context(), userFrom(r.Context()).ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

// openRegistration lets one app register itself for ten minutes (ADR-0016).
func (a *API) openRegistration(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if a.oauth == nil || !a.cfg.MCPEnabled {
		writeProblem(w, r, http.StatusNotFound, "not_found", "Not found", "the MCP connector is not enabled")
		return
	}
	if !u.TOTPEnabled {
		writeProblem(w, r, http.StatusForbidden, "2fa_required", "Two-factor authentication required", "turn on 2FA before connecting apps")
		return
	}
	exp, err := a.oauth.OpenRegistration(r.Context(), u.ID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "oauth.registration_opened", clientInfo(r), nil)
	writeJSON(w, http.StatusOK, map[string]any{"expires_at": exp.UTC().Format("2006-01-02T15:04:05Z")})
}

func (a *API) deleteConnectedApp(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	if a.oauth == nil {
		writeError(w, r, service.ErrNotFound)
		return
	}
	id := chi.URLParam(r, "id")
	if err := a.oauth.RevokeGrant(r.Context(), u.ID, id); err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), u.ID, "oauth.revoke", clientInfo(r), map[string]any{"grant_id": id, "by": "user"})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.ListAudit(r.Context(), userFrom(r.Context()).ID, 100)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

// ---- admin ----

func (a *API) adminListUsers(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.ListUsers(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

type adminCreateUser struct {
	service.NewUser
	IsAdmin bool `json:"is_admin"`
}

func (a *API) adminCreateUser(w http.ResponseWriter, r *http.Request) {
	var in adminCreateUser
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	u, err := a.svc.CreateUser(r.Context(), in.NewUser, in.IsAdmin)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), userFrom(r.Context()).ID, "admin.user_created", clientInfo(r), map[string]any{"user_id": u.ID})
	writeJSON(w, http.StatusCreated, u)
}

func (a *API) adminPatchUser(w http.ResponseWriter, r *http.Request) {
	var in service.AdminUserPatch
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	admin := userFrom(r.Context())
	id := chi.URLParam(r, "id")
	u, err := a.svc.AdminUpdateUser(r.Context(), admin, id, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), admin.ID, "admin.user_updated", clientInfo(r), map[string]any{"user_id": id,
		"disabled": in.Disabled.Set, "is_admin": in.IsAdmin.Set, "reset_2fa": in.Reset2FA.Set, "password": in.Password.Set})
	writeJSON(w, http.StatusOK, u)
}

func (a *API) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	admin := userFrom(r.Context())
	id := chi.URLParam(r, "id")
	if err := a.svc.AdminDeleteUser(r.Context(), admin, id); err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), admin.ID, "admin.user_deleted", clientInfo(r), map[string]any{"user_id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) adminListInvites(w http.ResponseWriter, r *http.Request) {
	out, err := a.svc.ListInvites(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

type inviteRequest struct {
	Email string `json:"email"`
}

func (a *API) adminCreateInvite(w http.ResponseWriter, r *http.Request) {
	var in inviteRequest
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	admin := userFrom(r.Context())
	inv, err := a.svc.CreateInvite(r.Context(), admin, in.Email)
	if err != nil {
		writeError(w, r, err)
		return
	}
	a.svc.Audit(r.Context(), admin.ID, "admin.invite_created", clientInfo(r), map[string]any{"invite_id": inv.ID})
	writeJSON(w, http.StatusCreated, inv)
}

func (a *API) adminDeleteInvite(w http.ResponseWriter, r *http.Request) {
	if err := a.svc.DeleteInvite(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- notifications ----

func (a *API) listNotifications(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := a.svc.ListNotifications(r.Context(), userFrom(r.Context()), r.URL.Query().Get("unread") == "true", limit)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) readNotifications(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	if err := a.svc.MarkNotificationsRead(r.Context(), userFrom(r.Context()), in.IDs); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
