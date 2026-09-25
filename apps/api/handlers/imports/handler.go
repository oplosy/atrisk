package imports

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	application "github.com/oplosy/atrisk/internal/imports"
)

type Handler struct{ Service application.Service }

func New(service application.Service) Handler { return Handler{Service: service} }

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(strings.Trim(r.URL.Path, "/"), "api/v1/")
	path = strings.TrimPrefix(path, "v1/")
	parts := strings.Split(path, "/")
	if len(parts) != 3 || parts[0] != "imports" || (parts[1] != application.KindPositions && parts[1] != application.KindManualPrices) || (parts[2] != "preview" && parts[2] != "commit") {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "import route not found")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is supported")
		return
	}
	if h.Service.Pool == nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "import service is unavailable")
		return
	}
	max := int64(application.MaxBytes) + 1024
	r.Body = http.MaxBytesReader(w, r.Body, max)
	if err := r.ParseMultipartForm(max); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_MULTIPART", "multipart form is invalid")
		return
	}
	schema := r.FormValue("schema_version")
	target := r.FormValue("target_id")
	if target == "" {
		target = r.FormValue("portfolio_id")
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "MISSING_FILE", "multipart field file is required")
		return
	}
	defer file.Close()
	media := header.Header.Get("Content-Type")
	if media == "" {
		media = ""
	}
	parsedMedia, _, _ := mime.ParseMediaType(media)
	if parsedMedia != "text/csv" && parsedMedia != "application/csv" && parsedMedia != "text/plain" {
		writeError(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", "file must use a CSV media type")
		return
	}
	body, err := io.ReadAll(io.LimitReader(file, int64(application.MaxBytes)+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_FILE", "CSV file could not be read")
		return
	}
	if len(body) > application.MaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE", "CSV file exceeds 10 MiB")
		return
	}
	if parts[2] == "preview" {
		response, err := h.Service.Preview(r.Context(), application.PreviewRequest{Kind: parts[1], TargetID: target, SchemaVersion: schema, CapturedAt: r.FormValue("captured_at"), Body: body})
		if err != nil {
			writeImportError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	}
	key := r.Header.Get("Idempotency-Key")
	response, err := h.Service.Commit(r.Context(), application.CommitRequest{Kind: parts[1], TargetID: target, SchemaVersion: schema, CapturedAt: r.FormValue("captured_at"), Token: r.FormValue("token"), IdempotencyKey: key, Body: body})
	if err != nil {
		writeImportError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, response)
}

func writeImportError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, application.ErrInvalidRequest):
		writeError(w, http.StatusBadRequest, "INVALID_IMPORT_REQUEST", "import request is invalid")
	case errors.Is(err, application.ErrConflict):
		writeError(w, http.StatusConflict, "IMPORT_CONFLICT", "preview token or idempotency request conflicts with existing state")
	case errors.Is(err, application.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND", "import target does not exist")
	case errors.Is(err, application.ErrUnavailable):
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "import service is unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "import failed")
	}
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "message": message})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
