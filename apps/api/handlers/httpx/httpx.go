// Package httpx holds the JSON helpers shared by the API handlers.
package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"regexp"
)

// RequestIDHeader carries the correlation identifier shared by a request, its
// response, and its log records (ADR-017).
const RequestIDHeader = "X-Request-ID"

// A client identifier is kept only when it cannot break a log line.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// WithRequestID keeps a well-formed client X-Request-ID, replaces anything
// else with a generated one, and returns it on the response.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !requestIDPattern.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r)
	})
}

// RequestID returns the response's correlation identifier, creating one when a
// handler runs without WithRequestID. Call it before the header is written.
func RequestID(w http.ResponseWriter) string {
	id := w.Header().Get(RequestIDHeader)
	if id == "" {
		id = newRequestID()
		w.Header().Set(RequestIDHeader, id)
	}
	return id
}

func newRequestID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	return hex.EncodeToString(raw[:])
}

// WriteJSON writes value as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// DecodeJSON reads exactly one JSON object of at most maxBytes into target and
// rejects unknown fields. On failure it calls fail with an error code and
// message and returns false.
func DecodeJSON(w http.ResponseWriter, r *http.Request, target any, maxBytes int64, fail func(code, message string)) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		fail("INVALID_REQUEST", "request body must be valid JSON")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		fail("INVALID_REQUEST", "request body must contain one JSON object")
		return false
	}
	return true
}

// LogInternalError records the cause behind a generic 500 response so an
// operator can diagnose it; the client never sees err.
func LogInternalError(w http.ResponseWriter, component string, err error) {
	slog.Error("api request failed", "request_id", RequestID(w), "component", component, "error", err)
}
