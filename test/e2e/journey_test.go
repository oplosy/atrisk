package e2e

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	apiimports "github.com/oplosy/atrisk/apps/api/handlers/imports"
	apijournal "github.com/oplosy/atrisk/apps/api/handlers/journal"
	apiportfolio "github.com/oplosy/atrisk/apps/api/handlers/portfolio"
	apirisk "github.com/oplosy/atrisk/apps/api/handlers/risk"
	apivaluation "github.com/oplosy/atrisk/apps/api/handlers/valuation"
	"github.com/oplosy/atrisk/internal/application/evidence"
	applicationjournal "github.com/oplosy/atrisk/internal/application/journal"
	applicationportfolio "github.com/oplosy/atrisk/internal/application/portfolio"
	applicationrisk "github.com/oplosy/atrisk/internal/application/risk"
	applicationvaluation "github.com/oplosy/atrisk/internal/application/valuation"
	"github.com/oplosy/atrisk/internal/archive"
	domainjournal "github.com/oplosy/atrisk/internal/domain/journal"
	domainportfolio "github.com/oplosy/atrisk/internal/domain/portfolio"
	domainvaluation "github.com/oplosy/atrisk/internal/domain/valuation"
	applicationimports "github.com/oplosy/atrisk/internal/imports"
	"github.com/oplosy/atrisk/internal/platform/database"
)

type journeyFixture struct {
	Cutoff                  string `json:"cutoff"`
	BusinessDayObservations int    `json:"business_day_observations"`
	BasePrice               int    `json:"base_price"`
	DailyIncrement          int    `json:"daily_increment"`
	USDTRY                  string `json:"usd_try"`
	AssetClass              string `json:"asset_class"`
	StressReturn            string `json:"stress_return"`
}

func TestAtlasRiskJourney(t *testing.T) {
	ctx := context.Background()
	root := repositoryRoot(t)
	fixtureBytes, err := os.ReadFile(filepath.Join(root, "test", "fixtures", "system", "journey.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture journeyFixture
	if err := json.Unmarshal(fixtureBytes, &fixture); err != nil {
		t.Fatal(err)
	}
	cutoff, err := time.Parse(time.RFC3339, fixture.Cutoff)
	if err != nil {
		t.Fatal(err)
	}
	knownAt := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	pool := journeyDatabase(t, root)
	defer pool.Close()
	archiveStore := journeyArchive(t, ctx)
	api := journeyAPI(pool, archiveStore)
	defer api.Close()
	var runID string
	if err := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&runID); err != nil {
		t.Fatalf("create isolated journey key: %v", err)
	}

	instrument := apiJSON[domainportfolio.Instrument](t, api.Client(), http.MethodPost, api.URL+"/api/v1/instruments", applicationportfolio.CreateInstrumentRequest{
		CanonicalSymbol: "AR601-JOURNEY-SPOT-" + strings.ReplaceAll(runID, "-", ""), InstrumentType: domainportfolio.InstrumentManualSpot, NativeUnit: "USD",
	}, http.StatusCreated)
	portfolio := apiJSON[domainportfolio.Portfolio](t, api.Client(), http.MethodPost, api.URL+"/api/v1/portfolios", applicationportfolio.CreatePortfolioRequest{
		Name: "AR-601 deterministic journey " + runID, ReportingCurrency: "TRY", Metadata: map[string]any{"fixture": "AR-601"},
	}, http.StatusCreated)
	account := apiJSON[domainportfolio.Account](t, api.Client(), http.MethodPost, api.URL+"/api/v1/portfolios/"+portfolio.ID+"/accounts", applicationportfolio.CreateAccountRequest{Name: "Fixture account"}, http.StatusCreated)
	snapshot := apiJSON[domainportfolio.Snapshot](t, api.Client(), http.MethodPost, api.URL+"/api/v1/portfolios/"+portfolio.ID+"/snapshots", applicationportfolio.CreateSnapshotRequest{
		CapturedAt: cutoff,
		Lines:      []applicationportfolio.SnapshotLineInput{{AccountID: account.ID, InstrumentID: instrument.ID, Quantity: "2"}},
	}, http.StatusCreated)
	if len(snapshot.Lines) != 1 {
		t.Fatalf("snapshot lines=%d, want 1", len(snapshot.Lines))
	}

	priceCSV := journeyPricesCSV(t, instrument.ID, cutoff, fixture)
	preview := apiImport(t, api.Client(), api.URL, "preview", portfolio.ID, priceCSV, "", http.StatusOK)
	if !preview.Valid || preview.RowCount != fixture.BusinessDayObservations {
		t.Fatalf("manual-price preview=%+v", preview)
	}
	apiImport(t, api.Client(), api.URL, "commit", portfolio.ID, priceCSV, preview.Token, http.StatusCreated, "ar601-journey-prices-"+runID)
	assertRevisedSourceAsOf(t, ctx, api, pool, instrument.ID, portfolio.ID, cutoff, runID, priceCSV)
	fxPayload := []byte(`{"pair":"USD/TRY","observation_time":"` + cutoff.Format(time.RFC3339) + `","rate":"` + fixture.USDTRY + `","journey_id":"` + runID + `"}`)
	fxRef, err := archive.ArchivePayload(ctx, archiveStore, fxPayload, "application/json", map[string]string{"fixture": "AR-601"})
	if err != nil {
		t.Fatalf("archive FX evidence: %v", err)
	}
	var fxRawID string
	if err := pool.QueryRow(ctx, `INSERT INTO raw_objects (content_sha256,object_key,media_type,byte_length,retrieved_at) VALUES ($1,$2,$3,$4,clock_timestamp()) RETURNING id::text`, fxRef.ContentSHA256, fxRef.Key, fxRef.MediaType, fxRef.ByteLength).Scan(&fxRawID); err != nil {
		t.Fatalf("register FX evidence: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO fx_quote_revisions (base_currency,quote_currency,observation_time,rate,source_known_at,knowledge_time_basis,raw_object_id) VALUES ('USD','TRY',$1,$2,$1,'source_published_at',$3::uuid)`, cutoff, fixture.USDTRY, fxRawID); err != nil {
		t.Fatalf("ingest USD/TRY quote: %v", err)
	}

	valuation := apiJSON[domainvaluation.Run](t, api.Client(), http.MethodPost, api.URL+"/api/v1/valuations", domainvaluation.Request{
		SnapshotID: snapshot.ID, Cutoff: cutoff, KnowledgeMode: domainvaluation.KnowledgeSystem,
		KnownAt: knownAt, PriceMaxAgeSeconds: 0, FXMaxAgeSeconds: 0,
	}, http.StatusCreated)
	if valuation.State != domainvaluation.StateValid || len(valuation.Lines) != 1 || valuation.Totals.TRY == nil || valuation.Totals.USD == nil {
		t.Fatalf("valuation not complete: %+v", valuation)
	}
	line := valuation.Lines[0]
	if line.PriceRevisionID == nil || line.PriceQuoteUnit == nil || *line.PriceQuoteUnit != "USD" || len(line.TryFXPath) != 1 || len(line.USDFXPath) != 0 {
		t.Fatalf("valuation lineage incomplete: %+v", line)
	}
	if *valuation.Totals.USD != "326.000000000000000000" || *valuation.Totals.TRY != "13040.000000000000000000" {
		t.Fatalf("valuation totals USD=%s TRY=%s", *valuation.Totals.USD, *valuation.Totals.TRY)
	}

	riskRequest := applicationrisk.SubmitRequest{
		AccountID: account.ID, SnapshotID: snapshot.ID, ValuationID: valuation.ID,
		Name: "AR-601 stress fixture", TemplateKey: "risk_off",
		Units:       map[string]any{"reporting_currency": "TRY"},
		Shocks:      map[string]any{"asset_class_returns": map[string]any{fixture.AssetClass: fixture.StressReturn}},
		Mappings:    map[string]any{"instrument_asset_classes": map[string]any{instrument.ID: fixture.AssetClass}},
		Assumptions: map[string]any{"coverage_policy": "block", "pnl_tolerance": "0.00000001"},
	}
	riskRun := submitRiskRunAPI(t, api, pool, riskRequest, "ar601-journey-risk-"+runID)
	if !runRiskEngineWorker(t, root) {
		t.Fatal("risk worker did not claim and complete the queued run")
	}
	completedRun := apiJSON[applicationrisk.Run](t, api.Client(), http.MethodGet, api.URL+"/api/v1/risk/runs/"+riskRun.ID, nil, http.StatusOK)
	if completedRun.Status != "completed" || completedRun.DataQuality != "healthy" {
		t.Fatalf("completed stress run=%+v", completedRun)
	}
	var workerResult map[string]any
	if err := json.Unmarshal(completedRun.Result, &workerResult); err != nil {
		t.Fatalf("decode persisted risk result: %v", err)
	}
	output, ok := workerResult["output"].(map[string]any)
	if !ok {
		t.Fatalf("stress output missing from completed API response: %s", completedRun.Result)
	}
	assertStressReconciles(t, output)
	assertPersistedScenarioRows(t, ctx, pool, riskRun.ID)

	decision := apiJSON[domainjournal.Decision](t, api.Client(), http.MethodPost, api.URL+"/api/v1/decisions", domainjournal.CreateRequest{
		AccountID: account.ID, Thesis: "Fixture thesis", Alternatives: []string{"Reduce exposure"},
		EvidenceReferences: []domainjournal.EvidenceRef{
			{Kind: "portfolio_snapshot", Reference: snapshot.ID},
			{Kind: "valuation_run", Reference: valuation.ID},
			{Kind: "risk_run", Reference: completedRun.ID},
		},
		InvalidationConditions: []domainjournal.Invalidation{{Condition: "Stress loss exceeds budget"}},
		Horizon:                domainjournal.Horizon{Start: cutoff, End: cutoff.AddDate(1, 0, 0)},
		RiskBudget:             domainjournal.RiskBudget{Amount: "2000", Currency: "TRY", Measure: "absolute_loss", Horizon: "12_months"},
		IntendedAction:         "Review monthly", Tags: []string{"e2e"}, Author: "ar601-fixture",
	}, http.StatusCreated)
	sealedDecision := apiJSON[domainjournal.Decision](t, api.Client(), http.MethodPost, api.URL+"/api/v1/decisions/"+decision.ID+"/finalize", map[string]any{}, http.StatusOK)
	if sealedDecision.Status != domainjournal.StatusFinalized {
		t.Fatalf("finalize decision=%+v", sealedDecision)
	}
	sealedBefore := apiJSON[evidence.Sealed](t, api.Client(), http.MethodGet, api.URL+"/api/v1/decisions/"+decision.ID+"/evidence", nil, http.StatusOK)
	apiJSON[domainjournal.Review](t, api.Client(), http.MethodPost, api.URL+"/api/v1/decisions/"+decision.ID+"/reviews", domainjournal.ReviewRequest{Review: "Fixture review", Outcome: "Continue monitoring", Author: "ar601-reviewer"}, http.StatusCreated)
	sealedAfter := apiJSON[evidence.Sealed](t, api.Client(), http.MethodGet, api.URL+"/api/v1/decisions/"+decision.ID+"/evidence", nil, http.StatusOK)
	if sealedBefore.SHA256 != sealedAfter.SHA256 || !bytes.Equal(sealedBefore.Manifest, sealedAfter.Manifest) {
		t.Fatalf("review changed sealed evidence: before=%s after=%s", sealedBefore.SHA256, sealedAfter.SHA256)
	}
	timeline := apiJSON[domainjournal.Timeline](t, api.Client(), http.MethodGet, api.URL+"/api/v1/decisions/"+decision.ID+"/timeline", nil, http.StatusOK)
	if len(timeline.Events) != 2 || timeline.Events[0].Kind != "decision" || timeline.Events[1].Kind != "review" {
		t.Fatalf("decision timeline=%+v", timeline)
	}

	// Missing required prices must remain blocked instead of producing a healthy valuation.
	unpriced := apiJSON[domainportfolio.Instrument](t, api.Client(), http.MethodPost, api.URL+"/api/v1/instruments", applicationportfolio.CreateInstrumentRequest{
		CanonicalSymbol: "AR601-JOURNEY-UNPRICED-" + strings.ReplaceAll(runID, "-", ""), InstrumentType: domainportfolio.InstrumentManualSpot, NativeUnit: "USD",
	}, http.StatusCreated)
	blockedSnapshot := apiJSON[domainportfolio.Snapshot](t, api.Client(), http.MethodPost, api.URL+"/api/v1/portfolios/"+portfolio.ID+"/snapshots", applicationportfolio.CreateSnapshotRequest{
		CapturedAt: cutoff, Lines: []applicationportfolio.SnapshotLineInput{{AccountID: account.ID, InstrumentID: unpriced.ID, Quantity: "1"}},
	}, http.StatusCreated)
	blocked := apiJSON[domainvaluation.Run](t, api.Client(), http.MethodPost, api.URL+"/api/v1/valuations", domainvaluation.Request{
		SnapshotID: blockedSnapshot.ID, Cutoff: cutoff, KnowledgeMode: domainvaluation.KnowledgeSystem,
		KnownAt: knownAt, PriceMaxAgeSeconds: 0, FXMaxAgeSeconds: 0,
	}, http.StatusCreated)
	if blocked.State != domainvaluation.StateBlocked {
		t.Fatalf("missing-price valuation state=%q", blocked.State)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(workingDirectory, "..", ".."))
}

func journeyDatabase(t *testing.T, root string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("ATLASRISK_TEST_DATABASE_URL")
	if err := database.ValidateIsolatedTestDatabaseURL(dsn); err != nil {
		if os.Getenv("ATLASRISK_REQUIRE_TEST_DATABASE") == "1" {
			t.Fatalf("isolated database validation failed: %v", err)
		}
		t.Skipf("isolated database unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	if err := database.Migrate(ctx, dsn, filepath.Join(root, "db", "migrations")); err != nil {
		t.Fatalf("migrate isolated database: %v", err)
	}
	pool, err := database.OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open isolated database: %v", err)
	}
	return pool
}

func journeyArchive(t *testing.T, ctx context.Context) *archive.S3Store {
	t.Helper()
	store, err := archive.NewS3StoreFromConfig(ctx, archive.ClientConfig{
		Endpoint: os.Getenv("ATLASRISK_S3_ENDPOINT"), Region: os.Getenv("ATLASRISK_S3_REGION"),
		AccessKeyID: os.Getenv("AWS_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		Bucket: os.Getenv("ATLASRISK_S3_BUCKET"),
	})
	if err != nil {
		t.Fatalf("configure isolated S3 archive: %v", err)
	}
	return store
}

func journeyAPI(pool *pgxpool.Pool, store archive.Store) *httptest.Server {
	portfolio := apiportfolio.New(applicationportfolio.Service{Queries: database.New(pool), Beginner: pool})
	imports := apiimports.New(applicationimports.Service{Pool: pool, Archive: store})
	valuation := apivaluation.New(applicationvaluation.Service{Pool: pool})
	risk := apirisk.New(applicationrisk.Service{Pool: pool})
	journal := apijournal.New(applicationjournal.Service{Pool: pool, Archive: store})
	mux := http.NewServeMux()
	for _, path := range []string{"/api/v1/instruments", "/api/v1/instruments/", "/api/v1/portfolios", "/api/v1/portfolios/"} {
		mux.Handle(path, portfolio)
	}
	mux.Handle("/api/v1/imports/", imports)
	mux.Handle("/api/v1/valuations", valuation)
	mux.Handle("/api/v1/valuations/", valuation)
	mux.Handle("/api/v1/risk/runs", risk)
	mux.Handle("/api/v1/risk/runs/", risk)
	mux.Handle("/api/v1/decisions", journal)
	mux.Handle("/api/v1/decisions/", journal)
	return httptest.NewServer(mux)
}

func apiJSON[T any](t *testing.T, client *http.Client, method, url string, body any, expectedStatus int) T {
	return apiJSONWithHeader[T](t, client, method, url, body, "", "", expectedStatus)
}

func apiJSONWithHeader[T any](t *testing.T, client *http.Client, method, url string, body any, header, value string, expectedStatus int) T {
	t.Helper()
	var encoded bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&encoded).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	request, err := http.NewRequest(method, url, &encoded)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if header != "" {
		request.Header.Set(header, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("%s %s status=%d want=%d body=%s", method, url, response.StatusCode, expectedStatus, data)
	}
	var result T
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode %s %s response: %v", method, url, err)
	}
	return result
}

func submitRiskRunAPI(t *testing.T, api *httptest.Server, pool *pgxpool.Pool, body applicationrisk.SubmitRequest, idempotencyKey string) applicationrisk.Run {
	t.Helper()
	var encoded bytes.Buffer
	if err := json.NewEncoder(&encoded).Encode(body); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, api.URL+"/api/v1/risk/runs", &encoded)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", idempotencyKey)
	response, err := api.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(response.Body)
		body.IdempotencyKey = idempotencyKey
		_, diagnosticErr := (applicationrisk.Service{Pool: pool}).Submit(context.Background(), body)
		t.Fatalf("POST %s status=%d want=%d body=%s; direct service diagnostic=%v", request.URL, response.StatusCode, http.StatusAccepted, data, diagnosticErr)
	}
	var result applicationrisk.Run
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode risk submit response: %v", err)
	}
	return result
}

func apiImport(t *testing.T, client *http.Client, baseURL, action, targetID string, content []byte, token string, expectedStatus int, idempotencyKey ...string) applicationimports.PreviewResponse {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	_ = writer.WriteField("schema_version", applicationimports.SchemaVersion)
	_ = writer.WriteField("target_id", targetID)
	if token != "" {
		_ = writer.WriteField("token", token)
	}
	part, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": []string{`form-data; name="file"; filename="journey-prices.csv"`},
		"Content-Type":        []string{"text/csv"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/v1/imports/"+applicationimports.KindManualPrices+"/"+action, &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if len(idempotencyKey) != 0 {
		request.Header.Set("Idempotency-Key", idempotencyKey[0])
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("manual price %s status=%d want=%d body=%s", action, response.StatusCode, expectedStatus, data)
	}
	var result applicationimports.PreviewResponse
	if action == "preview" {
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func journeyPricesCSV(t *testing.T, instrumentID string, cutoff time.Time, fixture journeyFixture) []byte {
	t.Helper()
	cutoff = cutoff.UTC()
	dates := make([]time.Time, 0, fixture.BusinessDayObservations)
	for day := cutoff; len(dates) < fixture.BusinessDayObservations; day = day.AddDate(0, 0, -1) {
		if day.Weekday() != time.Saturday && day.Weekday() != time.Sunday {
			dates = append(dates, day)
		}
	}
	var data bytes.Buffer
	writer := csv.NewWriter(&data)
	if err := writer.Write([]string{"instrument_id", "quote_currency", "observation_time", "price", "source_known_at"}); err != nil {
		t.Fatal(err)
	}
	for i := len(dates) - 1; i >= 0; i-- {
		observation := dates[i]
		sequence := len(dates) - 1 - i
		price := fixture.BasePrice + sequence*fixture.DailyIncrement
		if err := writer.Write([]string{instrumentID, "USD", observation.Format(time.RFC3339), fmt.Sprintf("%d", price), observation.Format(time.RFC3339)}); err != nil {
			t.Fatal(err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func assertRevisedSourceAsOf(t *testing.T, ctx context.Context, api *httptest.Server, pool *pgxpool.Pool, instrumentID, portfolioID string, cutoff time.Time, runID string, initialCSV []byte) {
	t.Helper()
	records, err := csv.NewReader(bytes.NewReader(initialCSV)).ReadAll()
	if err != nil || len(records) < 2 {
		t.Fatalf("read deterministic price fixture: records=%d err=%v", len(records), err)
	}
	observation, err := time.Parse(time.RFC3339, records[1][2])
	if err != nil {
		t.Fatal(err)
	}
	sourceAsOf, err := time.Parse(time.RFC3339, records[1][4])
	if err != nil {
		t.Fatal(err)
	}
	var knownBefore time.Time
	if err := pool.QueryRow(ctx, `SELECT system_known_at FROM price_revisions WHERE instrument_id=$1::uuid AND observation_time=$2 AND price=$3 ORDER BY system_known_at,id LIMIT 1`, instrumentID, observation, records[1][3]).Scan(&knownBefore); err != nil {
		t.Fatalf("capture original revision system time: %v", err)
	}
	revisedSourceAt := cutoff.Add(time.Hour)
	var revised bytes.Buffer
	writer := csv.NewWriter(&revised)
	if err := writer.Write([]string{"instrument_id", "quote_currency", "observation_time", "price", "source_known_at"}); err != nil {
		t.Fatal(err)
	}
	if err := writer.Write([]string{instrumentID, "USD", observation.Format(time.RFC3339), "101", revisedSourceAt.Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		t.Fatal(err)
	}
	preview := apiImport(t, api.Client(), api.URL, "preview", portfolioID, revised.Bytes(), "", http.StatusOK)
	apiImport(t, api.Client(), api.URL, "commit", portfolioID, revised.Bytes(), preview.Token, http.StatusCreated, "ar601-journey-revision-"+runID)
	oldSource := priceAtSourceAsOf(t, ctx, pool, instrumentID, observation, sourceAsOf)
	newSource := priceAtSourceAsOf(t, ctx, pool, instrumentID, observation, revisedSourceAt.Add(time.Hour))
	oldSystem := priceAtSystemAsOf(t, ctx, pool, instrumentID, observation, knownBefore)
	newSystem := priceAtSystemAsOf(t, ctx, pool, instrumentID, observation, time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC))
	if !decimalStringsEqual(oldSource, records[1][3]) || !decimalStringsEqual(oldSystem, records[1][3]) || !decimalStringsEqual(newSource, "101") || !decimalStringsEqual(newSystem, "101") {
		t.Fatalf("revised observation history source(old/new)=%s/%s system(old/new)=%s/%s", oldSource, newSource, oldSystem, newSystem)
	}
}

func decimalStringsEqual(left, right string) bool {
	leftRat, leftOK := new(big.Rat).SetString(left)
	rightRat, rightOK := new(big.Rat).SetString(right)
	return leftOK && rightOK && leftRat.Cmp(rightRat) == 0
}

func priceAtSourceAsOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instrumentID string, observation, sourceAsOf time.Time) string {
	t.Helper()
	var price string
	err := pool.QueryRow(ctx, `SELECT price::text FROM price_revisions WHERE instrument_id=$1::uuid AND observation_time=$2 AND source_known_at <= $3 ORDER BY source_known_at DESC,system_known_at DESC,id DESC LIMIT 1`, instrumentID, observation, sourceAsOf).Scan(&price)
	if err != nil {
		t.Fatalf("query source-as-of price: %v", err)
	}
	return price
}

func priceAtSystemAsOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, instrumentID string, observation, systemAsOf time.Time) string {
	t.Helper()
	var price string
	err := pool.QueryRow(ctx, `SELECT price::text FROM price_revisions WHERE instrument_id=$1::uuid AND observation_time=$2 AND system_known_at <= $3 ORDER BY system_known_at DESC,id DESC LIMIT 1`, instrumentID, observation, systemAsOf).Scan(&price)
	if err != nil {
		t.Fatalf("query system-as-of price: %v", err)
	}
	return price
}

func runRiskEngineWorker(t *testing.T, root string) bool {
	t.Helper()
	python := `import os, psycopg; from atlasrisk.jobs.postgres import PostgresQueueClient; from atlasrisk.jobs.worker import JobWorker; conn=psycopg.connect(os.environ["ATLASRISK_TEST_DATABASE_URL"]); worked=JobWorker({}, engine_version="atlasrisk-risk-engine-0.1.0").run_claimed_once(PostgresQueueClient(conn), "ar601-e2e", 120); conn.close(); print("true" if worked else "false")`
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "uv", "run", "--locked", "python", "-c", python)
	command.Dir = filepath.Join(root, "risk-engine")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run PostgreSQL-backed risk worker: %v\n%s", err, output)
	}
	return strings.TrimSpace(string(output)) == "true"
}

func assertStressReconciles(t *testing.T, output map[string]any) {
	t.Helper()
	total := exactDecimal(t, output["portfolio_pnl_try"])
	if len(anySlice(output["positions"])) != 1 {
		t.Fatalf("stress position count=%d, want 1", len(anySlice(output["positions"])))
	}
	if want := new(big.Rat).SetInt64(-1304); total.Cmp(want) != 0 {
		t.Fatalf("stress total=%s TRY, want -1304 TRY", total.RatString())
	}
	positions := new(big.Rat)
	for _, raw := range anySlice(output["positions"]) {
		position := raw.(map[string]any)
		positions.Add(positions, exactDecimal(t, position["pnl_try"]))
	}
	if total.Cmp(positions) != 0 {
		t.Fatalf("stress total %s does not equal position sum %s", total.RatString(), positions.RatString())
	}
	attribution, ok := output["attribution"].(map[string]any)
	if !ok || attribution["reconciles"] != true {
		t.Fatalf("stress attribution does not reconcile: %v", output["attribution"])
	}
	allocated := new(big.Rat)
	factors := anySlice(attribution["factor_contributions"])
	if len(factors) != 1 {
		t.Fatalf("stress factor count=%d, want 1", len(factors))
	}
	for _, raw := range factors {
		factor := raw.(map[string]any)
		allocated.Add(allocated, exactDecimal(t, factor["contribution"]))
	}
	residual := exactDecimal(t, attribution["interaction_residual"])
	allocated.Add(allocated, residual)
	if total.Cmp(allocated) != 0 {
		t.Fatalf("stress factors %s plus residual %s do not equal total %s", allocated.RatString(), residual.RatString(), total.RatString())
	}
}

func assertPersistedScenarioRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, runID string) {
	t.Helper()
	var positions, factors, positionFactors, metrics int
	err := pool.QueryRow(ctx, `
		SELECT
		 (SELECT count(*) FROM scenario_run_positions WHERE run_id=$1::uuid),
		 (SELECT count(*) FROM scenario_run_factor_attributions WHERE run_id=$1::uuid),
		 (SELECT count(*) FROM scenario_run_position_attributions WHERE run_id=$1::uuid),
		 (SELECT count(*) FROM scenario_run_metrics WHERE run_id=$1::uuid)
	`, runID).Scan(&positions, &factors, &positionFactors, &metrics)
	if err != nil {
		t.Fatalf("read persisted scenario evidence: %v", err)
	}
	if positions != 1 || factors != 1 || positionFactors != 1 || metrics == 0 {
		t.Fatalf("persisted scenario rows positions=%d factors=%d position_factors=%d metrics=%d", positions, factors, positionFactors, metrics)
	}
}

func exactDecimal(t *testing.T, value any) *big.Rat {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("decimal has type %T, want string", value)
	}
	result, ok := new(big.Rat).SetString(text)
	if !ok {
		t.Fatalf("invalid decimal %q", text)
	}
	return result
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func stringSlice(value any) []string {
	items := anySlice(value)
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, _ := item.(string)
		result = append(result, text)
	}
	return result
}
