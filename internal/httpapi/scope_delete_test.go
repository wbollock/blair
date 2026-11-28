package httpapi

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/wbollock/blair/api"
	"github.com/wbollock/blair/internal/service"
)

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// deleteRoutes lists every DELETE path in the contract with params filled in.
func deleteRoutes(t *testing.T) []string {
	t.Helper()
	doc, err := api.Load()
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for path, item := range doc.Paths.Map() {
		if item.Delete != nil {
			out = append(out, "/api/v1"+pathParam.ReplaceAllString(path, "0198b2a0-0000-7000-8000-000000000000"))
		}
	}
	sort.Strings(out)
	if len(out) < 8 {
		t.Fatalf("expected the contract to list the DELETE routes, got %v", out)
	}
	return out
}

// A token with every scope except tasks:delete and notes:write must be refused on every
// DELETE route. (Notes are soft-deleted under notes:write and are out of scope for ADR-0015.)
func TestDeleteRoutesNeedDeleteScope(t *testing.T) {
	h := newHarness(t, service.Options{Registration: "closed"})
	b := h.signedInAdmin()
	all := []string{}
	for _, s := range service.Scopes {
		if s != "tasks:delete" && s != "notes:write" {
			all = append(all, s)
		}
	}
	nodel := h.token(b.do("POST", "/api/v1/me/tokens", map[string]any{"name": "no-delete", "scopes": all}).str("token"))
	for _, p := range deleteRoutes(t) {
		expect(t, nodel.do("DELETE", p, nil), 403)
	}

	// With the scope the task routes reach their handlers (the id does not exist).
	del := h.token(b.do("POST", "/api/v1/me/tokens", map[string]any{"name": "deleter", "scopes": []string{"tasks:delete"}}).str("token"))
	for _, p := range deleteRoutes(t) {
		if strings.Contains(p, "/projects/") || strings.Contains(p, "/columns/") || strings.Contains(p, "/tasks/") ||
			strings.Contains(p, "/checklist/") || strings.Contains(p, "/labels/") {
			if r := del.do("DELETE", p, nil); r.status == 403 {
				t.Fatalf("DELETE %s refused a tasks:delete token: %s", p, r.raw)
			}
		}
	}
}

func TestBatchDeleteNeedsDeleteScope(t *testing.T) {
	h := newHarness(t, service.Options{Registration: "closed"})
	b := h.signedInAdmin()
	tk := b.do("POST", "/api/v1/tasks", map[string]any{"title": "keep me"})
	expect(t, tk, 201)
	ops := map[string]any{"ops": []any{map[string]any{"op": "delete", "type": "task", "id": tk.str("id")}}}

	w := h.token(b.do("POST", "/api/v1/me/tokens", map[string]any{"name": "writer", "scopes": []string{"tasks:read", "tasks:write"}}).str("token"))
	expect(t, w.do("POST", "/api/v1/batch", ops), 403)
	expect(t, w.do("GET", "/api/v1/tasks/"+tk.str("id"), nil), 200)

	d := h.token(b.do("POST", "/api/v1/me/tokens", map[string]any{"name": "deleter", "scopes": []string{"tasks:read", "tasks:write", "tasks:delete"}}).str("token"))
	expect(t, d.do("POST", "/api/v1/batch", ops), 200)
}
