package quality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	application "github.com/oplosy/atrisk/internal/application/quality"
)

type Evaluator interface {
	Evaluate(context.Context, application.Request) (application.Response, error)
}

type Handler struct{ Service Evaluator }

func New(service Evaluator) Handler { return Handler{Service: service} }

type evaluationRequest struct {
	From   time.Time      `json:"from"`
	To     time.Time      `json:"to"`
	AsOf   time.Time      `json:"as_of"`
	Inputs []inputRequest `json:"inputs"`
}

type inputRequest struct {
	SeriesID string `json:"series_id"`
	Required *bool  `json:"required"`
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !isQualityPath(r.URL.Path) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "quality route not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is supported")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var body evaluationRequest
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_QUALITY_REQUEST", "request body must be a valid quality evaluation")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "INVALID_QUALITY_REQUEST", "request body must contain one JSON object")
		return
	}
	if !body.From.Before(body.To) || body.AsOf.IsZero() || body.To.After(body.AsOf) || len(body.Inputs) == 0 {
		writeError(w, http.StatusBadRequest, "INVALID_QUALITY_REQUEST", "from, to, as_of and at least one input are required; to must not exceed as_of")
		return
	}
	inputs := make([]application.Input, 0, len(body.Inputs))
	for _, input := range body.Inputs {
		if input.Required == nil || strings.TrimSpace(input.SeriesID) == "" {
			writeError(w, http.StatusBadRequest, "INVALID_QUALITY_REQUEST", "every input must include series_id and required")
			return
		}
		inputs = append(inputs, application.Input{SeriesID: input.SeriesID, Required: *input.Required})
	}
	result, err := h.Service.Evaluate(r.Context(), application.Request{From: body.From.UTC(), To: body.To.UTC(), AsOf: body.AsOf.UTC(), Inputs: inputs})
	if err != nil {
		switch {
		case errors.Is(err, application.ErrInvalidRequest):
			writeError(w, http.StatusBadRequest, "INVALID_QUALITY_REQUEST", "quality evaluation request is invalid or exceeds supported bounds")
		case errors.Is(err, application.ErrNotFound):
			writeError(w, http.StatusNotFound, "NOT_FOUND", "a requested quality series does not exist")
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "quality evaluation failed")
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func isQualityPath(path string) bool {
	path = strings.TrimPrefix(path, "/api/v1/")
	path = strings.TrimPrefix(path, "/v1/")
	return strings.Trim(path, "/") == "quality/evaluate"
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message, "request_id": fmt.Sprintf("quality-%d", time.Now().UTC().UnixNano())})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
