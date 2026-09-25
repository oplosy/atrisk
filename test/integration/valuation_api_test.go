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
	if run.State != domainvaluation.StateValid || len(run.Lines) != 2 || run.Lines[0].PriceMethod != "identity" {
		t.Fatalf("unexpected valuation=%+v", run)
	}
	if run.Lines[1].NativeAmount == nil || *run.Lines[1].NativeAmount != "6.000000000000000002" {
		t.Fatalf("native amount=%v", run.Lines[1].NativeAmount)
	}
	if run.Lines[1].TryAmount == nil || *run.Lines[1].TryAmount != "240.000000000000000080" {
		t.Fatalf("try amount=%v", run.Lines[1].TryAmount)
	}
	var reachable int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM valuation_lines l JOIN price_revisions p ON p.id=l.price_revision_id JOIN raw_objects r ON r.id=p.raw_object_id WHERE l.run_id=$1`, mustUUID(t, run.ID)).Scan(&reachable); err != nil || reachable != 1 {
		t.Fatalf("price raw provenance reachable=%d err=%v", reachable, err)
	}
	var changed int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM valuation_runs WHERE id=$1`, mustUUID(t, run.ID)).Scan(&changed); err != nil || changed != 1 {
		t.Fatalf("persisted run count=%d err=%v", changed, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE valuation_runs SET state='blocked' WHERE id=$1`, mustUUID(t, run.ID)); err == nil {
		t.Fatal("valuation run was mutable")
	}
}

func insertValuationRaw(t *testing.T, pool *pgxpool.Pool, suffix string) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	sha := strings.Repeat("0", 62) + map[string]string{"valuation-price": "01", "valuation-fx": "02"}[suffix]
	if err := pool.QueryRow(context.Background(), `INSERT INTO raw_objects (content_sha256,object_key,media_type,byte_length,retrieved_at) VALUES ($1,$2,'application/json',1,clock_timestamp()) RETURNING id`, sha, "valuation/"+suffix).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
