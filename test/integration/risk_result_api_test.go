package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	apirisk "github.com/oplosy/atrisk/apps/api/handlers/risk"
	applicationrisk "github.com/oplosy/atrisk/internal/application/risk"
)

func TestRiskEndToEnd(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	accountID, snapshotID, lineID, instrumentID := createRiskAPIFixture(t, pool)
	handler := apirisk.New(applicationrisk.Service{Pool: pool})

	request := map[string]any{
		"account_id": accountID, "snapshot_id": snapshotID, "name": "risk-api-e2e",
		"template_key": "risk_off", "units": map[string]any{"reporting_currency": "TRY"},
		"shocks": map[string]any{"crypto_return": "-0.40"}, "mappings": map[string]any{},
		"assumptions": map[string]any{"coverage_policy": "block"},
		"positions":   []map[string]any{{"snapshot_line_id": lineID, "instrument_id": instrumentID}},
	}
	first := submitRiskRun(t, handler, request, "risk-api-e2e-key")
	replay := submitRiskRun(t, handler, request, "risk-api-e2e-key")
	if first.ID != replay.ID || first.JobID != replay.JobID || first.ScenarioVersion != replay.ScenarioVersion {
		t.Fatalf("idempotent submission created different runs: first=%+v replay=%+v", first, replay)
	}
	if first.SnapshotID != snapshotID || first.AccountID != accountID || first.ScenarioTemplate != "risk_off" {
		t.Fatalf("submission lost provenance: %+v", first)
	}

	get := requestRisk(t, handler, http.MethodGet, "/api/v1/risk/runs/"+first.ID, nil)
	if get.Code != http.StatusOK {
		t.Fatalf("get risk run status=%d body=%s", get.Code, get.Body.String())
	}
	var queued applicationrisk.Run
	decodeRisk(t, get, &queued)
	if queued.Status != "queued" || queued.SnapshotID != snapshotID || queued.EngineVersion == "" || queued.SchemaVersion == "" {
		t.Fatalf("queued status/provenance=%+v", queued)
	}
	status := requestRisk(t, handler, http.MethodGet, "/api/v1/risk/runs/"+first.ID+"/status", nil)
	if status.Code != http.StatusOK {
		t.Fatalf("status endpoint=%d body=%s", status.Code, status.Body.String())
	}
	positions := requestRisk(t, handler, http.MethodGet, "/api/v1/risk/runs/"+first.ID+"/positions?limit=1", nil)
	if positions.Code != http.StatusOK {
		t.Fatalf("positions endpoint=%d body=%s", positions.Code, positions.Body.String())
	}
	missingPositions := requestRisk(t, handler, http.MethodGet, "/api/v1/risk/runs/00000000-0000-0000-0000-000000000099/positions", nil)
	if missingPositions.Code != http.StatusNotFound {
		t.Fatalf("missing parent positions status=%d body=%s", missingPositions.Code, missingPositions.Body.String())
	}

	cancel := requestRisk(t, handler, http.MethodPost, "/api/v1/risk/runs/"+first.ID+"/cancel", strings.NewReader(`{"reason":"integration cleanup"}`))
	if cancel.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", cancel.Code, cancel.Body.String())
	}
	var cancelled applicationrisk.Run
	decodeRisk(t, cancel, &cancelled)
	if cancelled.Status != "cancelled" {
		t.Fatalf("cancelled run status=%q", cancelled.Status)
	}
	var jobState, runState string
	if err := pool.QueryRow(ctx, `SELECT j.state, sr.state FROM risk_jobs j JOIN scenario_runs sr ON sr.job_id=j.id WHERE sr.id=$1::uuid`, first.ID).Scan(&jobState, &runState); err != nil {
		t.Fatal(err)
	}
	if jobState != "cancelled" || runState != "failed" {
		t.Fatalf("cancel lifecycle job=%s run=%s", jobState, runState)
	}

	completedRequest := request
	completedRequest["name"] = "risk-api-e2e-completed"
	completedRequest["positions"] = []map[string]any{{"snapshot_line_id": lineID, "instrument_id": instrumentID}}
	completed := submitRiskRun(t, handler, completedRequest, "risk-api-e2e-completed-key")
	resultJSON := `{"job_id":"` + completed.JobID + `","schema_version":"1.0","status":"succeeded","input_snapshot_ids":["` + snapshotID + `"],"data_quality":"healthy","engine_version":"risk-engine-0.1.0","output":{"state":"valid","positions":[]}}`
	resultHash := strings.Repeat("a", 64)
	if _, err := pool.Exec(ctx, `UPDATE risk_jobs SET state='succeeded', result=$1::jsonb, result_hash=$2, completed_at=clock_timestamp() WHERE id=$3::uuid`, resultJSON, resultHash, completed.JobID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE scenario_runs SET state='valid', result=$1::jsonb, result_hash=$2, completed_at=clock_timestamp() WHERE id=$3::uuid`, resultJSON, resultHash, completed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scenario_run_positions (run_id,snapshot_line_id,instrument_id,state,reason_codes) VALUES ($1::uuid,$2::uuid,$3::uuid,'valid','{}')`, completed.ID, lineID, instrumentID); err != nil {
		t.Fatal(err)
	}
	completedRead := requestRisk(t, handler, http.MethodGet, "/api/v1/risk/runs/"+completed.ID, nil)
	if completedRead.Code != http.StatusOK {
		t.Fatalf("completed get status=%d body=%s", completedRead.Code, completedRead.Body.String())
	}
	var completedRun applicationrisk.Run
	decodeRisk(t, completedRead, &completedRun)
	if completedRun.Status != "completed" || completedRun.DataQuality != "healthy" || completedRun.EngineVersion != "risk-engine-0.1.0" || completedRun.ResultHash != resultHash {
		t.Fatalf("completed provenance/result=%+v", completedRun)
	}
	completedPositions := requestRisk(t, handler, http.MethodGet, "/api/v1/risk/runs/"+completed.ID+"/positions?limit=1", nil)
	if completedPositions.Code != http.StatusOK || !strings.Contains(completedPositions.Body.String(), lineID) {
		t.Fatalf("completed positions status=%d body=%s", completedPositions.Code, completedPositions.Body.String())
	}
	if _, err := pool.Exec(ctx, `UPDATE scenario_runs SET state='blocked' WHERE id=$1::uuid`, completed.ID); err == nil {
		t.Fatal("completed scenario result was mutable")
	}
	completedCancel := requestRisk(t, handler, http.MethodPost, "/api/v1/risk/runs/"+completed.ID+"/cancel", nil)
	if completedCancel.Code != http.StatusOK {
		t.Fatalf("completed cancel status=%d body=%s", completedCancel.Code, completedCancel.Body.String())
	}
	var afterCancel applicationrisk.Run
	decodeRisk(t, completedCancel, &afterCancel)
	if afterCancel.Status != "completed" {
		t.Fatalf("completed run changed after cancel: %+v", afterCancel)
	}
}

func createRiskAPIFixture(t *testing.T, pool *pgxpool.Pool) (accountID, snapshotID, lineID, instrumentID string) {
	t.Helper()
	ctx := context.Background()
	var portfolioID string
	if err := pool.QueryRow(ctx, `INSERT INTO portfolios (name,reporting_currency) VALUES ('risk-api-'||gen_random_uuid()::text,'TRY') RETURNING id::text`).Scan(&portfolioID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (portfolio_id,name) VALUES ($1::uuid,'risk-api-account') RETURNING id::text`, portfolioID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO instruments (canonical_symbol,instrument_type,native_currency) VALUES ('RISK-API-'||gen_random_uuid()::text,'manual_spot','USD') RETURNING id::text`).Scan(&instrumentID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO portfolio_snapshots (portfolio_id,captured_at) VALUES ($1::uuid,clock_timestamp()) RETURNING id::text`, portfolioID).Scan(&snapshotID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO portfolio_snapshot_lines (portfolio_id,snapshot_id,account_id,instrument_id,quantity) VALUES ($1::uuid,$2::uuid,$3::uuid,$4::uuid,1) RETURNING id::text`, portfolioID, snapshotID, accountID, instrumentID).Scan(&lineID); err != nil {
		t.Fatal(err)
	}
	return accountID, snapshotID, lineID, instrumentID
}

func submitRiskRun(t *testing.T, handler http.Handler, payload map[string]any, key string) applicationrisk.Run {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/risk/runs", bytes.NewReader(data))
	req.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusAccepted {
		t.Fatalf("submit status=%d body=%s", response.Code, response.Body.String())
	}
	var run applicationrisk.Run
	decodeRisk(t, response, &run)
	return run
}

func requestRisk(t *testing.T, handler http.Handler, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func decodeRisk(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode risk response: %v; body=%s", err, response.Body.String())
	}
}
