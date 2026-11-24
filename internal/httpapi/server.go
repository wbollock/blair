// Package httpapi serves the JSON API (/api/v1), its OpenAPI docs and the web app.
package httpapi

import (
	"context"
	_ "embed"
	"io/fs"
	"net/http"
	"net/netip"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/wbollock/blair/internal/service"
	"github.com/wbollock/blair/internal/store"
)

// Config is what the HTTP layer needs from the app config.
type Config struct {
	Secure         bool // https base URL (or localhost): __Host- cookies, HSTS when https
	HSTS           bool
	TrustedProxies []netip.Prefix
	MetricsHandler http.Handler
	MetricsToken   string
	Version        string
	Web            fs.FS // built SPA; nil serves a placeholder
	OpenAPIYAML    []byte
	OpenAPIJSON    []byte
}

type API struct {
	svc        *service.Service
	db         *store.DB
	cfg        Config
	cookieName string
	preName    string

	loginIPLimit    *limiter
	loginEmailLimit *limiter
	apiLimit        *limiter
}

// New builds the full HTTP handler.
func New(svc *service.Service, db *store.DB, cfg Config) http.Handler {
	a := &API{
		svc: svc, db: db, cfg: cfg,
		cookieName:      "blair_session",
		preName:         "blair_preauth",
		loginIPLimit:    newLimiter(time.Minute, 5),
		loginEmailLimit: newLimiter(time.Hour, 10),
		apiLimit:        newLimiter(time.Minute, 600),
	}
	if cfg.Secure {
		a.cookieName, a.preName = "__Host-blair_session", "__Host-blair_preauth"
	}

	r := chi.NewRouter()
	r.Use(realIP(cfg.TrustedProxies), securityHeaders(cfg.HSTS), observe)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	r.Get("/readyz", a.readyz)
	if cfg.MetricsHandler != nil {
		r.Get("/metrics", a.metrics)
	}
	r.Get("/api/openapi.yaml", a.openapi("application/yaml", cfg.OpenAPIYAML))
	r.Get("/api/openapi.json", a.openapi("application/json", cfg.OpenAPIJSON))
	r.Get("/api/docs", a.docs)

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(noStore, bodyLimit(1<<20))
		r.Use(http.NewCrossOriginProtection().Handler, csrf, a.authenticate)

		r.Get("/auth/status", a.authStatus)
		r.Post("/auth/setup", a.setup)
		r.Post("/auth/login", a.login)
		r.Post("/auth/login/mfa", a.loginMFA)
		r.Post("/auth/register", a.register)

		r.Group(func(r chi.Router) {
			r.Use(a.requireAuth)
			r.Post("/auth/logout", a.logout)

			r.With(scope("account:read")).Get("/me", a.getMe)
			r.Group(func(r chi.Router) {
				r.Use(sessionOnly)
				r.Patch("/me", a.patchMe)
				r.Delete("/me", a.deleteMe)
				r.Post("/me/password", a.changePassword)
				r.Post("/me/2fa/totp", a.beginTOTP)
				r.Post("/me/2fa/totp/confirm", a.confirmTOTP)
				r.Delete("/me/2fa/totp", a.disableTOTP)
				r.Post("/me/2fa/recovery-codes", a.regenRecovery)
				r.Get("/me/sessions", a.listSessions)
				r.Delete("/me/sessions", a.revokeOtherSessions)
				r.Delete("/me/sessions/{id}", a.deleteSession)
				r.Get("/me/tokens", a.listTokens)
				r.Post("/me/tokens", a.createToken)
				r.Delete("/me/tokens/{id}", a.deleteToken)
				r.Get("/me/audit", a.listAudit)
			})

			read, write := scope("tasks:read"), scope("tasks:write")
			r.With(read).Get("/projects", a.listProjects)
			r.With(write).Post("/projects", a.createProject)
			r.With(read).Get("/projects/{id}", a.getProject)
			r.With(write).Patch("/projects/{id}", a.patchProject)
			r.With(write).Delete("/projects/{id}", a.deleteProject)
			r.With(write).Post("/projects/{id}/columns", a.createColumn)
			r.With(write).Patch("/columns/{id}", a.patchColumn)
			r.With(write).Delete("/columns/{id}", a.deleteColumn)

			r.With(read).Get("/tasks", a.listTasks)
			r.With(write).Post("/tasks", a.createTask)
			r.With(read).Get("/tasks/{id}", a.getTask)
			r.With(write).Patch("/tasks/{id}", a.patchTask)
			r.With(write).Delete("/tasks/{id}", a.deleteTask)
			r.With(write).Post("/tasks/{id}/move", a.moveTask)
			r.With(write).Post("/tasks/{id}/restore", a.restoreTask)
			r.With(read).Get("/tasks/{id}/checklist", a.listChecklist)
			r.With(write).Post("/tasks/{id}/checklist", a.createChecklist)
			r.With(write).Patch("/checklist/{id}", a.patchChecklist)
			r.With(write).Delete("/checklist/{id}", a.deleteChecklist)

			r.With(read).Get("/labels", a.listLabels)
			r.With(write).Post("/labels", a.createLabel)
			r.With(write).Patch("/labels/{id}", a.patchLabel)
			r.With(write).Delete("/labels/{id}", a.deleteLabel)

			nread, nwrite := scope("notes:read"), scope("notes:write")
			r.With(nread).Get("/notes", a.listNotes)
			r.With(nwrite).Post("/notes", a.createNote)
			r.With(nread).Get("/notes/{id}", a.getNote)
			r.With(nwrite).Patch("/notes/{id}", a.patchNote)
			r.With(nwrite).Delete("/notes/{id}", a.deleteNote)
			r.With(nwrite).Post("/notes/{id}/restore", a.restoreNote)
			r.With(nwrite, write).Post("/notes/{id}/convert", a.convertNote)

			r.With(read).Get("/search", a.search)
			r.With(read).Post("/quick-add/parse", a.parseQuickAdd)
			r.With(write, bodyLimit(20<<20)).Post("/batch", a.batch)
			r.With(scope("export")).Get("/export", a.export)

			r.Group(func(r chi.Router) {
				r.Use(adminOnly)
				r.Get("/admin/users", a.adminListUsers)
				r.Post("/admin/users", a.adminCreateUser)
				r.Patch("/admin/users/{id}", a.adminPatchUser)
				r.Delete("/admin/users/{id}", a.adminDeleteUser)
				r.Get("/admin/invites", a.adminListInvites)
				r.Post("/admin/invites", a.adminCreateInvite)
				r.Delete("/admin/invites/{id}", a.adminDeleteInvite)
			})
		})
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			writeProblem(w, r, http.StatusNotFound, "not_found", "Not found", "no such API route")
		})
		r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
			writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", "")
		})
	})

	r.Handle("/*", newSPA(cfg.Web))

	return otelhttp.NewHandler(r, "http.server", otelhttp.WithFilter(func(r *http.Request) bool {
		return r.URL.Path != "/healthz" && r.URL.Path != "/readyz"
	}))
}

func (a *API) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.db.R.PingContext(ctx); err != nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "unavailable", "Database unavailable", "")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ready", "version": a.cfg.Version})
}

func (a *API) metrics(w http.ResponseWriter, r *http.Request) {
	if a.cfg.MetricsToken != "" && r.Header.Get("Authorization") != "Bearer "+a.cfg.MetricsToken {
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "")
		return
	}
	a.cfg.MetricsHandler.ServeHTTP(w, r)
}

func (a *API) openapi(ct string, body []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	}
}

//go:embed docs.html
var docsHTML []byte

func (a *API) docs(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	_, _ = w.Write(docsHTML)
}
