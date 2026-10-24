package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNoDueSchedule  = errors.New("no due collector schedule")
	ErrLeaseLost      = errors.New("collector schedule lease lost")
	ErrRequestChanged = errors.New("collector schedule request changed; checkpoint cannot be reused")
)

type Store struct{ Pool *pgxpool.Pool }

type Claim struct {
	ID                 string
	Name               string
	Provider           string
	SourceID           string
	DatasetID          *string
	SeriesID           *string
	InstrumentID       *string
	Configuration      []byte
	RequestFingerprint string
	Checkpoint         []byte
	LeaseOwner         string
	LeaseToken         string
	LeaseExpiresAt     time.Time
	AttemptCount       int
	MaxAttempts        int
	IntervalSeconds    int
	LeaseSeconds       int
	DueAt              time.Time
	OccurrenceKey      string
}

func (s Store) UpsertSchedule(ctx context.Context, schedule Schedule) (string, error) {
	if s.Pool == nil {
		return "", errors.New("collector database pool is required")
	}
	var identitiesValid bool
	if err := s.Pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM data_sources src
    LEFT JOIN datasets d ON d.id = NULLIF($2, '')::uuid
    LEFT JOIN series ser ON ser.id = NULLIF($3, '')::uuid
    LEFT JOIN instruments i ON i.id = NULLIF($4, '')::uuid
    WHERE src.id = $1::uuid
      AND ($2 = '' OR d.source_id = src.id)
      AND ($3 = '' OR (d.id IS NOT NULL AND ser.dataset_id = d.id))
      AND ($4 = '' OR i.id IS NOT NULL)
)`, schedule.SourceID, schedule.DatasetID, schedule.SeriesID, schedule.InstrumentID).Scan(&identitiesValid); err != nil {
		return "", fmt.Errorf("validate collector schedule identities: %w", err)
	}
	if !identitiesValid {
		return "", errors.New("collector schedule identities do not belong to the configured source")
	}
	next := time.Now().UTC()
	if schedule.NextRunAt != nil {
		next = schedule.NextRunAt.UTC()
	}
	var id string
	err := s.Pool.QueryRow(ctx, `
INSERT INTO collector_schedules
    (name, provider, source_id, dataset_id, series_id, instrument_id, configuration,
     request_fingerprint, interval_seconds, lease_seconds, next_run_at, max_attempts)
VALUES ($1, $2, $3::uuid, NULLIF($4, '')::uuid, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid,
        $7::jsonb, $8, $9, $10, $11, $12)
ON CONFLICT (name) DO NOTHING
RETURNING id::text`, schedule.Name, schedule.Provider, schedule.SourceID, schedule.DatasetID,
		schedule.SeriesID, schedule.InstrumentID, schedule.Configuration, schedule.RequestFingerprint,
		schedule.IntervalSeconds, schedule.LeaseSeconds, next, schedule.MaxAttempts).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("insert collector schedule: %w", err)
	}
	var existingID, existingFingerprint string
	if err := s.Pool.QueryRow(ctx, `SELECT id::text, request_fingerprint FROM collector_schedules WHERE name = $1`, schedule.Name).Scan(&existingID, &existingFingerprint); err != nil {
		return "", fmt.Errorf("load collector schedule: %w", err)
	}
	if existingFingerprint != schedule.RequestFingerprint {
		return "", ErrRequestChanged
	}
	return existingID, nil
}

func (s Store) ClaimDue(ctx context.Context, owner string, lease time.Duration) (Claim, error) {
	if s.Pool == nil {
		return Claim{}, errors.New("collector database pool is required")
	}
	if owner == "" || lease <= 0 {
		return Claim{}, errors.New("collector lease owner and duration are required")
	}
	var claim Claim
	var datasetID, seriesID, instrumentID *string
	err := s.Pool.QueryRow(ctx, `
WITH candidate AS (
    SELECT id
    FROM collector_schedules
    WHERE active AND COALESCE(retry_available_at, next_run_at) <= clock_timestamp()
      AND (lease_expires_at IS NULL OR lease_expires_at <= clock_timestamp())
      AND attempt_count < max_attempts
    ORDER BY next_run_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE collector_schedules AS s
SET lease_owner = $1,
    lease_token = gen_random_uuid(),
    lease_expires_at = clock_timestamp() + make_interval(secs => s.lease_seconds),
    attempt_count = s.attempt_count + 1,
    updated_at = clock_timestamp()
FROM candidate
WHERE s.id = candidate.id
RETURNING s.id::text, s.name, s.provider, s.source_id::text,
          s.dataset_id::text, s.series_id::text, s.instrument_id::text,
          s.configuration, s.request_fingerprint, s.checkpoint, s.lease_owner,

          s.lease_token::text, s.lease_expires_at, s.attempt_count, s.max_attempts,
          s.interval_seconds, s.lease_seconds, s.next_run_at`, owner).
		Scan(&claim.ID, &claim.Name, &claim.Provider, &claim.SourceID, &datasetID, &seriesID,
			&instrumentID, &claim.Configuration, &claim.RequestFingerprint, &claim.Checkpoint,
			&claim.LeaseOwner, &claim.LeaseToken, &claim.LeaseExpiresAt, &claim.AttemptCount,
			&claim.MaxAttempts, &claim.IntervalSeconds, &claim.LeaseSeconds, &claim.DueAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Claim{}, ErrNoDueSchedule
	}
	if err != nil {
		return Claim{}, fmt.Errorf("claim collector schedule: %w", err)
	}
	claim.DatasetID, claim.SeriesID, claim.InstrumentID = datasetID, seriesID, instrumentID
	claim.OccurrenceKey = fmt.Sprintf("%s:%s:%s", claim.ID, claim.RequestFingerprint, claim.DueAt.UTC().Format(time.RFC3339Nano))
	return claim, nil
}

func (s Store) Complete(ctx context.Context, claim Claim, runID string, checkpoint []byte, nextRun time.Time) error {
	if len(checkpoint) == 0 {
		checkpoint = []byte(`{}`)
	}
	var run any
	if runID != "" {
		run = runID
	}
	result, err := s.Pool.Exec(ctx, `
UPDATE collector_schedules
SET checkpoint = $2::jsonb, next_run_at = $3, last_run_id = NULLIF($4, '')::uuid,
    retry_available_at = NULL,
    lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL,
    attempt_count = 0, last_error_code = NULL, updated_at = clock_timestamp()
WHERE id = $1::uuid AND lease_owner = $5 AND lease_token = $6::uuid
  AND lease_expires_at > clock_timestamp()`, claim.ID, checkpoint, nextRun.UTC(), run, claim.LeaseOwner, claim.LeaseToken)
	if err != nil {
		return fmt.Errorf("complete collector schedule: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (s Store) Fail(ctx context.Context, claim Claim, code string, retryAt time.Time) error {
	result, err := s.Pool.Exec(ctx, `
UPDATE collector_schedules
SET retry_available_at = $2, last_error_code = $3,
    active = CASE WHEN $6 >= max_attempts THEN FALSE ELSE active END,
    lease_owner = NULL, lease_token = NULL, lease_expires_at = NULL,
    updated_at = clock_timestamp()
WHERE id = $1::uuid AND lease_owner = $4 AND lease_token = $5::uuid
  AND lease_expires_at > clock_timestamp()`, claim.ID, retryAt.UTC(), code, claim.LeaseOwner, claim.LeaseToken, claim.AttemptCount)
	if err != nil {
		return fmt.Errorf("fail collector schedule: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return nil
}

func intervalLiteral(value time.Duration) string {
	return fmt.Sprintf("%d microseconds", value.Microseconds())
}

func checkpointObject(value []byte) map[string]any {
	var result map[string]any
	if json.Unmarshal(value, &result) != nil || result == nil {
		return map[string]any{}
	}
	return result
}
