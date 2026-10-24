package reconciliation

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oplosy/atrisk/apps/api/handlers/httpx"
	application "github.com/oplosy/atrisk/internal/application/reconciliation"
	domain "github.com/oplosy/atrisk/internal/domain/reconciliation"
)

type Service interface {
	CreateTolerance(context.Context, string, domain.ToleranceRequest) (domain.ToleranceVersion, error)
	Create(context.Context, string, domain.Request) (domain.Checkpoint, error)
	Get(context.Context, string) (domain.Checkpoint, error)
}

type Handler struct{ Service Service }

func New(service Service) Handler { return Handler{Service: service} }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Service == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "reconciliation service is unavailable")
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	path = strings.TrimPrefix(path, "api/v1/")
	path = strings.TrimPrefix(path, "v1/")
	parts := strings.Split(path, "/")
	if len(parts) == 3 && parts[0] == "accounts" && parts[2] == "reconciliation-tolerances" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is supported for reconciliation tolerances")
			return
		}
		var request domain.ToleranceRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		result, err := h.Service.CreateTolerance(r.Context(), parts[1], request)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, result)
		return
	}
	if len(parts) == 3 && parts[0] == "valuations" && parts[2] == "reconciliations" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is supported for reconciliations")
			return
		}
		var request domain.Request
		if !decodeJSON(w, r, &request) {
			return
		}
		result, err := h.Service.Create(r.Context(), parts[1], request)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, result)
		return
	}
	if len(parts) == 2 && parts[0] == "reconciliations" && r.Method == http.MethodGet {
		result, err := h.Service.Get(r.Context(), parts[1])
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "reconciliation route not found")
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
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "reconciliation request is invalid")
	case errors.Is(err, application.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "reconciliation resource not found")
	case errors.As(err, &conflict):
		writeError(w, http.StatusConflict, conflict.Code, conflict.Message)
	case errors.Is(err, application.ErrDatabase):
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "reconciliation database is unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "reconciliation operation failed")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	httpx.WriteJSON(w, status, map[string]any{"code": code, "message": message, "request_id": "reconciliation-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)})
}
