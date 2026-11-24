package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/wbollock/blair/internal/service"
)

// Problem is an RFC 9457 problem document.
type Problem struct {
	Type     string               `json:"type"`
	Title    string               `json:"title"`
	Status   int                  `json:"status"`
	Detail   string               `json:"detail,omitempty"`
	Instance string               `json:"instance,omitempty"`
	Errors   []service.FieldError `json:"errors,omitempty"`
	Current  any                  `json:"current,omitempty"`
}

func problemType(code string) string { return "urn:blair:problem:" + code }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{Type: problemType(code), Title: title, Status: status, Detail: detail, Instance: r.URL.Path})
}

// writeError maps service errors to problem responses. Internal details never leak.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *service.ValidationError
	var ce *service.ConflictError
	var pe *service.PreconditionError
	var be *bodyError
	switch {
	case errors.As(err, &be):
		writeProblem(w, r, be.status, be.code, be.title, be.detail)
	case errors.As(err, &ve):
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(Problem{Type: problemType("validation"), Title: "Validation failed", Status: 422,
			Detail: ve.Error(), Instance: r.URL.Path, Errors: ve.Fields})
	case errors.As(err, &ce):
		writeProblem(w, r, http.StatusConflict, "conflict", "Conflict", ce.Msg)
	case errors.As(err, &pe):
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusPreconditionFailed)
		_ = json.NewEncoder(w).Encode(Problem{Type: problemType("precondition_failed"), Title: "Version mismatch", Status: 412,
			Detail: "the resource changed since you loaded it", Instance: r.URL.Path, Current: pe.Current})
	case errors.Is(err, service.ErrNotFound):
		writeProblem(w, r, http.StatusNotFound, "not_found", "Not found", "")
	case errors.Is(err, service.ErrUnauthorized):
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "")
	case errors.Is(err, service.ErrForbidden):
		writeProblem(w, r, http.StatusForbidden, "forbidden", "Forbidden", "")
	default:
		slog.ErrorContext(r.Context(), "request failed", "route", routePattern(r), "err", err)
		writeProblem(w, r, http.StatusInternalServerError, "internal", "Internal server error", "")
	}
}

type bodyError struct {
	status              int
	code, title, detail string
}

func (e *bodyError) Error() string { return e.detail }

// decode reads a strict JSON body (unknown fields rejected).
func decode(r *http.Request, v any) error {
	ct := r.Header.Get("Content-Type")
	if ct != "" && !strings.HasPrefix(ct, "application/json") {
		return &bodyError{http.StatusUnsupportedMediaType, "unsupported_media_type", "Unsupported media type", "send application/json"}
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return &bodyError{http.StatusRequestEntityTooLarge, "too_large", "Request body too large", fmt.Sprintf("limit is %d bytes", mbe.Limit)}
		}
		return &bodyError{http.StatusBadRequest, "bad_request", "Could not read body", ""}
	}
	if len(bytes.TrimSpace(body)) == 0 {
		body = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return &bodyError{http.StatusBadRequest, "invalid_json", "Invalid JSON", err.Error()}
	}
	if dec.More() {
		return &bodyError{http.StatusBadRequest, "invalid_json", "Invalid JSON", "trailing data after JSON value"}
	}
	return nil
}

// setETag writes a weak version ETag.
func setETag(w http.ResponseWriter, version int) {
	w.Header().Set("ETag", `"v`+strconv.Itoa(version)+`"`)
}

// ifMatch parses If-Match: "v3" -> 3. Absent or "*" -> 0 (no check).
func ifMatch(r *http.Request) (int, error) {
	h := strings.TrimSpace(r.Header.Get("If-Match"))
	if h == "" || h == "*" {
		return 0, nil
	}
	h = strings.TrimPrefix(h, "W/")
	h = strings.Trim(h, `"`)
	n, err := strconv.Atoi(strings.TrimPrefix(h, "v"))
	if err != nil || n < 1 {
		return 0, &bodyError{http.StatusBadRequest, "bad_request", "Invalid If-Match", `use the ETag value, e.g. "v3"`}
	}
	return n, nil
}
