package risk

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	application "github.com/oplosy/atrisk/internal/application/risk"
)

type fakeService struct{ submitted application.SubmitRequest }

func TestRiskAPI(t *testing.T) {
	t.Run("idempotency", TestHandlerRequiresAndForwardsIdempotencyKey)
	t.Run("lifecycle", TestHandlerExposesLifecycleRoutes)
}

func (f *fakeService) Submit(_ context.Context, request application.SubmitRequest) (application.Run, error) {
	f.submitted = request
	return application.Run{ID: "run-1", Status: "queued", DataQuality: "blocked", SchemaVersion: "1.0", EngineVersion: "unknown", InputSnapshotIDs: []string{"snapshot-1"}}, nil
}
func (f *fakeService) Get(context.Context, string) (application.Run, error) {
	return application.Run{ID: "run-1", Status: "completed"}, nil
}
func (f *fakeService) Positions(context.Context, string, string, int) (application.Page, error) {
	return application.Page{Items: []map[string]any{}, Limit: 50}, nil
}
func (f *fakeService) Cancel(context.Context, string, string) (application.Run, error) {
	return application.Run{ID: "run-1", Status: "cancelled"}, nil
}

func TestHandlerRequiresAndForwardsIdempotencyKey(t *testing.T) {
	fake := &fakeService{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/runs", strings.NewReader(`{"account_id":"00000000-0000-0000-0000-000000000001","snapshot_id":"00000000-0000-0000-0000-000000000002","name":"risk","template_key":"risk_off","units":{},"shocks":{},"mappings":{},"assumptions":{},"positions":[]}`))
	req.Header.Set("Idempotency-Key", "risk-1")
	rec := httptest.NewRecorder()
	New(fake).ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if fake.submitted.IdempotencyKey != "risk-1" {
		t.Fatalf("idempotency key=%q", fake.submitted.IdempotencyKey)
	}
}

func TestHandlerRejectsMalformedSubmitUUIDs(t *testing.T) {
	const validAccount = "00000000-0000-0000-0000-000000000001"
	const validSnapshot = "00000000-0000-0000-0000-000000000002"
	base := `{"account_id":"` + validAccount + `","snapshot_id":"` + validSnapshot + `","name":"risk","template_key":"risk_off","units":{},"shocks":{},"mappings":{},"assumptions":{},"positions":[]}`
	cases := []struct {
		name string
		body string
	}{
		{name: "account_id", body: strings.Replace(base, validAccount, "not-a-uuid", 1)},
		{name: "snapshot_id", body: strings.Replace(base, validSnapshot, "not-a-uuid", 1)},
		{name: "scenario_id", body: strings.Replace(base, `"name":"risk"`, `"scenario_id":"not-a-uuid","name":"risk"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/runs", strings.NewReader(tc.body))
			req.Header.Set("Idempotency-Key", "risk-uuid-"+tc.name)
			rec := httptest.NewRecorder()
			New(&fakeService{}).ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandlerRejectsBodyIdempotencyKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/runs", strings.NewReader(`{"idempotency_key":"body-key"}`))
	req.Header.Set("Idempotency-Key", "header-key")
	rec := httptest.NewRecorder()
	New(&fakeService{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerRejectsMissingIdempotencyKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/runs", strings.NewReader(`{"account_id":"a"}`))
	rec := httptest.NewRecorder()
	New(&fakeService{}).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandlerExposesLifecycleRoutes(t *testing.T) {
	badID := httptest.NewRequest(http.MethodGet, "/api/v1/risk/runs/not-a-uuid", nil)
	badRec := httptest.NewRecorder()
	New(&fakeService{}).ServeHTTP(badRec, badID)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid UUID status=%d body=%s", badRec.Code, badRec.Body.String())
	}

	const runID = "00000000-0000-0000-0000-000000000001"
	for _, methodPath := range []struct{ method, path string }{{http.MethodGet, "/api/v1/risk/runs/" + runID + "/status"}, {http.MethodPost, "/api/v1/risk/runs/" + runID + "/cancel"}, {http.MethodGet, "/api/v1/risk/runs/" + runID + "/positions"}} {
		req := httptest.NewRequest(methodPath.method, methodPath.path, nil)
		rec := httptest.NewRecorder()
		New(&fakeService{}).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", methodPath.method, methodPath.path, rec.Code, rec.Body.String())
		}
	}
}
