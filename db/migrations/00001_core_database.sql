-- +goose Up
-- +goose StatementBegin

CREATE TABLE data_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    adapter_version TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT data_sources_code_key UNIQUE (code),
    CONSTRAINT data_sources_code_not_blank CHECK (length(btrim(code)) > 0),
    CONSTRAINT data_sources_name_not_blank CHECK (length(btrim(name)) > 0)
);

CREATE TABLE datasets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id UUID NOT NULL REFERENCES data_sources (id),
    external_key TEXT NOT NULL,
    name TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT datasets_source_external_key UNIQUE (source_id, external_key),
    CONSTRAINT datasets_external_key_not_blank CHECK (length(btrim(external_key)) > 0)
);

CREATE TABLE series (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dataset_id UUID NOT NULL REFERENCES datasets (id),
    source_code TEXT NOT NULL,
    name TEXT NOT NULL,
    unit TEXT NOT NULL,
    frequency TEXT NOT NULL,
    seasonal_adjustment TEXT,
    source_timezone TEXT,
    freshness_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT series_dataset_source_code UNIQUE (dataset_id, source_code),
    CONSTRAINT series_source_code_not_blank CHECK (length(btrim(source_code)) > 0),
    CONSTRAINT series_frequency_not_blank CHECK (length(btrim(frequency)) > 0)
);

CREATE TABLE instruments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    canonical_symbol TEXT NOT NULL,
    instrument_type TEXT NOT NULL,
    native_currency CHAR(3) NOT NULL,
    external_ids JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT instruments_symbol_key UNIQUE (canonical_symbol),
    CONSTRAINT instruments_symbol_not_blank CHECK (length(btrim(canonical_symbol)) > 0),
    CONSTRAINT instruments_type_not_blank CHECK (length(btrim(instrument_type)) > 0),
    CONSTRAINT instruments_currency_upper CHECK (native_currency = upper(native_currency)),
    CONSTRAINT instruments_status CHECK (status IN ('active', 'inactive', 'delisted'))
);

CREATE TABLE ingestion_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id UUID NOT NULL REFERENCES data_sources (id),
    dataset_id UUID REFERENCES datasets (id),
    idempotency_key TEXT NOT NULL,
    adapter_version TEXT NOT NULL,
    requested_from TIMESTAMPTZ,
    requested_to TIMESTAMPTZ,
    status TEXT NOT NULL,
    coverage JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at TIMESTAMPTZ,
    CONSTRAINT ingestion_runs_source_idempotency UNIQUE (source_id, idempotency_key),
    CONSTRAINT ingestion_runs_status CHECK (status IN ('running', 'succeeded', 'partial', 'failed', 'cancelled')),
    CONSTRAINT ingestion_runs_window CHECK (requested_to IS NULL OR requested_from IS NULL OR requested_to >= requested_from),
    CONSTRAINT ingestion_runs_completion CHECK (status = 'running' OR completed_at IS NOT NULL)
);

CREATE TABLE raw_objects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    content_sha256 CHAR(64) NOT NULL,
    object_key TEXT NOT NULL,
    media_type TEXT NOT NULL,
    byte_length BIGINT NOT NULL,
    retrieved_at TIMESTAMPTZ NOT NULL,
    request_uri TEXT,
    request_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    ingestion_run_id UUID REFERENCES ingestion_runs (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT raw_objects_content_sha256_key UNIQUE (content_sha256),
    CONSTRAINT raw_objects_object_key_key UNIQUE (object_key),
    CONSTRAINT raw_objects_sha256_hex CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT raw_objects_byte_length_nonnegative CHECK (byte_length >= 0),
    CONSTRAINT raw_objects_object_key_not_blank CHECK (length(btrim(object_key)) > 0)
);

CREATE TABLE observation_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    series_id UUID NOT NULL REFERENCES series (id),
    observation_time TIMESTAMPTZ NOT NULL,
    value NUMERIC(38,18),
    value_text TEXT,
    source_known_at TIMESTAMPTZ,
    system_known_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    knowledge_time_basis TEXT NOT NULL,
    raw_object_id UUID NOT NULL REFERENCES raw_objects (id),
    quality_flags JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT observation_revisions_value_present CHECK (value IS NOT NULL OR value_text IS NOT NULL),
    CONSTRAINT observation_revisions_knowledge_basis CHECK (
        (source_known_at IS NULL AND knowledge_time_basis = 'first_observed_by_system')
        OR (source_known_at IS NOT NULL AND knowledge_time_basis IN ('source_published_at', 'source_effective_at'))
    ),
    CONSTRAINT observation_revisions_identity UNIQUE NULLS NOT DISTINCT
        (series_id, observation_time, value, value_text, source_known_at, raw_object_id)
);

CREATE TABLE price_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    instrument_id UUID NOT NULL REFERENCES instruments (id),
    quote_currency CHAR(3) NOT NULL,
    observation_time TIMESTAMPTZ NOT NULL,
    price NUMERIC(38,18) NOT NULL,
    source_known_at TIMESTAMPTZ,
    system_known_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    knowledge_time_basis TEXT NOT NULL,
    raw_object_id UUID NOT NULL REFERENCES raw_objects (id),
    quality_flags JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT price_revisions_quote_currency_upper CHECK (quote_currency = upper(quote_currency)),
    CONSTRAINT price_revisions_knowledge_basis CHECK (
        (source_known_at IS NULL AND knowledge_time_basis = 'first_observed_by_system')
        OR (source_known_at IS NOT NULL AND knowledge_time_basis IN ('source_published_at', 'source_effective_at'))
    ),
    CONSTRAINT price_revisions_identity UNIQUE NULLS NOT DISTINCT
        (instrument_id, quote_currency, observation_time, price, source_known_at, raw_object_id)
);

CREATE TABLE fx_quote_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    base_currency CHAR(3) NOT NULL,
    quote_currency CHAR(3) NOT NULL,
    observation_time TIMESTAMPTZ NOT NULL,
    rate NUMERIC(38,18) NOT NULL,
    source_known_at TIMESTAMPTZ,
    system_known_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    knowledge_time_basis TEXT NOT NULL,
    raw_object_id UUID NOT NULL REFERENCES raw_objects (id),
    quality_flags JSONB NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT fx_quote_revisions_currencies_upper CHECK (
        base_currency = upper(base_currency) AND quote_currency = upper(quote_currency)
    ),
    CONSTRAINT fx_quote_revisions_distinct_currencies CHECK (base_currency <> quote_currency),
    CONSTRAINT fx_quote_revisions_knowledge_basis CHECK (
        (source_known_at IS NULL AND knowledge_time_basis = 'first_observed_by_system')
        OR (source_known_at IS NOT NULL AND knowledge_time_basis IN ('source_published_at', 'source_effective_at'))
    ),
    CONSTRAINT fx_quote_revisions_identity UNIQUE NULLS NOT DISTINCT
        (base_currency, quote_currency, observation_time, rate, source_known_at, raw_object_id)
);

-- Revisions and raw evidence are immutable. A new value is represented by an
-- insert, never by changing or deleting the prior evidence row.
CREATE OR REPLACE FUNCTION prevent_immutable_row_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    RAISE EXCEPTION 'immutable table % does not allow %', TG_TABLE_NAME, TG_OP
        USING ERRCODE = '55000';
END;
$function$;

CREATE TRIGGER raw_objects_immutable
    BEFORE UPDATE OR DELETE ON raw_objects
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE TRIGGER observation_revisions_immutable
    BEFORE UPDATE OR DELETE ON observation_revisions
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE TRIGGER price_revisions_immutable
    BEFORE UPDATE OR DELETE ON price_revisions
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE TRIGGER fx_quote_revisions_immutable
    BEFORE UPDATE OR DELETE ON fx_quote_revisions
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE INDEX datasets_source_idx ON datasets (source_id, id);
CREATE INDEX series_dataset_idx ON series (dataset_id, id);
CREATE INDEX ingestion_runs_source_started_idx ON ingestion_runs (source_id, started_at DESC);
CREATE INDEX raw_objects_ingestion_run_idx ON raw_objects (ingestion_run_id, retrieved_at DESC);

CREATE INDEX observation_revisions_series_time_idx
    ON observation_revisions (series_id, observation_time DESC, system_known_at DESC);
CREATE INDEX observation_revisions_series_system_asof_idx
    ON observation_revisions (series_id, system_known_at DESC, observation_time DESC);
CREATE INDEX observation_revisions_series_source_asof_idx
    ON observation_revisions (series_id, source_known_at DESC NULLS LAST, observation_time DESC)
    WHERE source_known_at IS NOT NULL;

CREATE INDEX price_revisions_instrument_time_idx
    ON price_revisions (instrument_id, quote_currency, observation_time DESC, system_known_at DESC);
CREATE INDEX price_revisions_instrument_system_asof_idx
    ON price_revisions (instrument_id, system_known_at DESC, observation_time DESC);
CREATE INDEX price_revisions_instrument_source_asof_idx
    ON price_revisions (instrument_id, source_known_at DESC NULLS LAST, observation_time DESC)
    WHERE source_known_at IS NOT NULL;

CREATE INDEX fx_quote_revisions_pair_time_idx
    ON fx_quote_revisions (base_currency, quote_currency, observation_time DESC, system_known_at DESC);
CREATE INDEX fx_quote_revisions_pair_system_asof_idx
    ON fx_quote_revisions (base_currency, quote_currency, system_known_at DESC, observation_time DESC);
CREATE INDEX fx_quote_revisions_pair_source_asof_idx
    ON fx_quote_revisions (base_currency, quote_currency, source_known_at DESC NULLS LAST, observation_time DESC)
    WHERE source_known_at IS NOT NULL;

CREATE VIEW latest_observation_revisions AS
SELECT DISTINCT ON (series_id, observation_time) *
FROM observation_revisions
ORDER BY series_id, observation_time, system_known_at DESC, id DESC;

CREATE VIEW latest_price_revisions AS
SELECT DISTINCT ON (instrument_id, quote_currency, observation_time) *
FROM price_revisions
ORDER BY instrument_id, quote_currency, observation_time, system_known_at DESC, id DESC;

CREATE VIEW latest_fx_quote_revisions AS
SELECT DISTINCT ON (base_currency, quote_currency, observation_time) *
FROM fx_quote_revisions
ORDER BY base_currency, quote_currency, observation_time, system_known_at DESC, id DESC;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP VIEW IF EXISTS latest_fx_quote_revisions;
DROP VIEW IF EXISTS latest_price_revisions;
DROP VIEW IF EXISTS latest_observation_revisions;
DROP TRIGGER IF EXISTS fx_quote_revisions_immutable ON fx_quote_revisions;
DROP TRIGGER IF EXISTS price_revisions_immutable ON price_revisions;
DROP TRIGGER IF EXISTS observation_revisions_immutable ON observation_revisions;
DROP TRIGGER IF EXISTS raw_objects_immutable ON raw_objects;
DROP FUNCTION IF EXISTS prevent_immutable_row_mutation();
DROP TABLE IF EXISTS fx_quote_revisions;
DROP TABLE IF EXISTS price_revisions;
DROP TABLE IF EXISTS observation_revisions;
DROP TABLE IF EXISTS raw_objects;
DROP TABLE IF EXISTS ingestion_runs;
DROP TABLE IF EXISTS instruments;
DROP TABLE IF EXISTS series;
DROP TABLE IF EXISTS datasets;
DROP TABLE IF EXISTS data_sources;
-- +goose StatementEnd
