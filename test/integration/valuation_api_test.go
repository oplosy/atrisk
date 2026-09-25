package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	apivaluation "github.com/oplosy/atrisk/apps/api/handlers/valuation"
	applicationportfolio "github.com/oplosy/atrisk/internal/application/portfolio"
	applicationvaluation "github.com/oplosy/atrisk/internal/application/valuation"
	domainportfolio "github.com/oplosy/atrisk/internal/domain/portfolio"
	domainvaluation "github.com/oplosy/atrisk/internal/domain/valuation"
	"github.com/oplosy/atrisk/internal/platform/database"
)

func TestValuationAPI(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	portfolioService := applicationportfolio.Service{Queries: database.New(pool), Beginner: pool}
	usdCash, err := portfolioService.CreateInstrument(ctx, applicationportfolio.CreateInstrumentRequest{CanonicalSymbol: "valuation-usd-cash", InstrumentType: domainportfolio.InstrumentCash, NativeUnit: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	spot, err := portfolioService.CreateInstrument(ctx, applicationportfolio.CreateInstrumentRequest{CanonicalSymbol: "valuation-spot", InstrumentType: domainportfolio.InstrumentManualSpot, NativeUnit: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	blockedSpot, err := portfolioService.CreateInstrument(ctx, applicationportfolio.CreateInstrumentRequest{CanonicalSymbol: "valuation-blocked-spot", InstrumentType: domainportfolio.InstrumentManualSpot, NativeUnit: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	portfolio, err := portfolioService.CreatePortfolio(ctx, applicationportfolio.CreatePortfolioRequest{Name: "valuation-fixture", ReportingCurrency: "TRY"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := portfolioService.CreateAccount(ctx, portfolio.ID, applicationportfolio.CreateAccountRequest{Name: "valuation-account"})
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	snapshot, err := portfolioService.CreateSnapshot(ctx, portfolio.ID, applicationportfolio.CreateSnapshotRequest{CapturedAt: cutoff, Lines: []applicationportfolio.SnapshotLineInput{{AccountID: account.ID, InstrumentID: usdCash.ID, Quantity: "1.000000000000000001"}, {AccountID: account.ID, InstrumentID: spot.ID, Quantity: "2"}}})
	if err != nil {
		t.Fatal(err)
	}
	raw1 := insertValuationRaw(t, pool, "valuation-price")
	raw2 := insertValuationRaw(t, pool, "valuation-fx")
	if _, err := pool.Exec(ctx, `INSERT INTO price_revisions (instrument_id,quote_currency,observation_time,price,source_known_at,knowledge_time_basis,raw_object_id) VALUES ($1,'USD',$2,'3.000000000000000001',$2,'source_published_at',$3)`, mustUUID(t, spot.ID), cutoff, raw1); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO fx_quote_revisions (base_currency,quote_currency,observation_time,rate,source_known_at,knowledge_time_basis,raw_object_id) VALUES ('USD','TRY',$1,'40',$1,'source_published_at',$2)`, cutoff, raw2); err != nil {
		t.Fatal(err)
	}
	h := apivaluation.New(applicationvaluation.Service{Pool: pool})
	body, _ := json.Marshal(domainvaluation.Request{SnapshotID: snapshot.ID, Cutoff: cutoff, KnowledgeMode: domainvaluation.KnowledgeSource, KnownAt: cutoff.Add(time.Hour), PriceMaxAgeSeconds: 60, FXMaxAgeSeconds: 60})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/valuations", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var run domainvaluation.Run
	if err := json.NewDecoder(rec.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	if run.State != domainvaluation.StateValid || len(run.Lines) != 2 {
		t.Fatalf("unexpected valuation=%+v", run)
	}
	var pricedLine domainvaluation.Line
	identityFound := false
	for _, line := range run.Lines {
		if line.InstrumentID == spot.ID {
			pricedLine = line
		}
		if line.InstrumentID == usdCash.ID && line.PriceMethod == "identity" {
			identityFound = true
		}
	}
	if !identityFound {
		t.Fatalf("identity line missing: %+v", run.Lines)
	}
	if pricedLine.NativeAmount == nil || *pricedLine.NativeAmount != "6.000000000000000002" {
		t.Fatalf("native amount=%v", pricedLine.NativeAmount)
	}
	if pricedLine.TryAmount == nil || *pricedLine.TryAmount != "240.000000000000000080" || pricedLine.PriceQuoteUnit == nil || *pricedLine.PriceQuoteUnit != "USD" {
		t.Fatalf("try amount=%v quote=%v", pricedLine.TryAmount, pricedLine.PriceQuoteUnit)
	}
	if len(pricedLine.TryFXPath) != 1 || len(pricedLine.USDFXPath) != 0 {
		t.Fatalf("separate FX paths try=%v usd=%v", pricedLine.TryFXPath, pricedLine.USDFXPath)
	}
	var reachable int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM valuation_lines l JOIN price_revisions p ON p.id=l.price_revision_id JOIN raw_objects r ON r.id=p.raw_object_id WHERE l.run_id=$1`, mustUUID(t, run.ID)).Scan(&reachable); err != nil || reachable != 1 {
		t.Fatalf("price raw provenance reachable=%d err=%v", reachable, err)
	}
	var persistedTRY, persistedUSD string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(sum(try_amount),0)::text, COALESCE(sum(usd_amount),0)::text FROM valuation_lines WHERE run_id=$1`, mustUUID(t, run.ID)).Scan(&persistedTRY, &persistedUSD); err != nil {
		t.Fatal(err)
	}
	if run.Totals.TRY == nil || *run.Totals.TRY != persistedTRY || run.Totals.USD == nil || *run.Totals.USD != persistedUSD {
		t.Fatalf("run totals do not equal persisted line sums: run=%+v persisted try=%s usd=%s", run.Totals, persistedTRY, persistedUSD)
	}
	var changed int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM valuation_runs WHERE id=$1`, mustUUID(t, run.ID)).Scan(&changed); err != nil || changed != 1 {
		t.Fatalf("persisted run count=%d err=%v", changed, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE valuation_runs SET state='blocked' WHERE id=$1`, mustUUID(t, run.ID)); err == nil {
		t.Fatal("valuation run was mutable")
	}

	// Future, stale, and source-not-yet-known revisions are all excluded by a
	// source-as-of request; source-as-of must not fall back to system-known time.
	blockedSnapshot, err := portfolioService.CreateSnapshot(ctx, portfolio.ID, applicationportfolio.CreateSnapshotRequest{CapturedAt: cutoff, Lines: []applicationportfolio.SnapshotLineInput{{AccountID: account.ID, InstrumentID: blockedSpot.ID, Quantity: "1"}}})
	if err != nil {
		t.Fatal(err)
	}
	raw3 := insertValuationRaw(t, pool, "valuation-future")
	raw4 := insertValuationRaw(t, pool, "valuation-stale")
	raw5 := insertValuationRaw(t, pool, "valuation-source-late")
	if _, err := pool.Exec(ctx, `INSERT INTO price_revisions (instrument_id,quote_currency,observation_time,price,source_known_at,system_known_at,knowledge_time_basis,raw_object_id) VALUES ($1,'USD',$2,'9',$2,$2,'source_published_at',$3),($1,'USD',$4,'8',$4,$4,'source_published_at',$5),($1,'USD',$6,'7',$7,$6,'source_published_at',$8)`, mustUUID(t, blockedSpot.ID), cutoff.Add(time.Minute), raw3, cutoff.Add(-2*time.Hour), raw4, cutoff, cutoff.Add(2*time.Hour), raw5); err != nil {
		t.Fatal(err)
	}
	blockedBody, _ := json.Marshal(domainvaluation.Request{SnapshotID: blockedSnapshot.ID, Cutoff: cutoff, KnowledgeMode: domainvaluation.KnowledgeSource, KnownAt: cutoff.Add(time.Hour), PriceMaxAgeSeconds: 60, FXMaxAgeSeconds: 60})
	blockedReq := httptest.NewRequest(http.MethodPost, "/api/v1/valuations", bytes.NewReader(blockedBody))
	blockedRec := httptest.NewRecorder()
	h.ServeHTTP(blockedRec, blockedReq)
	if blockedRec.Code != http.StatusCreated {
		t.Fatalf("blocked valuation status=%d body=%s", blockedRec.Code, blockedRec.Body.String())
	}
	var blockedRun domainvaluation.Run
	if err := json.NewDecoder(blockedRec.Body).Decode(&blockedRun); err != nil {
		t.Fatal(err)
	}
	if blockedRun.State != domainvaluation.StateBlocked || len(blockedRun.Lines) != 1 || len(blockedRun.Lines[0].ReasonCodes) == 0 || blockedRun.Lines[0].ReasonCodes[0].Code != "PRICE_MISSING_OR_STALE" {
		t.Fatalf("point-in-time blocked result=%+v", blockedRun)
	}
}

func insertValuationRaw(t *testing.T, pool *pgxpool.Pool, suffix string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	sha := strings.Repeat("0", 62) + map[string]string{"valuation-price": "01", "valuation-fx": "02", "valuation-future": "03", "valuation-stale": "04", "valuation-source-late": "05"}[suffix]
	if err := pool.QueryRow(context.Background(), `INSERT INTO raw_objects (content_sha256,object_key,media_type,byte_length,retrieved_at) VALUES ($1,$2,'application/json',1,clock_timestamp()) RETURNING id`, sha, "valuation/"+suffix).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
