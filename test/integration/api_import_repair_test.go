package integration

import (
	"context"
	"errors"
	"testing"
	"time"

	application "github.com/oplosy/atrisk/internal/application/portfolio"
	"github.com/oplosy/atrisk/internal/domain/portfolio"
	manualimports "github.com/oplosy/atrisk/internal/imports"
)

func TestAPIImportRepair(t *testing.T) {
	runImportAPIRepair(t)
}

func TestImportAPIRepair(t *testing.T) {
	runImportAPIRepair(t)
}

func runImportAPIRepair(t *testing.T) {
	migrateTestDatabase(t)
	queries, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	stamp := time.Now().UTC().Format("20060102150405.000000000")
	psvc := application.Service{Queries: queries, Beginner: pool}
	ownerInstrument, err := psvc.CreateInstrument(ctx, application.CreateInstrumentRequest{CanonicalSymbol: "REPAIR-" + stamp, InstrumentType: portfolio.InstrumentCash, NativeUnit: "USDT"})
	if err != nil {
		t.Fatal(err)
	}
	foreignInstrument, err := psvc.CreateInstrument(ctx, application.CreateInstrumentRequest{CanonicalSymbol: "REPAIR-FOREIGN-" + stamp, InstrumentType: portfolio.InstrumentCash, NativeUnit: "USDT"})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := psvc.CreatePortfolio(ctx, application.CreatePortfolioRequest{Name: "repair-owner-" + stamp, ReportingCurrency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := psvc.CreatePortfolio(ctx, application.CreatePortfolioRequest{Name: "repair-foreign-" + stamp, ReportingCurrency: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := psvc.CreateAccount(ctx, owner.ID, application.CreateAccountRequest{Name: "repair-owner-account"})
	if err != nil {
		t.Fatal(err)
	}
	foreignAccount, err := psvc.CreateAccount(ctx, foreign.ID, application.CreateAccountRequest{Name: "repair-foreign-account"})
	if err != nil {
		t.Fatal(err)
	}

	validBody := []byte("account_id,instrument_id,quantity,total_cost_basis,modified_duration_years,convexity_years_squared\n" + account.ID + "," + ownerInstrument.ID + ",12345678901234567890.123456789012345678,98765432109876543210.123456789012345678,,\n")
	store := &importArchive{}
	service := manualimports.Service{Pool: pool, Archive: store, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }}
	preview, err := service.Preview(ctx, manualimports.PreviewRequest{Kind: manualimports.KindPositions, TargetID: owner.ID, SchemaVersion: manualimports.SchemaVersion, CapturedAt: "2026-01-02T03:04:05+02:00", Body: validBody})
	if err != nil || !preview.Valid {
		t.Fatalf("valid preview=%+v err=%v", preview, err)
	}
	result, err := service.Commit(ctx, manualimports.CommitRequest{Kind: manualimports.KindPositions, TargetID: owner.ID, SchemaVersion: manualimports.SchemaVersion, CapturedAt: "2026-01-02T03:04:05+02:00", Token: preview.Token, IdempotencyKey: "repair-valid-" + stamp, Body: validBody})
	if err != nil {
		t.Fatalf("valid commit: %v", err)
	}
	var quantity, cost string
	if err := pool.QueryRow(ctx, `SELECT quantity::text,total_cost_basis::text FROM portfolio_snapshot_lines WHERE snapshot_id=$1::uuid`, result["snapshot_id"]).Scan(&quantity, &cost); err != nil {
		t.Fatal(err)
	}
	if quantity != "12345678901234567890.123456789012345678" || cost != "98765432109876543210.123456789012345678" {
		t.Fatalf("exact decimals changed: quantity=%q cost=%q", quantity, cost)
	}

	priceBody := []byte("instrument_id,quote_currency,observation_time,price,source_known_at\n" + ownerInstrument.ID + ",usdt,2026-01-05T03:04:05+02:00,12345678901234567890.123456789012345678,\n" + ownerInstrument.ID + ",usdt,2026-01-06T03:04:05+02:00,2.000000000000000001,\n")
	pricePreview, err := service.Preview(ctx, manualimports.PreviewRequest{Kind: manualimports.KindManualPrices, TargetID: owner.ID, SchemaVersion: manualimports.SchemaVersion, Body: priceBody})
	if err != nil || !pricePreview.Valid {
		t.Fatalf("price preview=%+v err=%v", pricePreview, err)
	}
	priceResult, err := service.Commit(ctx, manualimports.CommitRequest{Kind: manualimports.KindManualPrices, TargetID: owner.ID, SchemaVersion: manualimports.SchemaVersion, Token: pricePreview.Token, IdempotencyKey: "repair-prices-" + stamp, Body: priceBody})
	if err != nil || priceResult["revision_count"] != int64(2) {
		t.Fatalf("price commit=%v err=%v", priceResult, err)
	}
	var storedPrice string
	if err := pool.QueryRow(ctx, `SELECT price::text FROM price_revisions WHERE instrument_id=$1::uuid AND observation_time='2026-01-05T01:04:05Z'`, ownerInstrument.ID).Scan(&storedPrice); err != nil {
		t.Fatal(err)
	}
	if storedPrice != "12345678901234567890.123456789012345678" {
		t.Fatalf("exact manual price changed: %q", storedPrice)
	}
	priceReplayPreview, err := service.Preview(ctx, manualimports.PreviewRequest{Kind: manualimports.KindManualPrices, TargetID: owner.ID, SchemaVersion: manualimports.SchemaVersion, Body: priceBody})
	if err != nil || !priceReplayPreview.Valid {
		t.Fatalf("price replay preview=%+v err=%v", priceReplayPreview, err)
	}
	priceReplay, err := service.Commit(ctx, manualimports.CommitRequest{Kind: manualimports.KindManualPrices, TargetID: owner.ID, SchemaVersion: manualimports.SchemaVersion, Token: priceReplayPreview.Token, IdempotencyKey: "repair-prices-replay-" + stamp, Body: priceBody})
	if !errors.Is(err, manualimports.ErrInvalidRequest) || priceReplay != nil {
		t.Fatalf("duplicate price revisions should fail atomically: result=%v err=%v", priceReplay, err)
	}

	foreignBody := []byte("account_id,instrument_id,quantity,total_cost_basis,modified_duration_years,convexity_years_squared\n" + foreignAccount.ID + "," + foreignInstrument.ID + ",1.25,2.50,,\n")
	foreignPreview, err := service.Preview(ctx, manualimports.PreviewRequest{Kind: manualimports.KindPositions, TargetID: owner.ID, SchemaVersion: manualimports.SchemaVersion, CapturedAt: "2026-01-03T03:04:05+02:00", Body: foreignBody})
	if err != nil || !foreignPreview.Valid {
		t.Fatalf("cross-portfolio preview=%+v err=%v", foreignPreview, err)
	}
	var beforeSnapshots int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM portfolio_snapshots WHERE portfolio_id=$1::uuid`, owner.ID).Scan(&beforeSnapshots); err != nil {
		t.Fatal(err)
	}
	_, err = service.Commit(ctx, manualimports.CommitRequest{Kind: manualimports.KindPositions, TargetID: owner.ID, SchemaVersion: manualimports.SchemaVersion, CapturedAt: "2026-01-03T03:04:05+02:00", Token: foreignPreview.Token, IdempotencyKey: "repair-cross-" + stamp, Body: foreignBody})
	if !errors.Is(err, manualimports.ErrInvalidRequest) {
		t.Fatalf("cross-portfolio commit error=%v", err)
	}
	var afterSnapshots int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM portfolio_snapshots WHERE portfolio_id=$1::uuid`, owner.ID).Scan(&afterSnapshots); err != nil {
		t.Fatal(err)
	}
	if beforeSnapshots != afterSnapshots {
		t.Fatalf("invalid cross-portfolio import changed snapshots: before=%d after=%d", beforeSnapshots, afterSnapshots)
	}

	duplicate := []byte("account_id,instrument_id,quantity,total_cost_basis,modified_duration_years,convexity_years_squared\n" + account.ID + "," + ownerInstrument.ID + ",1,2,,\n" + account.ID + "," + ownerInstrument.ID + ",3,4,,\n")
	duplicatePreview, err := service.Preview(ctx, manualimports.PreviewRequest{Kind: manualimports.KindPositions, TargetID: owner.ID, SchemaVersion: manualimports.SchemaVersion, CapturedAt: "2026-01-04T03:04:05+02:00", Body: duplicate})
	if err != nil || duplicatePreview.Valid || duplicatePreview.Token != "" {
		t.Fatalf("duplicate rows accepted: %+v err=%v", duplicatePreview, err)
	}
}
