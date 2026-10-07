package journal

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oplosy/atrisk/apps/api/handlers/httpx"
	"github.com/oplosy/atrisk/internal/application/evidence"
	application "github.com/oplosy/atrisk/internal/application/journal"
	domain "github.com/oplosy/atrisk/internal/domain/journal"
)

type Service interface {
	Create(context.Context, domain.CreateRequest) (domain.Decision, error)
	Get(context.Context, string) (domain.Decision, error)
	Finalize(context.Context, string) (domain.Decision, error)
	Evidence(context.Context, string) (evidence.Sealed, error)
	AddReview(context.Context, string, domain.ReviewRequest) (domain.Review, error)
	AddAmendment(context.Context, string, domain.AmendmentRequest) (domain.Amendment, error)
	Timeline(context.Context, string) (domain.Timeline, error)
}

type Handler struct{ Service Service }

func New(service Service) Handler { return Handler{Service: service} }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Service == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "decision journal service is unavailable")
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	path = strings.TrimPrefix(path, "api/v1/")
	path = strings.TrimPrefix(path, "v1/")
	parts := strings.Split(path, "/")
	if path == "decisions" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is supported for decisions")
			return
		}
		var request domain.CreateRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		result, err := h.Service.Create(r.Context(), request)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, result)
		return
	}
	if len(parts) == 2 && parts[0] == "decisions" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is supported for a decision")
			return
		}
		result, err := h.Service.Get(r.Context(), parts[1])
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
		return
	}
	if len(parts) == 3 && parts[0] == "decisions" {
		decisionID := parts[1]
		switch parts[2] {
		case "finalize":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is supported for finalization")
				return
			}
			result, err := h.Service.Finalize(r.Context(), decisionID)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, result)
		case "evidence":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is supported for decision evidence")
				return
			}
			result, err := h.Service.Evidence(r.Context(), decisionID)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, result)
		case "reviews":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is supported for reviews")
				return
			}
			var request domain.ReviewRequest
			if !decodeJSON(w, r, &request) {
				return
			}
			result, err := h.Service.AddReview(r.Context(), decisionID, request)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			httpx.WriteJSON(w, http.StatusCreated, result)
		case "amendments":
			if r.Method != http.MethodPost {
				writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is supported for amendments")
				return
			}
			var request domain.AmendmentRequest
			if !decodeJSON(w, r, &request) {
				return
			}
			result, err := h.Service.AddAmendment(r.Context(), decisionID, request)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			httpx.WriteJSON(w, http.StatusCreated, result)
		case "timeline":
			if r.Method != http.MethodGet {
				writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is supported for the decision timeline")
				return
			}
			result, err := h.Service.Timeline(r.Context(), decisionID)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, result)
		default:
			writeError(w, http.StatusNotFound, "NOT_FOUND", "decision journal route not found")
		}
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "decision journal route not found")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	return httpx.DecodeJSON(w, r, target, 2<<20, func(code, message string) {
		writeError(w, http.StatusBadRequest, code, message)
	})
}

func (h Handler) writeServiceError(w http.ResponseWriter, err error) {
	var conflict *application.ConflictError
	switch {
	case errors.Is(err, application.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "decision journal request is invalid")
	case errors.Is(err, application.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "decision journal resource not found")
	case errors.As(err, &conflict):
		writeError(w, http.StatusConflict, conflict.Code, conflict.Message)
	case errors.Is(err, evidence.ErrIntegrity):
		writeError(w, http.StatusConflict, "EVIDENCE_INTEGRITY_FAILURE", "sealed evidence failed integrity verification")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "decision journal operation failed")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	httpx.WriteJSON(w, status, map[string]any{"code": code, "message": message, "request_id": "journal-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)})
}
