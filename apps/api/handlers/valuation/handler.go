package valuation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	application "github.com/oplosy/atrisk/internal/application/valuation"
	domain "github.com/oplosy/atrisk/internal/domain/valuation"
)

type Service interface {
	Create(context.Context, domain.Request) (domain.Run, error)
	Get(context.Context, string) (domain.Run, error)
}

type Handler struct{ Service Service }

func New(service Service) Handler { return Handler{Service: service} }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Service == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "valuation service is unavailable")
		return
	}
	path := strings.TrimPrefix(strings.Trim(r.URL.Path, "/"), "api/v1/")
	path = strings.TrimPrefix(path, "v1/")
	if path == "valuations" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is supported for valuations")
			return
		}
		var request domain.Request
		if !decodeJSON(w, r, &request) {
			return
		}
		result, err := h.Service.Create(r.Context(), request)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[0] == "valuations" && r.Method == http.MethodGet {
		result, err := h.Service.Get(r.Context(), parts[1])
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "valuation route not found")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must contain one JSON object")
		return false
	}
	return true
}

func (h Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "valuation request is invalid")
	case errors.Is(err, application.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "valuation resource not found")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "valuation operation failed")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"code": code, "message": message, "request_id": "valuation-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
