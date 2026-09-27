// Package jobs implements the PostgreSQL-backed risk job boundary.
package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const SchemaVersion = "1.0"

type State string

const (
	StateQueued          State = "queued"
	StateRunning         State = "running"
	StateSucceeded       State = "succeeded"
	StateRetryableFailed State = "retryable_failed"
	StateFailed          State = "failed"
	StateCancelled       State = "cancelled"
)

type Job struct {
	ID               string          `json:"id"`
	Kind             string          `json:"kind"`
	SchemaVersion    string          `json:"schema_version"`
	IdempotencyKey   string          `json:"idempotency_key"`
	InputSnapshotIDs []string        `json:"input_snapshot_ids"`
	Payload          json.RawMessage `json:"payload"`
	State            State           `json:"state"`
	AttemptCount     int             `json:"attempt_count"`
	MaxAttempts      int             `json:"max_attempts"`
	AvailableAt      time.Time       `json:"available_at"`
	LeaseOwner       *string         `json:"lease_owner,omitempty"`
	LeaseExpiresAt   *time.Time      `json:"lease_expires_at,omitempty"`
	Result           json.RawMessage `json:"result,omitempty"`
	ResultHash       *string         `json:"result_hash,omitempty"`
	ErrorCode        *string         `json:"error_code,omitempty"`
	ErrorMessage     *string         `json:"error_message,omitempty"`
	ErrorDetails     json.RawMessage `json:"error_details,omitempty"`
	CompletedAt      *time.Time      `json:"completed_at,omitempty"`
}

type EnqueueRequest struct {
	Kind             string
	SchemaVersion    string
	IdempotencyKey   string
	InputSnapshotIDs []string
	Payload          any
	MaxAttempts      int
	AvailableAt      time.Time
}

type Claim struct {
	Job
	WorkerID string `json:"worker_id"`
}

type Failure struct {
	Code      string
	Message   string
	Details   any
	Retryable bool
	Delay     time.Duration
}

type Queue struct{ Pool *pgxpool.Pool }

func CanonicalJSON(value any) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(normalizeCanonical(value)); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(encoded.Bytes(), []byte{'\n'}), nil
}

func normalizeCanonical(value any) any {
	switch typed := value.(type) {
	case float64:
		if typed == 0 {
			return 0
		}
		if typed >= math.MinInt64 && typed <= math.MaxInt64 && math.Trunc(typed) == typed {
			return int64(typed)
		}
		return typed
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = normalizeCanonical(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = normalizeCanonical(item)
		}
		return result
	default:
		return value
	}
}

func HashCanonicalJSON(value any) (string, error) {
	data, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func validateEnvelope(request EnqueueRequest) error {
	if request.Kind == "" || request.Kind[0] < 'a' || request.Kind[0] > 'z' || !strings.Contains(request.Kind, ".") {
		return errors.New("job kind must be a lowercase dotted name")
	}
	if request.SchemaVersion == "" {
		return errors.New("job schema version is required")
	}
	if request.IdempotencyKey == "" || len(request.IdempotencyKey) > 255 {
		return errors.New("job idempotency key must be 1..255 characters")
	}
	seen := make(map[string]struct{}, len(request.InputSnapshotIDs))
	for _, id := range request.InputSnapshotIDs {
		if id == "" {
			return errors.New("job snapshot IDs must not be empty")
		}
		if _, ok := seen[id]; ok {
			return errors.New("job snapshot IDs must be unique")
		}
		seen[id] = struct{}{}
	}
	if request.Payload == nil {
		return errors.New("job payload is required")
	}
	return nil
}

// Enqueue is idempotent on (kind, idempotency_key); an existing job is returned.
func (q Queue) Enqueue(ctx context.Context, request EnqueueRequest) (Job, error) {
	if q.Pool == nil {
		return Job{}, errors.New("jobs queue requires a database pool")
	}
	if err := validateEnvelope(request); err != nil {
		return Job{}, err
	}
	payload, err := CanonicalJSON(request.Payload)
	if err != nil {
		return Job{}, fmt.Errorf("serialize job payload: %w", err)
	}
	version := request.SchemaVersion
	if version == "" {
		version = SchemaVersion
	}
	maxAttempts := request.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	availableAt := request.AvailableAt
	if availableAt.IsZero() {
		availableAt = time.Now().UTC()
	}
	const statement = `INSERT INTO risk_jobs (kind, schema_version, idempotency_key, input_snapshot_ids, payload, max_attempts, available_at) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7) ON CONFLICT (kind,idempotency_key) DO UPDATE SET id=risk_jobs.id RETURNING id::text,kind,schema_version,idempotency_key,input_snapshot_ids,payload,state,attempt_count,max_attempts,available_at,lease_owner,lease_expires_at,result,result_hash,error_code,error_message,error_details,completed_at`
	return scanJob(q.Pool.QueryRow(ctx, statement, request.Kind, version, request.IdempotencyKey, request.InputSnapshotIDs, payload, maxAttempts, availableAt))
}

// Claim atomically claims one available job. Row locking prevents two active leases.
func (q Queue) Claim(ctx context.Context, workerID string, lease time.Duration) (*Claim, error) {
	if q.Pool == nil || workerID == "" {
		return nil, errors.New("worker ID and database pool are required")
	}
	if lease <= 0 {
		return nil, errors.New("lease must be positive")
	}
	const statement = `WITH candidate AS (SELECT id FROM risk_jobs WHERE state IN ('queued','retryable_failed') AND available_at <= clock_timestamp() ORDER BY available_at,created_at,id FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE risk_jobs AS j SET state='running',attempt_count=j.attempt_count+1,lease_owner=$1,lease_expires_at=clock_timestamp()+$2::interval,updated_at=clock_timestamp() FROM candidate WHERE j.id=candidate.id RETURNING j.id::text,j.kind,j.schema_version,j.idempotency_key,j.input_snapshot_ids,j.payload,j.state,j.attempt_count,j.max_attempts,j.available_at,j.lease_owner,j.lease_expires_at,j.result,j.result_hash,j.error_code,j.error_message,j.error_details,j.completed_at`
	tx, err := q.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	job, err := scanJob(tx.QueryRow(ctx, statement, workerID, lease.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim job: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO risk_job_attempts (job_id,attempt,worker_id,lease_expires_at) VALUES ($1::uuid,$2,$3,$4)`, job.ID, job.AttemptCount, workerID, job.LeaseExpiresAt); err != nil {
		return nil, fmt.Errorf("record job attempt: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim transaction: %w", err)
	}
	return &Claim{Job: job, WorkerID: workerID}, nil
}

func (q Queue) Complete(ctx context.Context, claim Claim, result any) (bool, error) {
	data, err := CanonicalJSON(result)
	if err != nil {
		return false, fmt.Errorf("serialize result: %w", err)
	}
	digest := sha256.Sum256(data)
	hash := hex.EncodeToString(digest[:])
	tx, err := q.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ct, err := tx.Exec(ctx, `UPDATE risk_jobs SET state='succeeded',result=$1::jsonb,result_hash=$2,lease_owner=NULL,lease_expires_at=NULL,completed_at=clock_timestamp() WHERE id=$3::uuid AND state='running' AND lease_owner=$4 AND lease_expires_at > clock_timestamp()`, data, hash, claim.ID, claim.WorkerID)
	if err != nil {
		return false, fmt.Errorf("complete job: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE risk_job_attempts SET finished_at=clock_timestamp(),outcome='succeeded' WHERE job_id=$1::uuid AND attempt=$2 AND finished_at IS NULL`, claim.ID, claim.AttemptCount); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (q Queue) Fail(ctx context.Context, claim Claim, failure Failure) (bool, error) {
	state := StateFailed
	if failure.Retryable && claim.AttemptCount < claim.MaxAttempts {
		state = StateRetryableFailed
	}
	details, err := CanonicalJSON(failure.DetailsOrEmpty())
	if err != nil {
		return false, err
	}
	tx, err := q.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ct, err := tx.Exec(ctx, `UPDATE risk_jobs SET state=$1,available_at=clock_timestamp()+$2::interval,error_code=$3,error_message=$4,error_details=$5::jsonb,lease_owner=NULL,lease_expires_at=NULL,completed_at=CASE WHEN $1 IN ('failed','cancelled') THEN clock_timestamp() ELSE NULL END WHERE id=$6::uuid AND state='running' AND lease_owner=$7 AND lease_expires_at > clock_timestamp()`, state, failure.Delay.String(), failure.Code, failure.Message, details, claim.ID, claim.WorkerID)
	if err != nil {
		return false, err
	}
	if ct.RowsAffected() == 0 {
		return false, nil
	}
	if _, err = tx.Exec(ctx, `UPDATE risk_job_attempts SET finished_at=clock_timestamp(),outcome=$1,error_code=$2,error_message=$3,error_details=$4::jsonb WHERE job_id=$5::uuid AND attempt=$6 AND finished_at IS NULL`, string(state), failure.Code, failure.Message, details, claim.ID, claim.AttemptCount); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (f Failure) DetailsOrEmpty() any {
	if f.Details == nil {
		return map[string]any{}
	}
	return f.Details
}

func (q Queue) Cancel(ctx context.Context, id, reason string) (bool, error) {
	tx, err := q.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ct, err := tx.Exec(ctx, `UPDATE risk_jobs SET state='cancelled',error_code='JOB_CANCELLED',error_message=$1,lease_owner=NULL,lease_expires_at=NULL,completed_at=clock_timestamp() WHERE id=$2::uuid AND state IN ('queued','retryable_failed','running')`, reason, id)
	if err != nil || ct.RowsAffected() != 1 {
		return ct.RowsAffected() == 1, err
	}
	if _, err = tx.Exec(ctx, `UPDATE risk_job_attempts SET finished_at=clock_timestamp(),outcome='cancelled',error_code='JOB_CANCELLED',error_message=$1 WHERE job_id=$2::uuid AND finished_at IS NULL`, reason, id); err != nil {
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (q Queue) RecoverExpired(ctx context.Context) (int64, error) {
	ct, err := q.Pool.Exec(ctx, `WITH expired AS (UPDATE risk_jobs SET state=CASE WHEN attempt_count >= max_attempts THEN 'failed' ELSE 'retryable_failed' END,available_at=clock_timestamp(),lease_owner=NULL,lease_expires_at=NULL,completed_at=CASE WHEN attempt_count >= max_attempts THEN clock_timestamp() ELSE NULL END,error_code='LEASE_EXPIRED',error_message='worker lease expired' WHERE state='running' AND lease_expires_at < clock_timestamp() RETURNING id,attempt_count) UPDATE risk_job_attempts a SET finished_at=clock_timestamp(),outcome='expired',error_code='LEASE_EXPIRED',error_message='worker lease expired' FROM expired e WHERE a.job_id=e.id AND a.attempt=e.attempt_count`)
	return ct.RowsAffected(), err
}

func scanJob(row pgx.Row) (Job, error) {
	var job Job
	err := row.Scan(&job.ID, &job.Kind, &job.SchemaVersion, &job.IdempotencyKey, &job.InputSnapshotIDs, &job.Payload, &job.State, &job.AttemptCount, &job.MaxAttempts, &job.AvailableAt, &job.LeaseOwner, &job.LeaseExpiresAt, &job.Result, &job.ResultHash, &job.ErrorCode, &job.ErrorMessage, &job.ErrorDetails, &job.CompletedAt)
	return job, err
}
