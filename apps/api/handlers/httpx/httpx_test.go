package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteJSONSetsStatusAndContentType(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteJSON(recorder, http.StatusCreated, map[string]string{"id": "1"})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := strings.TrimSpace(recorder.Body.String()); got != `{"id":"1"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestDecodeJSON(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	cases := []struct {
		name    string
		body    string
		limit   int64
		message string
	}{
		{"valid", `{"name":"a"}`, 1024, ""},
		{"malformed", `{"name":`, 1024, "request body must be valid JSON"},
		{"unknown field", `{"other":1}`, 1024, "request body must be valid JSON"},
		{"over limit", `{"name":"` + strings.Repeat("a", 64) + `"}`, 16, "request body must be valid JSON"},
		{"trailing object", `{"name":"a"}{"name":"b"}`, 1024, "request body must contain one JSON object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			var target payload
			var message string
			ok := DecodeJSON(httptest.NewRecorder(), request, &target, tc.limit, func(code, msg string) {
				if code != "INVALID_REQUEST" {
					t.Fatalf("code = %q", code)
				}
				message = msg
			})
			if ok != (tc.message == "") || message != tc.message {
				t.Fatalf("ok = %v, message = %q", ok, message)
			}
		})
	}
}
