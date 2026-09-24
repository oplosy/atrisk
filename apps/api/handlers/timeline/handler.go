package timeline

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	application "github.com/oplosy/atrisk/internal/application/timeline"
)

type Handler struct{ Service application.Service }

func New(service application.Service) Handler { return Handler{Service: service} }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is supported")
		return
	}
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/api/v1/")
	path = strings.TrimPrefix(path, "/v1/")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if path == "series" {
		limit := parseInt(r.URL.Query().Get("limit"), 50)
		page, err := h.Service.ListSeries(r.Context(), limit, r.URL.Query().Get("cursor"))
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
		return
	}
	if path == "timeline" {
		ids := strings.Split(r.URL.Query().Get("series_id"), ",")
		query, err := parseObservationRequest(r, "observations")
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_QUERY", err.Error())
			return
		}
		page, err := h.Service.CrossSource(r.Context(), ids, query)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
		return
	}
	if len(parts) < 2 || parts[0] != "series" {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "timeline route not found")
		return
	}
	seriesID := parts[1]
	if len(parts) == 2 {
		series, err := h.Service.GetSeries(r.Context(), seriesID)
		if err != nil {
			h.writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, series)
		return
	}
	if len(parts) != 3 || (parts[2] != "observations" && parts[2] != "revisions") {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "timeline route not found")
		return
	}
	query, err := parseObservationRequest(r, parts[2])
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", err.Error())
		return
	}
	page, err := h.Service.Observations(r.Context(), seriesID, query)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func parseObservationRequest(r *http.Request, route string) (application.ObservationRequest, error) {
	q := r.URL.Query()
	result := application.ObservationRequest{Mode: application.Mode(q.Get("mode")), Limit: parseInt(q.Get("limit"), 50), Cursor: q.Get("cursor")}
	if route == "revisions" {
		// The revisions resource has one unambiguous clock contract.
		result.Mode = application.ModeRevisions
	}
	if result.Mode == "" {
		result.Mode = application.ModeLatest
	}
	var err error
	if result.From, err = parseTime(q.Get("from")); err != nil {
		return result, errors.New("from must be RFC3339")
	}
	if result.To, err = parseTime(q.Get("to")); err != nil {
		return result, errors.New("to must be RFC3339")
	}
	if result.AsOf, err = parseTime(q.Get("as_of")); err != nil {
		return result, errors.New("as_of must be RFC3339")
	}
	return result, nil
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, value)
}
func parseInt(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func (h Handler) writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
	case errors.Is(err, application.ErrSourceAsOfUnsupported):
		writeErrorDetails(w, http.StatusConflict, "SOURCE_AS_OF_UNSUPPORTED", err.Error(), map[string]any{
			"source_as_of_supported": false,
			"reason":                 "the series has no defensible source knowledge clock",
		})
	case errors.Is(err, application.ErrInvalidCursor), errors.Is(err, application.ErrInvalidWindow):
		writeError(w, http.StatusBadRequest, "INVALID_QUERY", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "timeline query failed")
	}
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeErrorDetails(w, status, code, message, nil)
}
func writeErrorDetails(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	body := map[string]any{"code": code, "message": message, "request_id": requestID()}
	if details != nil {
		body["details"] = details
	}
	writeJSON(w, status, body)
}

func requestID() string { return "timeline-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10) }
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
