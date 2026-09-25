package portfolio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	application "github.com/oplosy/atrisk/internal/application/portfolio"
	domain "github.com/oplosy/atrisk/internal/domain/portfolio"
)

type stubService struct{}

func (stubService) CreateInstrument(context.Context, application.CreateInstrumentRequest) (domain.Instrument, error) {
	return domain.Instrument{}, nil
}
func (stubService) ListInstruments(context.Context) ([]domain.Instrument, error) {
	return []domain.Instrument{}, nil
}
func (stubService) GetInstrument(context.Context, string) (domain.Instrument, error) {
	return domain.Instrument{}, nil
}
func (stubService) UpdateInstrumentStatus(context.Context, string, string) (domain.Instrument, error) {
	return domain.Instrument{}, nil
}
func (stubService) CreatePortfolio(context.Context, application.CreatePortfolioRequest) (domain.Portfolio, error) {
	return domain.Portfolio{}, nil
}
func (stubService) ListPortfolios(context.Context) ([]domain.Portfolio, error) {
	return []domain.Portfolio{}, nil
}
func (stubService) GetPortfolio(context.Context, string) (domain.Portfolio, error) {
	return domain.Portfolio{}, nil
}
func (stubService) UpdatePortfolio(context.Context, string, application.UpdatePortfolioRequest) (domain.Portfolio, error) {
	return domain.Portfolio{}, nil
}
func (stubService) DeletePortfolio(context.Context, string) error { return nil }
func (stubService) CreateAccount(context.Context, string, application.CreateAccountRequest) (domain.Account, error) {
	return domain.Account{}, nil
}
func (stubService) ListAccounts(context.Context, string) ([]domain.Account, error) {
	return []domain.Account{}, nil
}
func (stubService) GetAccount(context.Context, string) (domain.Account, error) {
	return domain.Account{}, nil
}
func (stubService) UpdateAccount(context.Context, string, application.UpdateAccountRequest) (domain.Account, error) {
	return domain.Account{}, nil
}
func (stubService) DeleteAccount(context.Context, string) error { return nil }
func (stubService) CreateSnapshot(context.Context, string, application.CreateSnapshotRequest) (domain.Snapshot, error) {
	return domain.Snapshot{}, nil
}
func (stubService) ListSnapshots(context.Context, string) ([]domain.Snapshot, error) {
	return []domain.Snapshot{}, nil
}
func (stubService) GetSnapshot(context.Context, string) (domain.Snapshot, error) {
	return domain.Snapshot{}, nil
}

func TestSnapshotRoutesAreReadOnlyExceptCreate(t *testing.T) {
	handler := New(stubService{})
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/v1/snapshots/00000000-0000-0000-0000-000000000001", strings.NewReader(`{}`))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("method=%s status=%d body=%s", method, rec.Code, rec.Body.String())
		}
	}
}

func TestHandlerRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	handler := New(stubService{})
	for _, body := range []string{`{"name":"p","reporting_currency":"TRY","unknown":true}`, `{"name":"p","reporting_currency":"TRY"}{}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/portfolios", strings.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d", body, rec.Code)
		}
	}
}
