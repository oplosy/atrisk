package integration

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// isolateRiskQueue cancels jobs left by earlier tests. Tests in one package run
// share a schema, and queue tests claim whatever job is due.
func isolateRiskQueue(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE risk_jobs
SET state = 'cancelled', lease_owner = NULL, lease_expires_at = NULL, completed_at = clock_timestamp(), updated_at = clock_timestamp()
WHERE state IN ('queued', 'running', 'retryable_failed')`); err != nil {
		t.Fatalf("cancel leftover risk jobs: %v", err)
	}
}
