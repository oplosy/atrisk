package integration

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	apiportfolio "github.com/oplosy/atrisk/apps/api/handlers/portfolio"
	application "github.com/oplosy/atrisk/internal/application/portfolio"
	domain "github.com/oplosy/atrisk/internal/domain/portfolio"
)

func TestPortfolioAPI(t *testing.T) {
	migrateTestDatabase(t)
	queries, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	service := application.Service{Queries: queries, Beginner: pool}

	types := []string{domain.InstrumentCash, domain.InstrumentCurrency, domain.InstrumentSpotCrypto, domain.InstrumentManualSpot, domain.InstrumentFixedBond}
	instruments := make(map[string]domain.Instrument, len(types))
	for _, instrumentType := range types {
		item, err := service.CreateInstrument(ctx, application.CreateInstrumentRequest{CanonicalSymbol: instrumentType + "-fixture", InstrumentType: instrumentType, NativeUnit: "USDT", ExternalIDs: []domain.ExternalIdentifier{{Namespace: "fixture-" + instrumentType, ExternalID: "id-" + instrumentType}}})
		if err != nil {
			t.Fatalf("create %s: %v", instrumentType, err)
		}
		instruments[instrumentType] = item
	}
	if _, err := service.CreateInstrument(ctx, application.CreateInstrumentRequest{CanonicalSymbol: "bad-type", InstrumentType: "future", NativeUnit: "USD"}); !errors.Is(err, application.ErrInvalidRequest) {
		t.Fatalf("invalid type error=%v", err)
	}
	if _, err := service.CreateInstrument(ctx, application.CreateInstrumentRequest{CanonicalSymbol: "bad-unit", InstrumentType: domain.InstrumentCash, NativeUnit: "usdt"}); !errors.Is(err, application.ErrInvalidRequest) {
		t.Fatalf("invalid unit error=%v", err)
	}
	if _, err := service.CreateInstrument(ctx, application.CreateInstrumentRequest{CanonicalSymbol: "duplicate-external", InstrumentType: domain.InstrumentCash, NativeUnit: "USD", ExternalIDs: []domain.ExternalIdentifier{{Namespace: "fixture-cash", ExternalID: "id-cash"}}}); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("duplicate external error=%v", err)
	}
	if _, err := service.CreateInstrument(ctx, application.CreateInstrumentRequest{CanonicalSymbol: "different-namespace", InstrumentType: domain.InstrumentCash, NativeUnit: "USD", ExternalIDs: []domain.ExternalIdentifier{{Namespace: "another", ExternalID: "id-cash"}}}); err != nil {
		t.Fatalf("different namespace external id: %v", err)
	}

	portfolio, err := service.CreatePortfolio(ctx, application.CreatePortfolioRequest{Name: "Fixture portfolio", ReportingCurrency: "TRY", Metadata: map[string]any{"fixture": true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreatePortfolio(ctx, application.CreatePortfolioRequest{Name: "bad", ReportingCurrency: "EUR"}); !errors.Is(err, application.ErrInvalidRequest) {
		t.Fatalf("invalid reporting currency error=%v", err)
	}
	account, err := service.CreateAccount(ctx, portfolio.ID, application.CreateAccountRequest{Name: "Manual account"})
	if err != nil {
		t.Fatal(err)
	}
	otherPortfolio, err := service.CreatePortfolio(ctx, application.CreatePortfolioRequest{Name: "Other portfolio", ReportingCurrency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	otherAccount, err := service.CreateAccount(ctx, otherPortfolio.ID, application.CreateAccountRequest{Name: "Other account"})
	if err != nil {
		t.Fatal(err)
	}

	captured := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("EET", 2*60*60))
	first, err := service.CreateSnapshot(ctx, portfolio.ID, application.CreateSnapshotRequest{CapturedAt: captured, Lines: []application.SnapshotLineInput{
		{AccountID: account.ID, InstrumentID: instruments[domain.InstrumentCash].ID, Quantity: "1.123456789012345678", TotalCostBasis: ptr("987.654321098765432109")},
		{AccountID: account.ID, InstrumentID: instruments[domain.InstrumentFixedBond].ID, Quantity: "-2", TotalCostBasis: nil, ModifiedDurationYears: "4.250000000000000000", ConvexityYearsSquared: "0.125000000000000000"},
	}})
	if err != nil {
		t.Fatalf("create snapshot: %v", err)
	}
	if first.CapturedAt.Location() != time.UTC || first.CapturedAt.Hour() != captured.UTC().Hour() {
		t.Fatalf("captured_at=%v", first.CapturedAt)
	}
	if got := first.Lines[0].Quantity; got != "1.123456789012345678" {
		t.Fatalf("quantity=%q", got)
	}
	if first.Lines[0].TotalCostBasis == nil || *first.Lines[0].TotalCostBasis != "987.654321098765432109" {
		t.Fatalf("cost basis=%v", first.Lines[0].TotalCostBasis)
	}
	if first.Lines[1].TotalCostBasis != nil {
		t.Fatalf("optional cost basis=%v", first.Lines[1].TotalCostBasis)
	}

	if _, err := service.CreateSnapshot(ctx, portfolio.ID, application.CreateSnapshotRequest{CapturedAt: captured.Add(time.Hour), Lines: []application.SnapshotLineInput{{AccountID: otherAccount.ID, InstrumentID: instruments[domain.InstrumentCash].ID, Quantity: "1"}}}); !errors.Is(err, application.ErrInvalidRequest) {
		t.Fatalf("wrong-portfolio account error=%v", err)
	}
	if _, err := service.CreateSnapshot(ctx, portfolio.ID, application.CreateSnapshotRequest{CapturedAt: captured.Add(time.Hour), Lines: []application.SnapshotLineInput{{AccountID: account.ID, InstrumentID: "00000000-0000-0000-0000-000000000001", Quantity: "1"}}}); !errors.Is(err, application.ErrInvalidRequest) {
		t.Fatalf("wrong instrument error=%v", err)
	}
	if _, err := service.CreateSnapshot(ctx, portfolio.ID, application.CreateSnapshotRequest{CapturedAt: captured.Add(time.Hour), Lines: []application.SnapshotLineInput{{AccountID: account.ID, InstrumentID: instruments[domain.InstrumentCash].ID, Quantity: "1", ModifiedDurationYears: "1"}}}); !errors.Is(err, application.ErrInvalidRequest) {
		t.Fatalf("unsupported risk attr error=%v", err)
	}

	correction, err := service.CreateSnapshot(ctx, portfolio.ID, application.CreateSnapshotRequest{CapturedAt: captured.Add(time.Hour), SupersedesSnapshotID: first.ID, Lines: []application.SnapshotLineInput{{AccountID: account.ID, InstrumentID: instruments[domain.InstrumentCash].ID, Quantity: "3.000000000000000000"}}})
	if err != nil {
		t.Fatalf("create correction: %v", err)
	}
	if correction.SupersedesSnapshotID != first.ID || correction.Lines[0].Quantity != "3.000000000000000000" {
		t.Fatalf("correction=%+v", correction)
	}
	original, err := service.GetSnapshot(ctx, first.ID)
	if err != nil || original.Lines[0].Quantity != "1.123456789012345678" {
		t.Fatalf("original changed: %+v err=%v", original, err)
	}

	firstID := validUUID(t, mustUUID(t, first.ID))
	if _, err := pool.Exec(ctx, `UPDATE portfolio_snapshots SET captured_at = captured_at + interval '1 minute' WHERE id = $1`, firstID); err == nil {
		t.Fatal("snapshot UPDATE unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM portfolio_snapshot_lines WHERE snapshot_id = $1`, firstID); err == nil {
		t.Fatal("snapshot line DELETE unexpectedly succeeded")
	}

	h := apiportfolio.New(service)
	for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/v1/snapshots/"+first.ID, bytes.NewReader([]byte(`{}`)))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("snapshot method=%s status=%d body=%s", method, rec.Code, rec.Body.String())
		}
	}

	if err := service.DeleteAccount(ctx, account.ID); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("referenced account delete=%v", err)
	}
	if err := service.DeletePortfolio(ctx, portfolio.ID); !errors.Is(err, application.ErrConflict) {
		t.Fatalf("referenced portfolio delete=%v", err)
	}

	// Concurrent identical namespace/id inserts have one database winner. This
	// is the PostgreSQL uniqueness boundary, not an application pre-check.
	concurrentInstrument, err := service.CreateInstrument(ctx, application.CreateInstrumentRequest{CanonicalSymbol: "concurrent-id", InstrumentType: domain.InstrumentCash, NativeUnit: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	concurrentInstrumentID := mustUUID(t, concurrentInstrument.ID)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, insertErr := pool.Exec(ctx, `INSERT INTO instrument_external_identifiers (instrument_id, namespace, external_id) VALUES ($1,'concurrent','same')`, concurrentInstrumentID)
			results <- insertErr
		}()
	}
	wg.Wait()
	close(results)
	winners, conflicts := 0, 0
	for insertErr := range results {
		if insertErr == nil {
			winners++
		} else {
			conflicts++
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("concurrent identifier results winners=%d conflicts=%d", winners, conflicts)
	}
}

func ptr(value string) *string { return &value }

func mustUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		t.Fatal(err)
	}
	return id
}
