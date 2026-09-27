-- +goose Up
-- +goose StatementBegin

CREATE TABLE risk_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind TEXT NOT NULL,
    schema_version TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    input_snapshot_ids TEXT[] NOT NULL DEFAULT '{}',
    payload JSONB NOT NULL,
    state TEXT NOT NULL DEFAULT 'queued',
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    available_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    result JSONB,
    result_hash CHAR(64),
    error_code TEXT,
    error_message TEXT,
    error_details JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT risk_jobs_kind_not_blank CHECK (length(btrim(kind)) > 0),
    CONSTRAINT risk_jobs_schema_version_not_blank CHECK (length(btrim(schema_version)) > 0),
    CONSTRAINT risk_jobs_idempotency_not_blank CHECK (length(btrim(idempotency_key)) > 0),
    CONSTRAINT risk_jobs_state CHECK (state IN ('queued', 'running', 'succeeded', 'retryable_failed', 'failed', 'cancelled')),
    CONSTRAINT risk_jobs_attempts CHECK (attempt_count >= 0 AND max_attempts > 0 AND attempt_count <= max_attempts),
    CONSTRAINT risk_jobs_running_lease CHECK (state <> 'running' OR (lease_owner IS NOT NULL AND lease_expires_at IS NOT NULL)),
    CONSTRAINT risk_jobs_result_hash CHECK (result_hash IS NULL OR result_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT risk_jobs_completed CHECK (state IN ('queued', 'running', 'retryable_failed') OR completed_at IS NOT NULL),
    CONSTRAINT risk_jobs_idempotency_key UNIQUE (kind, idempotency_key)
);

CREATE TABLE risk_job_attempts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    job_id UUID NOT NULL REFERENCES risk_jobs (id),
    attempt INTEGER NOT NULL,
    worker_id TEXT NOT NULL,
    lease_expires_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    finished_at TIMESTAMPTZ,
    outcome TEXT,
    error_code TEXT,
    error_message TEXT,
    error_details JSONB,
    CONSTRAINT risk_job_attempts_attempt CHECK (attempt > 0),
    CONSTRAINT risk_job_attempts_outcome CHECK (outcome IS NULL OR outcome IN ('succeeded', 'retryable_failed', 'failed', 'cancelled', 'expired')),
    CONSTRAINT risk_job_attempts_unique UNIQUE (job_id, attempt)
);

CREATE INDEX risk_jobs_claim_idx ON risk_jobs (available_at, created_at, id)
    WHERE state IN ('queued', 'retryable_failed');
CREATE INDEX risk_jobs_lease_idx ON risk_jobs (lease_expires_at)
    WHERE state = 'running';
CREATE INDEX risk_job_attempts_job_idx ON risk_job_attempts (job_id, attempt DESC);

CREATE OR REPLACE FUNCTION risk_jobs_touch_updated_at()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    NEW.updated_at = clock_timestamp();
    RETURN NEW;
END;
$function$;

CREATE TRIGGER risk_jobs_updated_at
    BEFORE UPDATE ON risk_jobs
    FOR EACH ROW EXECUTE FUNCTION risk_jobs_touch_updated_at();

-- The input contract is immutable after enqueue. State, lease, and evidence may change.
CREATE OR REPLACE FUNCTION risk_jobs_guard_input()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    IF NEW.kind IS DISTINCT FROM OLD.kind
       OR NEW.schema_version IS DISTINCT FROM OLD.schema_version
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.input_snapshot_ids IS DISTINCT FROM OLD.input_snapshot_ids
       OR NEW.payload IS DISTINCT FROM OLD.payload
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'risk job input fields are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE TRIGGER risk_jobs_input_immutable
    BEFORE UPDATE ON risk_jobs
    FOR EACH ROW EXECUTE FUNCTION risk_jobs_guard_input();

-- Attempts are evidence and cannot be rewritten or removed.
CREATE TRIGGER risk_job_attempts_immutable
    BEFORE UPDATE OR DELETE ON risk_job_attempts
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS risk_job_attempts_immutable ON risk_job_attempts;
DROP TRIGGER IF EXISTS risk_jobs_input_immutable ON risk_jobs;
DROP TRIGGER IF EXISTS risk_jobs_updated_at ON risk_jobs;
DROP FUNCTION IF EXISTS risk_jobs_guard_input();
DROP FUNCTION IF EXISTS risk_jobs_touch_updated_at();
DROP TABLE IF EXISTS risk_job_attempts;
DROP TABLE IF EXISTS risk_jobs;
-- +goose StatementEnd
