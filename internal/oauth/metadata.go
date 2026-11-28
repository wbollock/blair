package oauth

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// protectedResource is the RFC 9728 document that /mcp's 401 points at.
func (s *Server) protectedResource(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 s.Resource(),
		"authorization_servers":    []string{s.Issuer()},
		"scopes_supported":         Scopes,
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "Blair",
	})
}

// authServerMetadata is the RFC 8414 document.
func (s *Server) authServerMetadata(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                         s.Issuer(),
		"authorization_endpoint":                         s.base + "/oauth/authorize",
		"token_endpoint":                                 s.base + "/oauth/token",
		"registration_endpoint":                          s.base + "/oauth/register",
		"revocation_endpoint":                            s.base + "/oauth/revoke",
		"scopes_supported":                               Scopes,
		"response_types_supported":                       []string{"code"},
		"response_modes_supported":                       []string{"query"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"revocation_endpoint_auth_methods_supported":     []string{"none"},
		"authorization_response_iss_parameter_supported": true,
		"client_id_metadata_document_supported":          false,
	})
}
