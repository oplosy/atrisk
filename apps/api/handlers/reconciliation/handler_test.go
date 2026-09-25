package reconciliation

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	application "github.com/oplosy/atrisk/internal/application/reconciliation"
	domain "github.com/oplosy/atrisk/internal/domain/reconciliation"
)

type fakeService struct {
	tolerance  domain.ToleranceVersion
	checkpoint domain.Checkpoint
	err        error
}

func (f fakeService) CreateTolerance(context.Context, string, domain.ToleranceRequest) (domain.ToleranceVersion, error) {
	return f.tolerance, f.err
}
func (f fakeService) Create(context.Context, string, domain.Request) (domain.Checkpoint, error) {
	return f.checkpoint, f.err
}
func (f fakeService) Get(context.Context, string) (domain.Checkpoint, error) {
	return f.checkpoint, f.err
}

func TestReconciliationHandlerRoutesAndStableConflict(t *testing.T) {
	h := New(fakeService{err: &application.ConflictError{Code: "CURRENCY_MISMATCH", Message: "currency mismatch"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/valuations/00000000-0000-0000-0000-000000000001/reconciliations", strings.NewReader(`{"account_id":"00000000-0000-0000-0000-000000000002","source_label":"broker","currency":"USD","cutoff":"2026-01-01T00:00:00Z","external_nav":"1"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"CURRENCY_MISMATCH"`) {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReconciliationHandlerRejectsUnknownFields(t *testing.T) {
	h := New(fakeService{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/00000000-0000-0000-0000-000000000001/reconciliation-tolerances", strings.NewReader(`{"tolerance_amount":"1","unexpected":true}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestReconciliationHandlerMapsNotFound(t *testing.T) {
	h := New(fakeService{err: application.ErrNotFound})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reconciliations/00000000-0000-0000-0000-000000000001", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found status=%d body=%s", rec.Code, rec.Body.String())
	}
}
