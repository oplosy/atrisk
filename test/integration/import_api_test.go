package integration

import (
	"context"
	"io"
	"testing"
	"time"

	application "github.com/oplosy/atrisk/internal/application/portfolio"
	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/domain/portfolio"
	manualimports "github.com/oplosy/atrisk/internal/imports"
)

type importArchive struct{ objects map[string][]byte }

func (s *importArchive) Put(_ context.Context, object archive.Object) error {
	if s.objects == nil {
		s.objects = map[string][]byte{}
	}
	s.objects[object.Key] = append([]byte(nil), object.Body...)
	return nil
}
func (s *importArchive) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(nil), nil
}

func TestImportAPI(t *testing.T) {
	migrateTestDatabase(t)
	queries, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	psvc := application.Service{Queries: queries, Beginner: pool}
	inst, err := psvc.CreateInstrument(ctx, application.CreateInstrumentRequest{CanonicalSymbol: "IMPORT-USDT", InstrumentType: portfolio.InstrumentCash, NativeUnit: "USDT"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := psvc.CreatePortfolio(ctx, application.CreatePortfolioRequest{Name: "Import fixture", ReportingCurrency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := psvc.CreateAccount(ctx, p.ID, application.CreateAccountRequest{Name: "Manual"})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("account_id,instrument_id,quantity,total_cost_basis,modified_duration_years,convexity_years_squared\n" + a.ID + "," + inst.ID + ",1.250000000000000000,125.000000000000000000,,\n")
	store := &importArchive{}
	svc := manualimports.Service{Pool: pool, Archive: store, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }}
	preview, err := svc.Preview(ctx, manualimports.PreviewRequest{Kind: manualimports.KindPositions, TargetID: p.ID, SchemaVersion: manualimports.SchemaVersion, CapturedAt: "2026-01-02T03:04:05+02:00", Body: body})
	if err != nil || !preview.Valid || preview.Token == "" {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
	result, err := svc.Commit(ctx, manualimports.CommitRequest{Kind: manualimports.KindPositions, TargetID: p.ID, SchemaVersion: manualimports.SchemaVersion, CapturedAt: "2026-01-02T03:04:05+02:00", Token: preview.Token, IdempotencyKey: "import-fixture-1", Body: body})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if result["snapshot_id"] == nil || len(store.objects) != 1 {
		t.Fatalf("result=%v archive=%d", result, len(store.objects))
	}
	replay, err := svc.Commit(ctx, manualimports.CommitRequest{Kind: manualimports.KindPositions, TargetID: p.ID, SchemaVersion: manualimports.SchemaVersion, CapturedAt: "2026-01-02T03:04:05+02:00", Token: preview.Token, IdempotencyKey: "import-fixture-1", Body: body})
	if err != nil || replay["snapshot_id"] != result["snapshot_id"] {
		t.Fatalf("replay=%v err=%v", replay, err)
	}
	bad := manualimports.Parse([]byte("account_id,instrument_id,quantity,total_cost_basis,modified_duration_years,convexity_years_squared\n"+a.ID+","+inst.ID+",=1,,,,\n"), manualimports.KindPositions)
	if bad.Valid || len(bad.Diagnostics) == 0 {
		t.Fatalf("unsafe CSV accepted: %+v", bad)
	}
}
