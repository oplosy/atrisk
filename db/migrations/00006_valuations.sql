-- +goose Up
-- +goose StatementBegin

CREATE TABLE valuation_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_id UUID NOT NULL REFERENCES portfolio_snapshots (id),
    cutoff TIMESTAMPTZ NOT NULL,
    knowledge_mode TEXT NOT NULL,
    known_at TIMESTAMPTZ NOT NULL,
    price_max_age_seconds BIGINT NOT NULL,
    fx_max_age_seconds BIGINT NOT NULL,
    request JSONB NOT NULL,
    state TEXT NOT NULL,
    result_hash CHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT valuation_runs_knowledge_mode CHECK (knowledge_mode IN ('system_as_of', 'source_as_of')),
    CONSTRAINT valuation_runs_max_age CHECK (price_max_age_seconds >= 0 AND fx_max_age_seconds >= 0),
    CONSTRAINT valuation_runs_state CHECK (state IN ('valid', 'degraded', 'blocked')),
    CONSTRAINT valuation_runs_hash CHECK (result_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE valuation_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES valuation_runs (id),
    snapshot_line_id UUID NOT NULL REFERENCES portfolio_snapshot_lines (id),
    native_currency TEXT NOT NULL,
    native_amount NUMERIC(38,18),
    try_amount NUMERIC(38,18),
    usd_amount NUMERIC(38,18),
    state TEXT NOT NULL,
    reason_codes JSONB NOT NULL DEFAULT '[]'::jsonb,
    price_method TEXT NOT NULL,
    price_revision_id UUID REFERENCES price_revisions (id),
    price_quote_unit TEXT,
    try_fx_quote_revision_ids UUID[] NOT NULL DEFAULT '{}',
    try_fx_directions TEXT[] NOT NULL DEFAULT '{}',
    usd_fx_quote_revision_ids UUID[] NOT NULL DEFAULT '{}',
    usd_fx_directions TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT valuation_lines_state CHECK (state IN ('valid', 'degraded', 'blocked')),
    CONSTRAINT valuation_lines_price_method CHECK (price_method IN ('revision', 'identity')),
    CONSTRAINT valuation_lines_identity_price CHECK ((price_method = 'identity' AND price_revision_id IS NULL AND price_quote_unit IS NULL) OR (price_method = 'revision' AND ((price_revision_id IS NOT NULL AND price_quote_unit IS NOT NULL) OR (state = 'blocked' AND price_revision_id IS NULL AND price_quote_unit IS NULL)))),
    CONSTRAINT valuation_lines_try_fx_parallel_arrays CHECK (cardinality(try_fx_quote_revision_ids) = cardinality(try_fx_directions)),
    CONSTRAINT valuation_lines_usd_fx_parallel_arrays CHECK (cardinality(usd_fx_quote_revision_ids) = cardinality(usd_fx_directions))
);

ALTER TABLE fx_quote_revisions
    ADD CONSTRAINT fx_quote_revisions_rate_positive CHECK (rate > 0);

CREATE INDEX valuation_runs_snapshot_idx ON valuation_runs (snapshot_id, created_at DESC, id DESC);
CREATE INDEX valuation_lines_run_idx ON valuation_lines (run_id, id);

CREATE TRIGGER valuation_runs_immutable
    BEFORE UPDATE OR DELETE ON valuation_runs
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE TRIGGER valuation_lines_immutable
    BEFORE UPDATE OR DELETE ON valuation_lines
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE fx_quote_revisions
    DROP CONSTRAINT IF EXISTS fx_quote_revisions_rate_positive;
DROP TRIGGER IF EXISTS valuation_lines_immutable ON valuation_lines;
DROP TRIGGER IF EXISTS valuation_runs_immutable ON valuation_runs;
DROP TABLE IF EXISTS valuation_lines;
DROP TABLE IF EXISTS valuation_runs;
-- +goose StatementEnd
