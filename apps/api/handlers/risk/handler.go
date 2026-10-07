package risk

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/oplosy/atrisk/apps/api/handlers/httpx"
	application "github.com/oplosy/atrisk/internal/application/risk"
)

type Service interface {
	Submit(context.Context, application.SubmitRequest) (application.Run, error)
	Get(context.Context, string) (application.Run, error)
	Positions(context.Context, string, string, int) (application.Page, error)
	Cancel(context.Context, string, string) (application.Run, error)
}

type Handler struct{ Service Service }

func New(service Service) Handler { return Handler{Service: service} }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Service == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "risk service is unavailable")
		return
	}
	path := strings.TrimPrefix(strings.Trim(r.URL.Path, "/"), "api/v1/")
	path = strings.TrimPrefix(path, "v1/")
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[0] == "risk" && parts[1] == "runs" {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is supported for risk run submission")
			return
		}
		var request application.SubmitRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		if request.IdempotencyKey == "" {
			request.IdempotencyKey = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		}
		if request.IdempotencyKey == "" {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "Idempotency-Key is required")
			return
		}
		if !validUUID(request.AccountID) || !validUUID(request.SnapshotID) || !validUUID(request.ValuationID) || (request.ScenarioID != "" && !validUUID(request.ScenarioID)) {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "account_id, snapshot_id, valuation_id, and scenario_id must be UUIDs")
			return
		}
		result, err := h.Service.Submit(r.Context(), request)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, result)
		return
	}
	if len(parts) >= 3 && parts[0] == "risk" && parts[1] == "runs" {
		id := parts[2]
		if !validUUID(id) {
			writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "run_id must be a UUID")
			return
		}
		if len(parts) == 3 && r.Method == http.MethodGet {
			h.get(w, r, id)
			return
		}
		if len(parts) == 4 && parts[3] == "status" && r.Method == http.MethodGet {
			h.get(w, r, id)
			return
		}
		if len(parts) == 4 && parts[3] == "positions" && r.Method == http.MethodGet {
			limit, cursor, ok := parsePage(r)
			if !ok {
				writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid pagination")
				return
			}
			result, err := h.Service.Positions(r.Context(), id, cursor, limit)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, result)
			return
		}
		if len(parts) == 4 && parts[3] == "cancel" && r.Method == http.MethodPost {
			var body struct {
				Reason string `json:"reason"`
			}
			if r.Body != nil && r.ContentLength > 0 && !decodeJSON(w, r, &body) {
				return
			}
			result, err := h.Service.Cancel(r.Context(), id, body.Reason)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			httpx.WriteJSON(w, http.StatusOK, result)
			return
		}
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "risk route not found")
}

func (h Handler) get(w http.ResponseWriter, r *http.Request, id string) {
	result, err := h.Service.Get(r.Context(), id)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (h Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "risk request is invalid")
	case errors.Is(err, application.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "risk run was not found")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "risk operation failed")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	return httpx.DecodeJSON(w, r, target, 4<<20, func(code, message string) {
		writeError(w, http.StatusBadRequest, code, message)
	})
}

func parsePage(r *http.Request) (int, string, bool) {
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		var err error
		if _, err = fmt.Sscan(value, &limit); err != nil || limit < 1 || limit > 200 {
			return 0, "", false
		}
	}
	cursor := r.URL.Query().Get("cursor")
	return limit, cursor, cursor == "" || len(cursor) <= 512
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:])
	return err == nil
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	httpx.WriteJSON(w, status, map[string]any{"code": code, "message": message})
}
