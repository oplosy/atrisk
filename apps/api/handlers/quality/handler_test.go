package quality

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	application "github.com/oplosy/atrisk/internal/application/quality"
	domain "github.com/oplosy/atrisk/internal/quality"
)

type evaluatorFunc func(context.Context, application.Request) (application.Response, error)

func (f evaluatorFunc) Evaluate(ctx context.Context, request application.Request) (application.Response, error) {
	return f(ctx, request)
}

func TestQualityHandlerValidatesAndEvaluatesRequest(t *testing.T) {
	var called bool
	handler := New(evaluatorFunc(func(_ context.Context, request application.Request) (application.Response, error) {
		called = true
		if len(request.Inputs) != 1 || !request.Inputs[0].Required || request.AsOf.Location() != time.UTC {
			t.Fatalf("unexpected parsed request: %+v", request)
		}
		return application.Response{State: domain.Valid, EvaluatedAt: request.AsOf, Items: []application.Item{}}, nil
	}))
	body := `{"from":"2024-01-01T00:00:00Z","to":"2024-01-02T00:00:00Z","as_of":"2024-01-02T00:00:00Z","inputs":[{"series_id":"00000000-0000-0000-0000-000000000001","required":true}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quality/evaluate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !called {
		t.Fatalf("status=%d called=%v body=%s", rec.Code, called, rec.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["state"] != string(domain.Valid) {
		t.Fatalf("response state=%v", response["state"])
	}
}

func TestQualityHandlerRejectsOmittedRequiredFlagAndUnknownRoute(t *testing.T) {
	handler := New(evaluatorFunc(func(context.Context, application.Request) (application.Response, error) {
		t.Fatal("evaluator must not be called for invalid requests")
		return application.Response{}, nil
	}))
	body := `{"from":"2024-01-01T00:00:00Z","to":"2024-01-02T00:00:00Z","as_of":"2024-01-02T00:00:00Z","inputs":[{"series_id":"00000000-0000-0000-0000-000000000001"}]}`
	bad := httptest.NewRequest(http.MethodPost, "/v1/quality/evaluate", strings.NewReader(body))
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, bad)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("omitted required status=%d body=%s", badRec.Code, badRec.Body.String())
	}
	unknown := httptest.NewRequest(http.MethodPost, "/api/v1/quality/unknown", strings.NewReader(body))
	unknownRec := httptest.NewRecorder()
	handler.ServeHTTP(unknownRec, unknown)
	if unknownRec.Code != http.StatusNotFound {
		t.Fatalf("unknown route status=%d", unknownRec.Code)
	}
}

func TestQualityHandlerRejectsTrailingJSONAndWrongMethod(t *testing.T) {
	handler := New(evaluatorFunc(func(context.Context, application.Request) (application.Response, error) {
		t.Fatal("evaluator must not be called for invalid request")
		return application.Response{}, nil
	}))
	body := `{"from":"2024-01-01T00:00:00Z","to":"2024-01-02T00:00:00Z","as_of":"2024-01-02T00:00:00Z","inputs":[{"series_id":"00000000-0000-0000-0000-000000000001","required":false}]} {}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quality/evaluate", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON status=%d", rec.Code)
	}
	wrongMethod := httptest.NewRequest(http.MethodGet, "/api/v1/quality/evaluate", nil)
	wrongRec := httptest.NewRecorder()
	handler.ServeHTTP(wrongRec, wrongMethod)
	if wrongRec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("wrong method status=%d", wrongRec.Code)
	}
}
