package httpx

import (
	"bytes"
	"errors"
	"log/slog"
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

func TestLogInternalErrorRecordsComponentAndCause(t *testing.T) {
	var buffer bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buffer, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	recorder := httptest.NewRecorder()
	recorder.Header().Set(RequestIDHeader, "req-42")
	LogInternalError(recorder, "risk", errors.New("submit risk run: connection refused"))
	got := buffer.String()
	for _, want := range []string{"level=ERROR", "request_id=req-42", "component=risk", `error="submit risk run: connection refused"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("log %q does not contain %q", got, want)
		}
	}
}

func TestWithRequestIDKeepsSafeClientIDsAndGeneratesOthers(t *testing.T) {
	var seen string
	handler := WithRequestID(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen = RequestID(w)
	}))
	cases := map[string]bool{"client-req_1.a:b": true, "": false, "has space": false, "line\nbreak": false, strings.Repeat("a", 129): false}
	for client, kept := range cases {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/portfolios", nil)
		if client != "" {
			request.Header.Set(RequestIDHeader, client)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		got := recorder.Header().Get(RequestIDHeader)
		if got == "" || got != seen {
			t.Fatalf("client %q: response id %q, handler id %q", client, got, seen)
		}
		if (got == client) != kept {
			t.Fatalf("client %q: response id %q, kept=%v", client, got, kept)
		}
	}
}

func TestRequestIDIsStableWithoutMiddleware(t *testing.T) {
	recorder := httptest.NewRecorder()
	first := RequestID(recorder)
	if len(first) != 32 || RequestID(recorder) != first || recorder.Header().Get(RequestIDHeader) != first {
		t.Fatalf("request id %q was not created once and reused", first)
	}
}
