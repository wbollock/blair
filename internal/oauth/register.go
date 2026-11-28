package oauth

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"unicode"
)

type registration struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]string{"error": code, "error_description": desc})
}

// cleanName drops control characters and caps the length. The name is untrusted and is
// only ever shown HTML-escaped on the consent page.
func cleanName(n string) string {
	n = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, n)
	n = strings.TrimSpace(n)
	if r := []rune(n); len(r) > 100 {
		n = string(r[:100])
	}
	if n == "" {
		return "MCP client"
	}
	return n
}

// register is RFC 7591 dynamic registration for public clients.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	s.cors(w, r)
	if ok, wait := s.regIP.Allow(s.cfg.ClientIP(r)); !ok {
		w.Header().Set("Retry-After", itoa(int(wait.Seconds())+1))
		oauthError(w, http.StatusTooManyRequests, "temporarily_unavailable", "too many registrations; try again later")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8<<10))
	var in registration
	if err != nil || json.Unmarshal(body, &in) != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "send a JSON object under 8 KiB")
		return
	}
	if len(in.RedirectURIs) == 0 || len(in.RedirectURIs) > 5 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "send between 1 and 5 redirect_uris")
		return
	}
	for _, u := range in.RedirectURIs {
		if !s.allowedRedirect(u) {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "this server only accepts the Claude callback URLs")
			return
		}
	}
	if in.TokenEndpointAuthMethod != "" && in.TokenEndpointAuthMethod != "none" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "only public clients (token_endpoint_auth_method none) are supported")
		return
	}
	for _, g := range in.GrantTypes {
		if g != "authorization_code" && g != "refresh_token" {
			oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "unsupported grant_type "+g)
			return
		}
	}
	if len(in.GrantTypes) > 0 && !slices.Contains(in.GrantTypes, "authorization_code") {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "grant_types must include authorization_code")
		return
	}
	if len(in.ResponseTypes) > 0 && (len(in.ResponseTypes) != 1 || in.ResponseTypes[0] != "code") {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "response_types must be [code]")
		return
	}
	scope, ok := parseScope(in.Scope)
	if !ok {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "scope must be within: "+strings.Join(Scopes, " "))
		return
	}

	owner, ok := s.takeWindow(r.Context())
	if !ok {
		s.audit(r, "", "oauth.registration_denied", nil)
		oauthError(w, http.StatusForbidden, "access_denied", "registration is closed: open it in Blair under Settings, Connected apps, then add the connector again")
		return
	}
	id := "blair_c_" + random(32)
	name := cleanName(in.ClientName)
	if _, err := s.db.W.ExecContext(r.Context(), `INSERT INTO oauth_clients (id, owner_id, name, redirect_uris, created_at) VALUES (?,?,?,?,?)`,
		id, owner, name, strings.Join(in.RedirectURIs, " "), s.nowStr()); err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "could not register")
		return
	}
	s.audit(r, owner, "oauth.client_registered", map[string]any{"client_id": id})
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  id,
		"client_id_issued_at":        s.now().Unix(),
		"client_name":                name,
		"redirect_uris":              in.RedirectURIs,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
		"scope":                      scope,
	})
}
