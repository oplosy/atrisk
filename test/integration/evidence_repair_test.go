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
		"price_history": map[string]any{instrumentID: map[string]string{"2026-01-01": "100"}},
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
