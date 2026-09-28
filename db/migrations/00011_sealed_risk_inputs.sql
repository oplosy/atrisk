-- +goose Up
-- +goose StatementBegin

-- New runs retain the selected valuation and the complete immutable input
-- provenance. Existing AR-303 rows remain readable and are intentionally
-- nullable because they predate sealed valuation binding.
ALTER TABLE scenario_runs
    ADD COLUMN valuation_id UUID REFERENCES valuation_runs (id),
    ADD COLUMN input_provenance JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE risk_jobs
    ADD COLUMN input_hash CHAR(64),
    ADD CONSTRAINT risk_jobs_input_hash CHECK (input_hash IS NULL OR input_hash ~ '^[0-9a-f]{64}$');

CREATE INDEX scenario_runs_valuation_idx ON scenario_runs (valuation_id, created_at DESC, id DESC);

CREATE TABLE scenario_run_factor_attributions (
    run_id UUID NOT NULL REFERENCES scenario_runs (id),
    factor TEXT NOT NULL,
    contribution NUMERIC(38,18) NOT NULL,
    method TEXT NOT NULL,
    method_version TEXT NOT NULL,
    interaction_residual NUMERIC(38,18),
    tolerance NUMERIC(38,18) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (run_id, factor),
    CONSTRAINT scenario_run_factor_attributions_factor_not_blank CHECK (length(btrim(factor)) > 0),
    CONSTRAINT scenario_run_factor_attributions_tolerance_nonnegative CHECK (tolerance >= 0)
);

CREATE TABLE scenario_run_position_attributions (
    run_id UUID NOT NULL REFERENCES scenario_runs (id),
    snapshot_line_id UUID NOT NULL REFERENCES portfolio_snapshot_lines (id),
    instrument_id UUID NOT NULL REFERENCES instruments (id),
    state TEXT NOT NULL,
    total_pnl NUMERIC(38,18),
    factor_contributions JSONB NOT NULL DEFAULT '[]'::jsonb,
    residual NUMERIC(38,18),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (run_id, snapshot_line_id),
    CONSTRAINT scenario_run_position_attributions_state CHECK (state IN ('valid', 'blocked'))
);

CREATE INDEX scenario_run_factor_attributions_run_idx
    ON scenario_run_factor_attributions (run_id, factor);
CREATE INDEX scenario_run_position_attributions_run_idx
    ON scenario_run_position_attributions (run_id, snapshot_line_id);

CREATE TRIGGER scenario_run_factor_attributions_immutable
    BEFORE UPDATE OR DELETE ON scenario_run_factor_attributions
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();
CREATE TRIGGER scenario_run_position_attributions_immutable
    BEFORE UPDATE OR DELETE ON scenario_run_position_attributions
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE OR REPLACE FUNCTION risk_jobs_guard_input()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    IF NEW.kind IS DISTINCT FROM OLD.kind
       OR NEW.schema_version IS DISTINCT FROM OLD.schema_version
       OR NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
       OR NEW.input_snapshot_ids IS DISTINCT FROM OLD.input_snapshot_ids
       OR NEW.input_hash IS DISTINCT FROM OLD.input_hash
       OR NEW.payload IS DISTINCT FROM OLD.payload
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'risk job input fields are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$function$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS scenario_run_position_attributions_immutable ON scenario_run_position_attributions;
DROP TRIGGER IF EXISTS scenario_run_factor_attributions_immutable ON scenario_run_factor_attributions;
DROP TABLE IF EXISTS scenario_run_position_attributions;
DROP TABLE IF EXISTS scenario_run_factor_attributions;
DROP INDEX IF EXISTS scenario_runs_valuation_idx;
ALTER TABLE risk_jobs DROP CONSTRAINT IF EXISTS risk_jobs_input_hash;
ALTER TABLE risk_jobs DROP COLUMN IF EXISTS input_hash;
ALTER TABLE scenario_runs DROP COLUMN IF EXISTS input_provenance;
ALTER TABLE scenario_runs DROP COLUMN IF EXISTS valuation_id;
-- +goose StatementEnd
