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
    CONSTRAINT scenario_run_factor_attributions_method_not_blank CHECK (length(btrim(method)) > 0),
    CONSTRAINT scenario_run_factor_attributions_version_not_blank CHECK (length(btrim(method_version)) > 0),
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
    CONSTRAINT scenario_run_position_attributions_state CHECK (state IN ('valid', 'blocked')),
    CONSTRAINT scenario_run_position_attributions_factors_array CHECK (jsonb_typeof(factor_contributions) = 'array')
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

-- A scenario run may only bind a valuation made for the same account and
-- snapshot. This keeps direct database callers subject to the same seal as the
-- application service.
CREATE OR REPLACE FUNCTION scenario_runs_validate_valuation_binding()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    IF NEW.valuation_id IS NOT NULL AND NOT EXISTS (
        SELECT 1
        FROM valuation_runs v
        JOIN portfolio_snapshots ps ON ps.id = v.snapshot_id
        JOIN accounts a ON a.portfolio_id = ps.portfolio_id
        WHERE v.id = NEW.valuation_id
          AND v.snapshot_id = NEW.snapshot_id
          AND a.id = NEW.account_id
    ) THEN
        RAISE EXCEPTION 'scenario run valuation does not belong to account snapshot' USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$function$;
CREATE TRIGGER scenario_runs_valuation_binding
    BEFORE INSERT OR UPDATE OF account_id, snapshot_id, valuation_id ON scenario_runs
    FOR EACH ROW EXECUTE FUNCTION scenario_runs_validate_valuation_binding();

-- AR-303 identity protection predates valuation binding. Preserve the old
-- lifecycle semantics while extending immutability to the sealed fields.
CREATE OR REPLACE FUNCTION scenario_runs_preserve_identity()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'scenario runs are immutable evidence' USING ERRCODE = '55000';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.scenario_id IS DISTINCT FROM OLD.scenario_id
       OR NEW.scenario_version IS DISTINCT FROM OLD.scenario_version
       OR NEW.account_id IS DISTINCT FROM OLD.account_id
       OR NEW.snapshot_id IS DISTINCT FROM OLD.snapshot_id
       OR NEW.valuation_id IS DISTINCT FROM OLD.valuation_id
       OR NEW.job_id IS DISTINCT FROM OLD.job_id
       OR NEW.request_hash IS DISTINCT FROM OLD.request_hash
       OR NEW.input_provenance IS DISTINCT FROM OLD.input_provenance
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'scenario run identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.state NOT IN ('queued', 'running') THEN
        RAISE EXCEPTION 'completed scenario runs are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$function$;

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
DROP TRIGGER IF EXISTS scenario_runs_valuation_binding ON scenario_runs;
DROP FUNCTION IF EXISTS scenario_runs_validate_valuation_binding();
DROP TRIGGER IF EXISTS scenario_run_position_attributions_immutable ON scenario_run_position_attributions;
DROP TRIGGER IF EXISTS scenario_run_factor_attributions_immutable ON scenario_run_factor_attributions;
DROP TABLE IF EXISTS scenario_run_position_attributions;
DROP TABLE IF EXISTS scenario_run_factor_attributions;
DROP INDEX IF EXISTS scenario_runs_valuation_idx;
ALTER TABLE risk_jobs DROP CONSTRAINT IF EXISTS risk_jobs_input_hash;
ALTER TABLE risk_jobs DROP COLUMN IF EXISTS input_hash;
ALTER TABLE scenario_runs DROP COLUMN IF EXISTS input_provenance;
ALTER TABLE scenario_runs DROP COLUMN IF EXISTS valuation_id;

CREATE OR REPLACE FUNCTION scenario_runs_preserve_identity()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'scenario runs are immutable evidence' USING ERRCODE = '55000';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.scenario_id IS DISTINCT FROM OLD.scenario_id
       OR NEW.scenario_version IS DISTINCT FROM OLD.scenario_version
       OR NEW.account_id IS DISTINCT FROM OLD.account_id
       OR NEW.snapshot_id IS DISTINCT FROM OLD.snapshot_id
       OR NEW.job_id IS DISTINCT FROM OLD.job_id
       OR NEW.request_hash IS DISTINCT FROM OLD.request_hash
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'scenario run identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.state NOT IN ('queued', 'running') THEN
        RAISE EXCEPTION 'completed scenario runs are immutable' USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$function$;

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
-- +goose StatementEnd
