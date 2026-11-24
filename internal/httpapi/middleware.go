package httpapi

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"

	"github.com/wbollock/blair/internal/service"
)

type ctxKey int

const (
	keyUser ctxKey = iota
	keySession
	keyScopes
	keyClientIP
)

func userFrom(ctx context.Context) *service.User {
	u, _ := ctx.Value(keyUser).(*service.User)
	return u
}

func sessionFrom(ctx context.Context) *service.Session {
	s, _ := ctx.Value(keySession).(*service.Session)
	return s
}

func clientIP(ctx context.Context) string {
	ip, _ := ctx.Value(keyClientIP).(string)
	return ip
}

func clientInfo(r *http.Request) service.ClientInfo {
	return service.ClientInfo{IP: clientIP(r.Context()), UserAgent: r.UserAgent()}
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
		return rc.RoutePattern()
	}
	return "unmatched"
}

// realIP trusts X-Forwarded-For only from configured proxies (SPEC §9.6).
func realIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	isTrusted := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a.Unmap()) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			ip := host
			if peer, err := netip.ParseAddr(host); err == nil && isTrusted(peer) {
				// Walk right-to-left, skipping trusted proxies.
				hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
				for i := len(hops) - 1; i >= 0; i-- {
					a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
					if err != nil {
						break
					}
					ip = a.Unmap().String()
					if !isTrusted(a) {
						break
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyClientIP, ip)))
		})
	}
}

// securityHeaders sets the baseline headers for every response (SPEC §9.7).
func securityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "same-origin")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}
			if h.Get("Content-Security-Policy") == "" {
				h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; base-uri 'none'")
			}
			next.ServeHTTP(w, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// observe names spans by route, logs requests (no query strings or bodies), and recovers panics.
func observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		defer func() {
			if v := recover(); v != nil {
				slog.ErrorContext(r.Context(), "panic", "panic", v, "stack", string(debug.Stack()))
				writeProblem(rec, r, http.StatusInternalServerError, "internal", "Internal server error", "")
			}
			route := routePattern(r)
			sp := trace.SpanFromContext(r.Context())
			sp.SetName(r.Method + " " + route)
			sp.SetAttributes(attribute.String("http.route", route))
			if u := userFrom(r.Context()); u != nil {
				sp.SetAttributes(attribute.String("enduser.id", u.ID))
			}
			if route == "/healthz" || route == "/readyz" {
				return
			}
			slog.InfoContext(r.Context(), "http request", "method", r.Method, "route", route, "status", rec.status,
				"duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(rec, r)
	})
}

func bodyLimit(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, n)
			next.ServeHTTP(w, r)
		})
	}
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// ---- rate limiting ----

type limiter struct {
	mu      sync.Mutex
	every   rate.Limit
	burst   int
	buckets map[string]*bucket
}

type bucket struct {
	l    *rate.Limiter
	seen time.Time
}

func newLimiter(per time.Duration, n int) *limiter {
	l := &limiter{every: rate.Every(per / time.Duration(n)), burst: n, buckets: map[string]*bucket{}}
	go func() {
		for range time.Tick(10 * time.Minute) {
			l.mu.Lock()
			for k, b := range l.buckets {
				if time.Since(b.seen) > time.Hour {
					delete(l.buckets, k)
				}
			}
			l.mu.Unlock()
		}
	}()
	return l
}

// allow consumes a token for key and returns the wait until the next one when refused.
func (l *limiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{l: rate.NewLimiter(l.every, l.burst)}
		l.buckets[key] = b
	}
	b.seen = time.Now()
	res := b.l.Reserve()
	if d := res.Delay(); d > 0 {
		res.Cancel()
		return false, d
	}
	return true, 0
}

func tooMany(w http.ResponseWriter, r *http.Request, wait time.Duration) {
	secs := int(wait.Seconds()) + 1
	w.Header().Set("Retry-After", itoa(secs))
	writeProblem(w, r, http.StatusTooManyRequests, "rate_limited", "Too many requests", "try again in "+itoa(secs)+" seconds")
}

func itoa(n int) string { return strconv.Itoa(n) }

// ---- auth ----

// authenticate resolves a bearer token or session cookie. It does not reject anonymous requests.
func (a *API) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if h := r.Header.Get("Authorization"); h != "" {
			tok, ok := strings.CutPrefix(h, "Bearer ")
			if !ok {
				writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "use Authorization: Bearer <token>")
				return
			}
			u, scopes, err := a.svc.LookupToken(ctx, strings.TrimSpace(tok))
			if err != nil {
				writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "invalid or expired token")
				return
			}
			ctx = context.WithValue(ctx, keyUser, u)
			ctx = context.WithValue(ctx, keyScopes, scopes)
		} else if c, err := r.Cookie(a.cookieName); err == nil {
			if sess, u, err := a.svc.LookupSession(ctx, c.Value); err == nil && !sess.MFAPending {
				ctx = context.WithValue(ctx, keyUser, u)
				ctx = context.WithValue(ctx, keySession, sess)
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// csrf requires the custom header on cookie-authenticated unsafe requests.
// Cross-origin requests are already rejected by http.CrossOriginProtection.
func csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
		default:
			if r.Header.Get("Authorization") == "" && r.Header.Get("X-Requested-With") != "blair" {
				writeProblem(w, r, http.StatusForbidden, "csrf", "Missing CSRF header", "send X-Requested-With: blair")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// requireAuth rejects anonymous requests and enforces account gates
// (forced password change, mandatory 2FA) and the API rate limit.
func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFrom(r.Context())
		if u == nil {
			writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "sign in or send a bearer token")
			return
		}
		if ok, wait := a.apiLimit.allow(u.ID); !ok {
			tooMany(w, r, wait)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v1")
		gateExempt := path == "/me" || strings.HasPrefix(path, "/me/2fa") || path == "/me/password" || path == "/auth/logout"
		if u.MustChangePassword && !gateExempt {
			writeProblem(w, r, http.StatusForbidden, "password_change_required", "Password change required", "change your password at POST /api/v1/me/password")
			return
		}
		if a.svc.Options().Require2FA && !u.TOTPEnabled && !gateExempt {
			writeProblem(w, r, http.StatusForbidden, "2fa_required", "Two-factor authentication required", "enable 2FA at POST /api/v1/me/2fa/totp")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hasScope reports whether the request may use a PAT scope. Browser sessions have every scope.
func hasScope(r *http.Request, name string) bool {
	if sessionFrom(r.Context()) != nil {
		return true
	}
	have, _ := r.Context().Value(keyScopes).([]string)
	return slices.Contains(have, name)
}

// scope requires PAT scopes.
func scope(names ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, n := range names {
				if !hasScope(r, n) {
					writeProblem(w, r, http.StatusForbidden, "insufficient_scope", "Insufficient scope", "this token needs the "+n+" scope")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// sessionOnly blocks API tokens (account security endpoints).
func sessionOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessionFrom(r.Context()) == nil {
			writeProblem(w, r, http.StatusForbidden, "session_required", "Browser session required", "API tokens cannot use this endpoint")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// adminOnly requires an admin browser session (never a token).
func adminOnly(next http.Handler) http.Handler {
	return sessionOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := userFrom(r.Context()); u == nil || !u.IsAdmin {
			writeProblem(w, r, http.StatusForbidden, "forbidden", "Admins only", "")
			return
		}
		next.ServeHTTP(w, r)
	}))
}
