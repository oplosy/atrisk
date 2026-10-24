package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oplosy/atrisk/internal/application/evidence"
	applicationjournal "github.com/oplosy/atrisk/internal/application/journal"
	domain "github.com/oplosy/atrisk/internal/domain/journal"
)

func TestDecisionJournalAPIRepairRejectsRiskValuationMismatch(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	portfolioID, accountID := repairPortfolio(t, pool)
	_, refs := decisionEvidenceFixture(t, pool, portfolioID, accountID)
	var alternateValuationID string
	if err := pool.QueryRow(ctx, `INSERT INTO valuation_runs (snapshot_id,cutoff,knowledge_mode,known_at,price_max_age_seconds,fx_max_age_seconds,request,state,result_hash)
		VALUES ($1::uuid,'2026-01-01T00:00:00Z','system_as_of','2026-01-01T00:00:00Z',3600,3600,'{}','valid',$2) RETURNING id::text`, refs[0].Reference, strings.Repeat("9", 64)).Scan(&alternateValuationID); err != nil {
		t.Fatal(err)
	}
	riskID := insertRepairRiskRun(t, pool, refs[0].Reference, accountID, refs[0].Reference, alternateValuationID, "{}")
	refs[2].Reference = riskID
	service := applicationjournal.Service{Pool: pool, Archive: &evidenceArchive{objects: map[string][]byte{}}}
	decision, err := service.Create(ctx, repairDecision(accountID, refs))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Finalize(ctx, decision.ID); !errors.Is(err, applicationjournal.ErrConflict) {
		t.Fatalf("mismatched risk valuation finalized: %v", err)
	}
	current, err := service.Get(ctx, decision.ID)
	if err != nil || current.Status != domain.StatusDraft {
		t.Fatalf("mismatch changed draft: status=%q err=%v", current.Status, err)
	}
}

func TestDecisionJournalAPIRepairRejectsNullRiskValuation(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	portfolioID, accountID := repairPortfolio(t, pool)
	_, refs := decisionEvidenceFixture(t, pool, portfolioID, accountID)
	riskID := insertRepairRiskRun(t, pool, refs[0].Reference, accountID, refs[0].Reference, "", "{}")
	refs[2].Reference = riskID
	service := applicationjournal.Service{Pool: pool, Archive: &evidenceArchive{objects: map[string][]byte{}}}
	decision, err := service.Create(ctx, repairDecision(accountID, refs))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Finalize(ctx, decision.ID); !errors.Is(err, applicationjournal.ErrConflict) {
		t.Fatalf("NULL risk valuation finalized: %v", err)
	}
}

func TestDecisionJournalAPIRepairRejectsExplicitNullMetricProvenance(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	portfolioID, accountID := repairPortfolio(t, pool)
	for _, provenance := range []string{
		`{"metric_inputs":null}`,
		`{"metric_inputs":{"price_history":null}}`,
		`{"metric_inputs":{"price_revision_history":null}}`,
	} {
		_, refs := decisionEvidenceFixture(t, pool, portfolioID, accountID)
		riskID := insertRepairRiskRun(t, pool, refs[0].Reference, accountID, refs[0].Reference, refs[1].Reference, provenance)
		refs[2].Reference = riskID
		service := applicationjournal.Service{Pool: pool, Archive: &evidenceArchive{objects: map[string][]byte{}}}
		decision, err := service.Create(ctx, repairDecision(accountID, refs))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Finalize(ctx, decision.ID); !errors.Is(err, applicationjournal.ErrConflict) {
			t.Fatalf("explicit malformed provenance finalized (%s): %v", provenance, err)
		}
		current, err := service.Get(ctx, decision.ID)
		if err != nil || current.Status != domain.StatusDraft {
			t.Fatalf("explicit malformed provenance changed draft (%s): status=%q err=%v", provenance, current.Status, err)
		}
	}
}

func TestDecisionJournalAPIRepairClosesHistoricalMetricRawDependencies(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	portfolioID, accountID := repairPortfolio(t, pool)
	_, refs := decisionEvidenceFixture(t, pool, portfolioID, accountID)
	var instrumentID string
	if err := pool.QueryRow(ctx, `INSERT INTO instruments (canonical_symbol,instrument_type,native_currency) VALUES ('AR705-HISTORY-'||replace(gen_random_uuid()::text,'-',''),'crypto_spot','USD') RETURNING id::text`).Scan(&instrumentID); err != nil {
		t.Fatal(err)
	}
	content := []byte("historical metric source:" + instrumentID)
	digest := sha256.Sum256(content)
	key := "ar705-history-" + instrumentID
	var rawID string
	if err := pool.QueryRow(ctx, `INSERT INTO raw_objects (content_sha256,object_key,media_type,byte_length,retrieved_at) VALUES ($1,$2,'text/plain',$3,clock_timestamp()) RETURNING id::text`, hex.EncodeToString(digest[:]), key, len(content)).Scan(&rawID); err != nil {
		t.Fatal(err)
	}
	var priceID string
	var observationAt, systemKnownAt time.Time
	if err := pool.QueryRow(ctx, `INSERT INTO price_revisions (instrument_id,quote_currency,observation_time,price,knowledge_time_basis,raw_object_id)
		VALUES ($1,'USD','2026-01-01T00:00:00Z','100','first_observed_by_system',$2::uuid)
		RETURNING id::text,observation_time,system_known_at`, instrumentID, rawID).Scan(&priceID, &observationAt, &systemKnownAt); err != nil {
		t.Fatal(err)
	}
	provenance, err := json.Marshal(map[string]any{"metric_inputs": map[string]any{
		"price_history": map[string]any{instrumentID: map[string]string{"2026-01-01": "100.000000000000000000"}},
		"price_revision_history": map[string]any{instrumentID: []map[string]any{{
			"id": priceID, "observation_time": observationAt.UTC().Format(time.RFC3339Nano), "price": "100.000000000000000000", "price_quote_currency": "USD",
			"usd_price": "100.000000000000000000", "source_known_at": nil, "system_known_at": systemKnownAt.UTC().Format(time.RFC3339Nano),
			"knowledge_time_basis": "first_observed_by_system", "fx_path": []any{},
		}}},
		"signed_exposures": map[string]string{instrumentID: "100"}, "calendar": "crypto_daily",
	}})
	if err != nil {
		t.Fatal(err)
	}
	riskID := insertRepairRiskRun(t, pool, refs[0].Reference, accountID, refs[0].Reference, refs[1].Reference, string(provenance))
	refs[2].Reference = riskID
	archiveStore := &evidenceArchive{objects: map[string][]byte{key: content}}
	service := applicationjournal.Service{Pool: pool, Archive: archiveStore}
	decision, err := service.Create(ctx, repairDecision(accountID, refs))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sealErr := (evidence.Service{Pool: pool, Archive: archiveStore}).Seal(ctx, tx, decision.ID, accountID, refs)
	_ = tx.Rollback(ctx)
	if sealErr != nil {
		t.Fatalf("historical metric seal detail: %v", sealErr)
	}
	delete(archiveStore.objects, key)
	if _, err := service.Finalize(ctx, decision.ID); !errors.Is(err, applicationjournal.ErrConflict) {
		t.Fatalf("missing historical raw source finalized: %v", err)
	}
	if current, err := service.Get(ctx, decision.ID); err != nil || current.Status != domain.StatusDraft {
		t.Fatalf("missing historical source changed draft: status=%q err=%v", current.Status, err)
	}
	archiveStore.objects[key] = content
	if _, err := service.Finalize(ctx, decision.ID); err != nil {
		t.Fatal(err)
	}
	sealed, err := service.Evidence(ctx, decision.ID)
	if err != nil {
		t.Fatal(err)
	}
	var manifest evidence.Manifest
	if err := json.Unmarshal(sealed.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range manifest.RawObjects {
		if raw.ID == rawID {
			found = true
		}
	}
	if !found {
		t.Fatalf("historical metric raw object %s missing from manifest: %+v", rawID, manifest.RawObjects)
	}
	before := append([]byte(nil), sealed.Manifest...)
	archiveStore.objects[key] = []byte("changed historical metric source")
	if _, err := service.Evidence(ctx, decision.ID); !errors.Is(err, evidence.ErrIntegrity) {
		t.Fatalf("corrupt historical raw source: %v", err)
	}
	archiveStore.objects[key] = content
	after, err := service.Evidence(ctx, decision.ID)
	if err != nil || !bytes.Equal(before, after.Manifest) {
		t.Fatalf("sealed historical manifest changed: err=%v", err)
	}
}

func TestDecisionJournalAPIRepairBindsCashMetricHistoryToInstrument(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	portfolioID, accountID := repairPortfolio(t, pool)
	cashTRY := repairInstrument(t, pool, "cash", "TRY")
	cashUSD := repairInstrument(t, pool, "currency", "USD")
	stockUSD := repairInstrument(t, pool, "crypto_spot", "USD")
	fixtureSuffix := time.Now().UTC().Format("20060102T150405.999999999")
	fxContent := []byte("ar705 cash FX source:" + fixtureSuffix)
	fxDigest := sha256.Sum256(fxContent)
	fxKey := "ar705-cash-fx-" + fixtureSuffix
	var fxRawID, fxID string
	if err := pool.QueryRow(ctx, `INSERT INTO raw_objects (content_sha256,object_key,media_type,byte_length,retrieved_at) VALUES ($1,$2,'text/plain',$3,clock_timestamp()) RETURNING id::text`, hex.EncodeToString(fxDigest[:]), fxKey, len(fxContent)).Scan(&fxRawID); err != nil {
		t.Fatal(err)
	}
	var observationAt, systemKnownAt time.Time
	if err := pool.QueryRow(ctx, `INSERT INTO fx_quote_revisions (base_currency,quote_currency,observation_time,rate,knowledge_time_basis,raw_object_id)
		VALUES ('USD','TRY','2026-01-02T00:00:00Z','30','first_observed_by_system',$1::uuid)
		RETURNING id::text,observation_time,system_known_at`, fxRawID).Scan(&fxID, &observationAt, &systemKnownAt); err != nil {
		t.Fatal(err)
	}
	fxEntry := map[string]any{
		"id": fxID, "pair": "USD/TRY", "direction": "inverse", "observation_time": observationAt.UTC().Format(time.RFC3339Nano),
		"rate": "30.000000000000000000", "source_known_at": nil, "system_known_at": systemKnownAt.UTC().Format(time.RFC3339Nano), "knowledge_time_basis": "first_observed_by_system",
	}
	cashEntry := map[string]any{
		"id": fxID, "observation_time": observationAt.UTC().Format(time.RFC3339Nano), "price": "1", "price_quote_currency": "TRY",
		"usd_price": "0.033333333333333333", "source_known_at": nil, "system_known_at": systemKnownAt.UTC().Format(time.RFC3339Nano),
		"knowledge_time_basis": "first_observed_by_system", "fx_path": []any{fxEntry},
	}
	archiveStore := &evidenceArchive{objects: map[string][]byte{fxKey: fxContent}}
	valid := jsonMetricInputs(cashTRY, "2026-01-02", "0.033333333333333333", map[string]any{cashTRY: []map[string]any{cashEntry}})
	runCashEvidenceCase(t, pool, portfolioID, accountID, valid, archiveStore, true)

	constant := func(instrumentID string) string {
		return jsonMetricInputs(instrumentID, "2026-01-02", "1", map[string]any{instrumentID: []map[string]any{{
			"id": "cash-constant:" + instrumentID + ":2026-01-02", "observation_time": "2026-01-02T00:00:00Z", "price": "1", "price_quote_currency": "USD", "usd_price": "1",
			"source_known_at": nil, "system_known_at": "0001-01-01T00:00:00Z", "knowledge_time_basis": "constant_cash", "fx_path": []any{},
		}}})
	}
	runCashEvidenceCase(t, pool, portfolioID, accountID, constant(cashUSD), archiveStore, true)
	for name, provenance := range map[string]string{
		"nonexistent constant instrument":  constant("00000000-0000-0000-0000-000000000001"),
		"noncash constant instrument":      constant(stockUSD),
		"nonUSD constant instrument":       constant(cashTRY),
		"nonexistent dated FX instrument":  jsonMetricInputs("00000000-0000-0000-0000-000000000002", "2026-01-02", "0.033333333333333333", map[string]any{"00000000-0000-0000-0000-000000000002": []map[string]any{cashEntry}}),
		"noncash dated FX instrument":      jsonMetricInputs(stockUSD, "2026-01-02", "0.033333333333333333", map[string]any{stockUSD: []map[string]any{cashEntry}}),
		"native mismatch dated FX":         jsonMetricInputs(cashUSD, "2026-01-02", "0.033333333333333333", map[string]any{cashUSD: []map[string]any{cashEntry}}),
		"ordinary price group FX fallback": jsonPriceMetricInputs(cashTRY, "2026-01-02", "0.033333333333333333", map[string]any{cashTRY: []map[string]any{cashEntry}}),
	} {
		t.Run(name, func(t *testing.T) {
			runCashEvidenceCase(t, pool, portfolioID, accountID, provenance, archiveStore, false)
		})
	}
}

func jsonMetricInputs(instrumentID, day, value string, history map[string]any) string {
	inputs := map[string]any{
		"price_history":         map[string]any{instrumentID: map[string]string{day: value}},
		"cash_revision_history": history,
	}
	if _, ok := history[instrumentID]; !ok {
		inputs["cash_revision_history"] = map[string]any{}
	}
	if priceHistory, ok := inputs["price_history"].(map[string]any); ok && priceHistory[instrumentID] == nil {
		delete(priceHistory, instrumentID)
	}
	data, err := json.Marshal(map[string]any{"metric_inputs": inputs})
	if err != nil {
		panic(err)
	}
	return string(data)
}

func jsonPriceMetricInputs(instrumentID, day, value string, history map[string]any) string {
	data, err := json.Marshal(map[string]any{"metric_inputs": map[string]any{
		"price_history":          map[string]any{instrumentID: map[string]string{day: value}},
		"price_revision_history": history,
	}})
	if err != nil {
		panic(err)
	}
	return string(data)
}

func runCashEvidenceCase(t *testing.T, pool *pgxpool.Pool, portfolioID, accountID, provenance string, archiveStore *evidenceArchive, wantSuccess bool) {
	t.Helper()
	ctx := context.Background()
	_, refs := decisionEvidenceFixture(t, pool, portfolioID, accountID)
	riskID := insertRepairRiskRun(t, pool, refs[0].Reference, accountID, refs[0].Reference, refs[1].Reference, provenance)
	refs[2].Reference = riskID
	service := applicationjournal.Service{Pool: pool, Archive: archiveStore}
	decision, err := service.Create(ctx, repairDecision(accountID, refs))
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Finalize(ctx, decision.ID)
	if wantSuccess {
		if err != nil {
			t.Fatalf("valid cash provenance rejected: %v", err)
		}
		return
	}
	if !errors.Is(err, applicationjournal.ErrConflict) {
		t.Fatalf("invalid cash provenance finalized: %v", err)
	}
	current, getErr := service.Get(ctx, decision.ID)
	if getErr != nil || current.Status != domain.StatusDraft {
		t.Fatalf("invalid cash provenance changed draft: status=%q err=%v", current.Status, getErr)
	}
}

func repairInstrument(t *testing.T, pool *pgxpool.Pool, instrumentType, nativeCurrency string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `INSERT INTO instruments (canonical_symbol,instrument_type,native_currency) VALUES ('ar705-'||gen_random_uuid()::text,$1,$2) RETURNING id::text`, instrumentType, nativeCurrency).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func repairPortfolio(t *testing.T, pool *pgxpool.Pool) (string, string) {
	t.Helper()
	ctx := context.Background()
	var portfolioID, accountID string
	if err := pool.QueryRow(ctx, `INSERT INTO portfolios (name,reporting_currency) VALUES ('ar705-'||gen_random_uuid()::text,'TRY') RETURNING id::text`).Scan(&portfolioID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (portfolio_id,name) VALUES ($1::uuid,'ar705-account-'||gen_random_uuid()::text) RETURNING id::text`, portfolioID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	return portfolioID, accountID
}

func insertRepairRiskRun(t *testing.T, pool *pgxpool.Pool, snapshotID, accountID, scenarioSnapshotID, valuationID, provenance string) string {
	t.Helper()
	ctx := context.Background()
	var scenarioID, jobID, riskID string
	if err := pool.QueryRow(ctx, `INSERT INTO scenarios (account_id,name,template_key) VALUES ($1::uuid,'ar705-scenario-'||gen_random_uuid()::text,'rates_up') RETURNING id::text`, accountID).Scan(&scenarioID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scenario_versions (scenario_id,version,template_key,units,shocks,assumptions,content_hash) VALUES ($1::uuid,1,'rates_up','{}','{}','{}',$2)`, scenarioID, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO risk_jobs (kind,schema_version,idempotency_key,input_snapshot_ids,payload,state,result,result_hash,completed_at)
		VALUES ('scenario','1',gen_random_uuid()::text,ARRAY[$1]::text[],'{}','succeeded','{"engine_version":"fixture-1"}',$2,clock_timestamp()) RETURNING id::text`, snapshotID, strings.Repeat("c", 64)).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	query := `INSERT INTO scenario_runs (scenario_id,scenario_version,account_id,snapshot_id,valuation_id,job_id,state,input_provenance,result,result_hash,request_hash,completed_at)
		VALUES ($1::uuid,1,$2::uuid,$3::uuid,NULLIF($4,'')::uuid,$5::uuid,'valid',$6::jsonb,'{"engine_version":"fixture-1"}',$7,$8,clock_timestamp()) RETURNING id::text`
	if err := pool.QueryRow(ctx, query, scenarioID, accountID, scenarioSnapshotID, valuationID, jobID, provenance, strings.Repeat("d", 64), strings.Repeat("e", 64)).Scan(&riskID); err != nil {
		t.Fatal(err)
	}
	return riskID
}

func repairDecision(accountID string, refs []domain.EvidenceRef) domain.CreateRequest {
	return domain.CreateRequest{
		AccountID: accountID, Thesis: "AR-705 repair", EvidenceReferences: refs,
		InvalidationConditions: []domain.Invalidation{{Condition: "Evidence changes"}},
		Horizon:                domain.Horizon{Start: mustTime("2026-01-01T00:00:00Z"), End: mustTime("2027-01-01T00:00:00Z")},
		RiskBudget:             domain.RiskBudget{Amount: "100", Currency: "TRY", Measure: "loss", Horizon: "one_year"},
		IntendedAction:         "Hold", Author: "ar705-fixture",
	}
}
