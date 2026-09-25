package portfolio

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	application "github.com/oplosy/atrisk/internal/application/portfolio"
	domain "github.com/oplosy/atrisk/internal/domain/portfolio"
)

type Service interface {
	CreateInstrument(context.Context, application.CreateInstrumentRequest) (domain.Instrument, error)
	ListInstruments(context.Context) ([]domain.Instrument, error)
	GetInstrument(context.Context, string) (domain.Instrument, error)
	UpdateInstrumentStatus(context.Context, string, string) (domain.Instrument, error)
	CreatePortfolio(context.Context, application.CreatePortfolioRequest) (domain.Portfolio, error)
	ListPortfolios(context.Context) ([]domain.Portfolio, error)
	GetPortfolio(context.Context, string) (domain.Portfolio, error)
	UpdatePortfolio(context.Context, string, application.UpdatePortfolioRequest) (domain.Portfolio, error)
	DeletePortfolio(context.Context, string) error
	CreateAccount(context.Context, string, application.CreateAccountRequest) (domain.Account, error)
	ListAccounts(context.Context, string) ([]domain.Account, error)
	GetAccount(context.Context, string) (domain.Account, error)
	UpdateAccount(context.Context, string, application.UpdateAccountRequest) (domain.Account, error)
	DeleteAccount(context.Context, string) error
	CreateSnapshot(context.Context, string, application.CreateSnapshotRequest) (domain.Snapshot, error)
	ListSnapshots(context.Context, string) ([]domain.Snapshot, error)
	GetSnapshot(context.Context, string) (domain.Snapshot, error)
}

type Handler struct{ Service Service }

func New(service Service) Handler { return Handler{Service: service} }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Service == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "portfolio service is unavailable")
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	path = strings.TrimPrefix(path, "api/v1/")
	path = strings.TrimPrefix(path, "v1/")
	parts := strings.Split(path, "/")
	if path == "instruments" {
		switch r.Method {
		case http.MethodGet:
			items, err := h.Service.ListInstruments(r.Context())
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": items})
		case http.MethodPost:
			var request application.CreateInstrumentRequest
			if !decodeJSON(w, r, &request) {
				return
			}
			item, err := h.Service.CreateInstrument(r.Context(), request)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, item)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not supported for instruments")
		}
		return
	}
	if path == "portfolios" {
		switch r.Method {
		case http.MethodGet:
			items, err := h.Service.ListPortfolios(r.Context())
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": items})
		case http.MethodPost:
			var request application.CreatePortfolioRequest
			if !decodeJSON(w, r, &request) {
				return
			}
			item, err := h.Service.CreatePortfolio(r.Context(), request)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, item)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method is not supported for portfolios")
		}
		return
	}
	if path == "accounts" || path == "snapshots" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "portfolio route not found")
		return
	}
	if len(parts) >= 2 && parts[0] == "instruments" {
		if len(parts) == 2 && r.Method == http.MethodGet {
			item, err := h.Service.GetInstrument(r.Context(), parts[1])
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
			return
		}
		if len(parts) == 3 && parts[2] == "status" && r.Method == http.MethodPatch {
			var request struct {
				Status string `json:"status"`
			}
			if !decodeJSON(w, r, &request) {
				return
			}
			item, err := h.Service.UpdateInstrumentStatus(r.Context(), parts[1], request.Status)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
			return
		}
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "instrument identity is immutable; only status may change")
		return
	}
	if len(parts) >= 2 && parts[0] == "portfolios" {
		portfolioID := parts[1]
		if len(parts) == 2 {
			switch r.Method {
			case http.MethodGet:
				item, err := h.Service.GetPortfolio(r.Context(), portfolioID)
				if err != nil {
					h.writeServiceError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, item)
			case http.MethodPatch:
				var request application.UpdatePortfolioRequest
				if !decodeJSON(w, r, &request) {
					return
				}
				item, err := h.Service.UpdatePortfolio(r.Context(), portfolioID, request)
				if err != nil {
					h.writeServiceError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, item)
			case http.MethodDelete:
				err := h.Service.DeletePortfolio(r.Context(), portfolioID)
				if err != nil {
					h.writeServiceError(w, err)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			default:
				writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported portfolio method")
			}
			return
		}
		if len(parts) == 3 && (parts[2] == "accounts" || parts[2] == "snapshots") {
			resource := parts[2]
			switch r.Method {
			case http.MethodGet:
				if resource == "accounts" {
					items, err := h.Service.ListAccounts(r.Context(), portfolioID)
					if err != nil {
						h.writeServiceError(w, err)
						return
					}
					writeJSON(w, http.StatusOK, map[string]any{"items": items})
					return
				}
				items, err := h.Service.ListSnapshots(r.Context(), portfolioID)
				if err != nil {
					h.writeServiceError(w, err)
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{"items": items})
			case http.MethodPost:
				if resource == "accounts" {
					var request application.CreateAccountRequest
					if !decodeJSON(w, r, &request) {
						return
					}
					item, err := h.Service.CreateAccount(r.Context(), portfolioID, request)
					if err != nil {
						h.writeServiceError(w, err)
						return
					}
					writeJSON(w, http.StatusCreated, item)
					return
				}
				var request application.CreateSnapshotRequest
				if !decodeJSON(w, r, &request) {
					return
				}
				item, err := h.Service.CreateSnapshot(r.Context(), portfolioID, request)
				if err != nil {
					h.writeServiceError(w, err)
					return
				}
				writeJSON(w, http.StatusCreated, item)
			default:
				writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported portfolio resource method")
			}
			return
		}
	}
	if len(parts) >= 2 && parts[0] == "accounts" {
		if len(parts) != 2 {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "account route not found")
			return
		}
		switch r.Method {
		case http.MethodGet:
			item, err := h.Service.GetAccount(r.Context(), parts[1])
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodPatch:
			var request application.UpdateAccountRequest
			if !decodeJSON(w, r, &request) {
				return
			}
			item, err := h.Service.UpdateAccount(r.Context(), parts[1], request)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, item)
		case http.MethodDelete:
			if err := h.Service.DeleteAccount(r.Context(), parts[1]); err != nil {
				h.writeServiceError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "unsupported account method")
		}
		return
	}
	if len(parts) >= 2 && parts[0] == "snapshots" {
		if len(parts) != 2 || r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "snapshots are immutable and read-only")
			return
		}
		item, err := h.Service.GetSnapshot(r.Context(), parts[1])
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
		return
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND", "portfolio route not found")
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
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "portfolio request is invalid")
	case errors.Is(err, application.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "portfolio resource not found")
	case errors.Is(err, application.ErrConflict):
		writeError(w, http.StatusConflict, "CONFLICT", "portfolio resource conflicts with an existing immutable or referenced resource")
	case errors.Is(err, application.ErrDatabase):
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "portfolio database is unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "portfolio operation failed")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"code": code, "message": message, "request_id": "portfolio-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
