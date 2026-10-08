package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apirisk "github.com/oplosy/atrisk/apps/api/handlers/risk"
	applicationrisk "github.com/oplosy/atrisk/internal/application/risk"
	applicationscenarios "github.com/oplosy/atrisk/internal/application/scenarios"
	"github.com/oplosy/atrisk/internal/jobs"
)

func TestRiskJobLifecycleRepairAttemptFence(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	queue := jobs.Queue{Pool: pool}

	job := enqueueRiskJob(t, queue, riskJobKey(t, "repair-fence"), 3)
	first, err := queue.Claim(ctx, "repair-worker", 20*time.Millisecond)
	if err != nil || first == nil {
		t.Fatalf("claim first attempt: claim=%v err=%v", first, err)
	}
	time.Sleep(50 * time.Millisecond)
	if renewed, err := queue.Renew(ctx, *first, time.Minute); err != nil || renewed {
		t.Fatalf("expired first attempt was renewed: renewed=%v err=%v", renewed, err)
	}
	if recovered, err := queue.RecoverExpired(ctx); err != nil || recovered != 1 {
		t.Fatalf("recover first attempt: recovered=%d err=%v", recovered, err)
	}
	second, err := queue.Claim(ctx, "repair-worker", time.Minute)
	if err != nil || second == nil || second.AttemptCount != first.AttemptCount+1 {
		t.Fatalf("reclaim attempt identity: second=%v err=%v", second, err)
	}
	if completed, err := queue.Complete(ctx, *first, map[string]any{"stale": true}); err != nil || completed {
		t.Fatalf("stale same-worker completion accepted: completed=%v err=%v", completed, err)
	}
	if renewed, err := queue.Renew(ctx, *first, time.Minute); err != nil || renewed {
		t.Fatalf("stale same-worker renewal accepted: renewed=%v err=%v", renewed, err)
	}
	if completed, err := queue.Complete(ctx, *second, map[string]any{"attempt": second.AttemptCount}); err != nil || !completed {
		t.Fatalf("current attempt completion failed: completed=%v err=%v", completed, err)
	}

	var outcomes int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM risk_job_attempts WHERE job_id=$1::uuid AND outcome='expired'`, job.ID).Scan(&outcomes); err != nil {
		t.Fatal(err)
	}
	if outcomes != 1 {
		t.Fatalf("expected one expired attempt, got %d", outcomes)
	}
}

func TestRiskEndToEndRepairRejectsNumericShockAtomically(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	accountID, snapshotID, valuationID, _, _ := createRiskAPIFixture(t, pool)
	handler := apirisk.New(applicationrisk.Service{Pool: pool})
	baseRequest := map[string]any{
		"account_id": accountID, "snapshot_id": snapshotID, "valuation_id": valuationID,
		"name": "repair-numeric", "template_key": "risk_off",
		"units":    map[string]any{"reporting_currency": "TRY"},
		"mappings": map[string]any{}, "assumptions": map[string]any{"coverage_policy": "block"},
	}
	for name, shock := range map[string]any{
		"numeric":   map[string]any{"asset_class_returns": map[string]any{"crypto": -0.4}},
		"oversized": map[string]any{"correlation_target": "123456789012345678901.0"},
	} {
		request := make(map[string]any, len(baseRequest)+1)
		for key, value := range baseRequest {
			request[key] = value
		}
		request["shocks"] = shock
		key := riskJobKey(t, "repair-"+name)
		response := submitRiskRequest(t, handler, request, key)
		if response != http.StatusBadRequest {
			t.Fatalf("%s shock status=%d", name, response)
		}
		var jobsCreated int
		if err := pool.QueryRow(context.Background(), `SELECT count(*)::int FROM risk_jobs WHERE idempotency_key=$1`, key).Scan(&jobsCreated); err != nil {
			t.Fatal(err)
		}
		if jobsCreated != 0 {
			t.Fatalf("invalid %s shock created %d jobs", name, jobsCreated)
		}
	}
}

func TestRiskEndToEndRepairCashUSDMetricBundle(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	var portfolioID, accountID, snapshotID, instrumentID, lineID, valuationID string
	if err := pool.QueryRow(ctx, `INSERT INTO portfolios (name,reporting_currency) VALUES ('repair-cash-'||gen_random_uuid()::text,'TRY') RETURNING id::text`).Scan(&portfolioID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (portfolio_id,name) VALUES ($1::uuid,'repair-cash-account') RETURNING id::text`, portfolioID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO instruments (canonical_symbol,instrument_type,native_currency) VALUES ('REPAIR-CASH-'||gen_random_uuid()::text,'cash','USD') RETURNING id::text`).Scan(&instrumentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO portfolio_snapshots (portfolio_id,captured_at) VALUES ($1::uuid,clock_timestamp()) RETURNING id::text`, portfolioID).Scan(&snapshotID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO portfolio_snapshot_lines (portfolio_id,snapshot_id,account_id,instrument_id,quantity) VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,1) RETURNING id::text`, portfolioID, snapshotID, accountID, instrumentID).Scan(&lineID); err != nil {
		t.Fatal(err)
	}
	cutoff := "2026-01-01T00:00:00Z"
	if err := pool.QueryRow(ctx, `INSERT INTO valuation_runs (snapshot_id,cutoff,knowledge_mode,known_at,price_max_age_seconds,fx_max_age_seconds,request,state,result_hash) VALUES ($1::uuid,$2::timestamptz,'system_as_of',$2::timestamptz,0,0,'{}','valid',repeat('0',64)) RETURNING id::text`, snapshotID, cutoff).Scan(&valuationID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO valuation_lines (run_id,snapshot_line_id,native_currency,native_amount,try_amount,usd_amount,state,reason_codes,price_method) VALUES ($1::uuid,$2::uuid,'USD','1','40','1','valid','[]','identity')`, valuationID, lineID); err != nil {
		t.Fatal(err)
	}
	var foreignInstrumentID, foreignLineID string
	if err := pool.QueryRow(ctx, `INSERT INTO instruments (canonical_symbol,instrument_type,native_currency) VALUES ('REPAIR-EUR-CASH-'||gen_random_uuid()::text,'cash','EUR') RETURNING id::text`).Scan(&foreignInstrumentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO portfolio_snapshot_lines (portfolio_id,snapshot_id,account_id,instrument_id,quantity) VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,1) RETURNING id::text`, portfolioID, snapshotID, accountID, foreignInstrumentID).Scan(&foreignLineID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO valuation_lines (run_id,snapshot_line_id,native_currency,native_amount,try_amount,usd_amount,state,reason_codes,price_method) VALUES ($1::uuid,$2::uuid,'EUR','1','40','1','valid','[]','identity')`, valuationID, foreignLineID); err != nil {
		t.Fatal(err)
	}
	run, err := (applicationscenarios.Service{Pool: pool}).CreateVersionAndRun(ctx, applicationscenarios.VersionInput{
		AccountID: accountID, SnapshotID: snapshotID, ValuationID: valuationID,
		Name: "repair-cash", TemplateKey: "risk_off", IdempotencyKey: riskJobKey(t, "repair-cash"),
		Units: map[string]any{"reporting_currency": "TRY"},
		Shocks: map[string]any{
			"fx_pair_changes": map[string]any{}, "asset_class_returns": map[string]any{"cash": "0"},
			"yield_shifts_bps": map[string]any{}, "volatility_multipliers": map[string]any{},
			"correlation_target": nil, "correlation_blend": nil,
		},
		Mappings: map[string]any{}, Assumptions: map[string]any{"coverage_policy": "block"},
	})
	if err != nil {
		t.Fatalf("create USD cash scenario: %v", err)
	}
	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM risk_jobs WHERE id=$1::uuid`, run.JobID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(payload, &envelope); err != nil {
		t.Fatal(err)
	}
	metricInputs, ok := envelope["metric_inputs"].(map[string]any)
	if !ok {
		t.Fatalf("metric inputs missing from cash payload: %#v", envelope)
	}
	if _, ok := metricInputs["price_history"].(map[string]any); !ok {
		t.Fatalf("USD cash price history missing: %#v", metricInputs)
	}
	if _, ok := metricInputs["cash_revision_history"].(map[string]any); !ok {
		t.Fatalf("USD cash provenance missing: %#v", metricInputs)
	}
	if _, ok := metricInputs["constant_instruments"].([]any); !ok {
		t.Fatalf("USD cash constant instrument marker missing: %#v", metricInputs)
	}
	if _, ok := metricInputs["nav_history"]; ok {
		t.Fatalf("missing EUR FX unexpectedly produced NAV history: %#v", metricInputs)
	}
	if preMetrics, ok := envelope["pre_metrics"].(map[string]any); !ok || preMetrics["data_quality"] != "blocked" {
		t.Fatalf("mixed cash without FX did not fail closed: %#v", envelope["pre_metrics"])
	}
}

func submitRiskRequest(t *testing.T, handler http.Handler, payload map[string]any, key string) int {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/risk/runs", bytes.NewReader(data))
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code
}
