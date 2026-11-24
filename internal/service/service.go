// Package service holds Blair's business rules: ownership, the column/status
// invariants, quick-add, batch upserts and auth flows. Handlers never
// reimplement these.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wbollock/blair/internal/auth"
	"github.com/wbollock/blair/internal/store"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/wbollock/blair/internal/service")

func span(ctx context.Context, name string) (context.Context, trace.Span) {
	return tracer.Start(ctx, "service."+name)
}

// Options configures account policy.
type Options struct {
	Registration string // closed | invite | open
	Require2FA   bool
	Issuer       string // TOTP issuer label
}

type Service struct {
	db    *store.DB
	box   *auth.SecretBox
	opts  Options
	clock func() time.Time

	setupMu    sync.Mutex
	setupToken string
	setupUntil time.Time
}

func New(db *store.DB, box *auth.SecretBox, opts Options) *Service {
	if opts.Issuer == "" {
		opts.Issuer = "Blair"
	}
	return &Service{db: db, box: box, opts: opts, clock: time.Now}
}

// SetClock overrides the clock (tests).
func (s *Service) SetClock(f func() time.Time) { s.clock = f }

func (s *Service) now() string { return store.FormatTime(s.clock()) }

// Options returns the account policy.
func (s *Service) Options() Options { return s.opts }

// querier is satisfied by *sql.DB and *sql.Tx so rules run inside batch transactions.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func newID() string { return uuid.Must(uuid.NewV7()).String() }

func validID(id string) bool {
	_, err := uuid.Parse(id)
	return err == nil && len(id) == 36
}

// ---- errors ----

var (
	ErrNotFound     = errors.New("not found")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
)

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ValidationError struct{ Fields []FieldError }

func (e *ValidationError) Error() string {
	var parts []string
	for _, f := range e.Fields {
		parts = append(parts, f.Field+": "+f.Message)
	}
	return "validation failed: " + strings.Join(parts, "; ")
}

func invalid(field, code, msg string) error {
	return &ValidationError{Fields: []FieldError{{Field: field, Code: code, Message: msg}}}
}

// ConflictError is a 409.
type ConflictError struct{ Msg string }

func (e *ConflictError) Error() string { return e.Msg }

func conflict(format string, a ...any) error { return &ConflictError{Msg: fmt.Sprintf(format, a...)} }

// PreconditionError is a 412: the If-Match version is stale.
type PreconditionError struct{ Current any }

func (e *PreconditionError) Error() string { return "version mismatch" }

// ---- small helpers ----

func nullStr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func strPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func intPtr(ni sql.NullInt64) *int {
	if !ni.Valid {
		return nil
	}
	v := int(ni.Int64)
	return &v
}

func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

// Opt is a JSON Merge Patch field: Set when present, Null when explicitly null.
type Opt[T any] struct {
	Set  bool
	Null bool
	V    T
}

func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.V)
}

// Some builds a set, non-null Opt (tests and internal callers).
func Some[T any](v T) Opt[T] { return Opt[T]{Set: true, V: v} }

// Statuses in canonical order.
var Statuses = []string{"todo", "doing", "waiting", "done", "cancelled"}

func validStatus(s string) bool {
	for _, v := range Statuses {
		if v == s {
			return true
		}
	}
	return false
}

func closedStatus(s string) bool { return s == "done" || s == "cancelled" }

func validDate(s string) bool {
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

func validClock(s string) bool {
	_, err := time.Parse("15:04", s)
	return err == nil && len(s) == 5
}

func validTimestamp(s string) (string, bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return "", false
	}
	return store.FormatTime(t), true
}
