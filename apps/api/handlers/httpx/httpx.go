// Package httpx holds the JSON helpers shared by the API handlers.
package httpx

import (
	"encoding/json"
	"io"
	"net/http"
)

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
