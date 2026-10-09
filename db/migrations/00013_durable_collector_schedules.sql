-- +goose Up
-- +goose StatementBegin

CREATE TABLE collector_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL UNIQUE,
    provider TEXT NOT NULL,
    source_id UUID NOT NULL REFERENCES data_sources (id),
    dataset_id UUID REFERENCES datasets (id),
    series_id UUID REFERENCES series (id),
    instrument_id UUID REFERENCES instruments (id),
    configuration JSONB NOT NULL,
    request_fingerprint CHAR(64) NOT NULL,
    interval_seconds INTEGER NOT NULL,
    lease_seconds INTEGER NOT NULL DEFAULT 120,
    next_run_at TIMESTAMPTZ NOT NULL,
    retry_available_at TIMESTAMPTZ,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    lease_owner TEXT,
    lease_token UUID,
    lease_expires_at TIMESTAMPTZ,
    checkpoint JSONB NOT NULL DEFAULT '{}'::jsonb,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    last_run_id UUID REFERENCES ingestion_runs (id),
    last_error_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT collector_schedules_provider CHECK (provider IN ('fred', 'tcmb', 'binance')),
    CONSTRAINT collector_schedules_configuration_object CHECK (jsonb_typeof(configuration) = 'object'),
    CONSTRAINT collector_schedules_checkpoint_object CHECK (jsonb_typeof(checkpoint) = 'object'),
    CONSTRAINT collector_schedules_interval_positive CHECK (interval_seconds > 0),
    CONSTRAINT collector_schedules_lease_positive CHECK (lease_seconds > 0),
    CONSTRAINT collector_schedules_attempts_positive CHECK (max_attempts > 0),
    CONSTRAINT collector_schedules_request_fingerprint CHECK (request_fingerprint ~ '^[0-9a-f]{64}$'),
    CONSTRAINT collector_schedules_lease_pair CHECK ((lease_owner IS NULL) = (lease_token IS NULL) AND (lease_token IS NULL OR lease_expires_at IS NOT NULL))
);

CREATE INDEX collector_schedules_due_idx
    ON collector_schedules (next_run_at, id)
    WHERE active;

CREATE TABLE raw_object_occurrences (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    raw_object_id UUID NOT NULL REFERENCES raw_objects (id),
    ingestion_run_id UUID NOT NULL REFERENCES ingestion_runs (id),
    occurrence_key TEXT NOT NULL,
    retrieved_at TIMESTAMPTZ NOT NULL,
    request_uri TEXT,
    request_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT raw_object_occurrences_key UNIQUE (ingestion_run_id, occurrence_key),
    CONSTRAINT raw_object_occurrences_metadata_object CHECK (jsonb_typeof(request_metadata) = 'object'),
    CONSTRAINT raw_object_occurrences_key_not_blank CHECK (length(btrim(occurrence_key)) > 0)
);

CREATE INDEX raw_object_occurrences_raw_idx
    ON raw_object_occurrences (raw_object_id, retrieved_at DESC);

CREATE OR REPLACE FUNCTION prevent_raw_occurrence_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    RAISE EXCEPTION 'immutable table % does not allow %', TG_TABLE_NAME, TG_OP
        USING ERRCODE = '55000';
END;
$function$;

CREATE TRIGGER raw_object_occurrences_immutable
    BEFORE UPDATE OR DELETE ON raw_object_occurrences
    FOR EACH ROW EXECUTE FUNCTION prevent_raw_occurrence_mutation();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS raw_object_occurrences_immutable ON raw_object_occurrences;
DROP FUNCTION IF EXISTS prevent_raw_occurrence_mutation();
DROP TABLE IF EXISTS raw_object_occurrences;
DROP INDEX IF EXISTS collector_schedules_due_idx;
DROP TABLE IF EXISTS collector_schedules;
-- +goose StatementEnd
