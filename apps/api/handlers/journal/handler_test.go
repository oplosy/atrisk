package journal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	application "github.com/oplosy/atrisk/internal/application/journal"
	domain "github.com/oplosy/atrisk/internal/domain/journal"
)

type fakeService struct {
	decision  domain.Decision
	review    domain.Review
	amendment domain.Amendment
	timeline  domain.Timeline
	err       error
}

func (f fakeService) Create(context.Context, domain.CreateRequest) (domain.Decision, error) {
	return f.decision, f.err
}
func (f fakeService) Get(context.Context, string) (domain.Decision, error) { return f.decision, f.err }
func (f fakeService) Finalize(context.Context, string) (domain.Decision, error) {
	return f.decision, f.err
}
func (f fakeService) AddReview(context.Context, string, domain.ReviewRequest) (domain.Review, error) {
	return f.review, f.err
}
func (f fakeService) AddAmendment(context.Context, string, domain.AmendmentRequest) (domain.Amendment, error) {
	return f.amendment, f.err
}
func (f fakeService) Timeline(context.Context, string) (domain.Timeline, error) {
	return f.timeline, f.err
}

func TestDecisionJournalHandlerRoutesAndRejectsUnknownFields(t *testing.T) {
	h := New(fakeService{decision: domain.Decision{ID: "decision-1"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/decisions", strings.NewReader(`{"account_id":"00000000-0000-0000-0000-000000000001","unexpected":true}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/decisions/00000000-0000-0000-0000-000000000001/finalize", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("finalize status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDecisionJournalHandlerMapsConflictAndNotFound(t *testing.T) {
	h := New(fakeService{err: &application.ConflictError{Code: "DECISION_IMMUTABLE", Message: "already finalized"}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/decisions/00000000-0000-0000-0000-000000000001/finalize", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"DECISION_IMMUTABLE"`) {
		t.Fatalf("conflict status=%d body=%s", rec.Code, rec.Body.String())
	}

	h = New(fakeService{err: application.ErrNotFound})
	req = httptest.NewRequest(http.MethodGet, "/api/v1/decisions/00000000-0000-0000-0000-000000000001/timeline", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("not found status=%d body=%s", rec.Code, rec.Body.String())
	}
}
