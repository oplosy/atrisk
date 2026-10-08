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
	key := riskJobKey(t, "repair-numeric")
	handler := apirisk.New(applicationrisk.Service{Pool: pool})
	request := map[string]any{
		"account_id": accountID, "snapshot_id": snapshotID, "valuation_id": valuationID,
		"name": "repair-numeric", "template_key": "risk_off",
		"units":    map[string]any{"reporting_currency": "TRY"},
		"shocks":   map[string]any{"asset_class_returns": map[string]any{"crypto": -0.4}},
		"mappings": map[string]any{}, "assumptions": map[string]any{"coverage_policy": "block"},
	}
	response := submitRiskRequest(t, handler, request, key)
	if response != http.StatusBadRequest {
		t.Fatalf("numeric shock status=%d", response)
	}
	var jobsCreated int
	if err := pool.QueryRow(context.Background(), `SELECT count(*)::int FROM risk_jobs WHERE idempotency_key=$1`, key).Scan(&jobsCreated); err != nil {
		t.Fatal(err)
	}
	if jobsCreated != 0 {
		t.Fatalf("invalid numeric shock created %d jobs", jobsCreated)
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
