package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/oplosy/atrisk/internal/jobs"
)

func riskJobKey(t *testing.T, name string) string {
	t.Helper()
	return fmt.Sprintf("%s-%d", name, time.Now().UnixNano())
}

func enqueueRiskJob(t *testing.T, queue jobs.Queue, key string, maxAttempts int) jobs.Job {
	t.Helper()
	job, err := queue.Enqueue(context.Background(), jobs.EnqueueRequest{
		Kind: "risk.run", SchemaVersion: jobs.SchemaVersion, IdempotencyKey: key,
		InputSnapshotIDs: []string{"snapshot-risk-001"}, Payload: map[string]any{"case": key},
		MaxAttempts: maxAttempts,
	})
	if err != nil {
		t.Fatalf("enqueue risk job: %v", err)
	}
	return job
}

func TestRiskJobLifecycle(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	queue := jobs.Queue{Pool: pool}
	ctx := context.Background()

	job := enqueueRiskJob(t, queue, riskJobKey(t, "concurrent"), 3)
	claims := make(chan *jobs.Claim, 2)
	errors := make(chan error, 2)
	var group sync.WaitGroup
	for _, workerID := range []string{"worker-a", "worker-b"} {
		group.Add(1)
		go func(workerID string) {
			defer group.Done()
			claim, err := queue.Claim(ctx, workerID, time.Minute)
			if err != nil {
				errors <- err
				return
			}
			claims <- claim
		}(workerID)
	}
	group.Wait()
	close(claims)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	var winner *jobs.Claim
	claimedCount := 0
	for claim := range claims {
		if claim != nil {
			winner = claim
			claimedCount++
		}
	}
	if claimedCount != 1 || winner == nil {
		t.Fatalf("expected exactly one active claim, got %d", claimedCount)
	}
	if completed, err := queue.Complete(ctx, *winner, map[string]any{"ok": true}); err != nil || !completed {
		t.Fatalf("complete claimed job: completed=%v err=%v", completed, err)
	}
	if completed, err := queue.Complete(ctx, *winner, map[string]any{"ok": true}); err != nil || completed {
		t.Fatalf("completion was not idempotent: completed=%v err=%v", completed, err)
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM risk_jobs WHERE id=$1::uuid`, job.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != string(jobs.StateSucceeded) {
		t.Fatalf("completed job state=%s", state)
	}

	expired := enqueueRiskJob(t, queue, riskJobKey(t, "expired"), 3)
	claim, err := queue.Claim(ctx, "worker-expiring", 20*time.Millisecond)
	if err != nil || claim == nil {
		t.Fatalf("claim expiring job: claim=%v err=%v", claim, err)
	}
	time.Sleep(50 * time.Millisecond)
	recovered, err := queue.RecoverExpired(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("recover expired job: recovered=%d err=%v", recovered, err)
	}
	reclaimed, err := queue.Claim(ctx, "worker-recovered", time.Minute)
	if err != nil || reclaimed == nil || reclaimed.ID != expired.ID {
		t.Fatalf("reclaim expired job: claim=%v err=%v", reclaimed, err)
	}
	if completed, err := queue.Complete(ctx, *claim, map[string]any{"stale": true}); err != nil || completed {
		t.Fatalf("expired lease completion accepted: completed=%v err=%v", completed, err)
	}
	if completed, err := queue.Complete(ctx, *reclaimed, map[string]any{"recovered": true}); err != nil || !completed {
		t.Fatalf("recovered completion failed: completed=%v err=%v", completed, err)
	}

	retry := enqueueRiskJob(t, queue, riskJobKey(t, "retry"), 2)
	retryClaim, err := queue.Claim(ctx, "worker-retry-1", time.Minute)
	if err != nil || retryClaim == nil {
		t.Fatalf("claim retry job: claim=%v err=%v", retryClaim, err)
	}
	if failed, err := queue.Fail(ctx, *retryClaim, jobs.Failure{Code: "TRANSIENT", Message: "temporary", Retryable: true}); err != nil || !failed {
		t.Fatalf("retryable failure: failed=%v err=%v", failed, err)
	}
	retryClaim, err = queue.Claim(ctx, "worker-retry-2", time.Minute)
	if err != nil || retryClaim == nil {
		t.Fatalf("claim retry attempt two: claim=%v err=%v", retryClaim, err)
	}
	if failed, err := queue.Fail(ctx, *retryClaim, jobs.Failure{Code: "TRANSIENT", Message: "still temporary", Retryable: true}); err != nil || !failed {
		t.Fatalf("terminal retry failure: failed=%v err=%v", failed, err)
	}
	if err := pool.QueryRow(ctx, `SELECT state FROM risk_jobs WHERE id=$1::uuid`, retry.ID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != string(jobs.StateFailed) {
		t.Fatalf("max-attempt retry state=%s", state)
	}
	var attempts int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM risk_job_attempts WHERE job_id=$1::uuid AND error_code='TRANSIENT'`, retry.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("expected two retained failure attempts, got %d", attempts)
	}

	cancelled := enqueueRiskJob(t, queue, riskJobKey(t, "cancel"), 3)
	cancelClaim, err := queue.Claim(ctx, "worker-cancel", time.Minute)
	if err != nil || cancelClaim == nil {
		t.Fatalf("claim cancellable job: claim=%v err=%v", cancelClaim, err)
	}
	if ok, err := queue.Cancel(ctx, cancelled.ID, "operator requested cancellation"); err != nil || !ok {
		t.Fatalf("cancel job: ok=%v err=%v", ok, err)
	}
	var outcome string
	if err := pool.QueryRow(ctx, `SELECT outcome FROM risk_job_attempts WHERE job_id=$1::uuid`, cancelled.ID).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != "cancelled" {
		t.Fatalf("cancelled attempt outcome=%s", outcome)
	}
}
