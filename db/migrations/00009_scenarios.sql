-- +goose Up
-- +goose StatementBegin

CREATE TABLE scenarios (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts (id),
    name TEXT NOT NULL,
    template_key TEXT NOT NULL,
    current_version INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT scenarios_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT scenarios_template_key CHECK (template_key IN ('try_depreciation', 'rates_up', 'risk_off')),
    CONSTRAINT scenarios_current_version CHECK (current_version >= 0),
    CONSTRAINT scenarios_account_name_key UNIQUE (account_id, name),
    CONSTRAINT scenarios_id_account_key UNIQUE (id, account_id)
);

CREATE TABLE scenario_versions (
    scenario_id UUID NOT NULL REFERENCES scenarios (id),
    version INTEGER NOT NULL,
    template_key TEXT NOT NULL,
    units JSONB NOT NULL,
    shocks JSONB NOT NULL,
    mappings JSONB NOT NULL DEFAULT '{}',
    assumptions JSONB NOT NULL,
    content_hash CHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (scenario_id, version),
    CONSTRAINT scenario_versions_positive_version CHECK (version > 0),
    CONSTRAINT scenario_versions_template_key CHECK (template_key IN ('try_depreciation', 'rates_up', 'risk_off')),
    CONSTRAINT scenario_versions_hash CHECK (content_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE scenario_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scenario_id UUID NOT NULL,
    scenario_version INTEGER NOT NULL,
    account_id UUID NOT NULL REFERENCES accounts (id),
    snapshot_id UUID NOT NULL REFERENCES portfolio_snapshots (id),
    job_id UUID NOT NULL UNIQUE REFERENCES risk_jobs (id),
    state TEXT NOT NULL DEFAULT 'queued',
    result JSONB,
    result_hash CHAR(64),
    request_hash CHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    completed_at TIMESTAMPTZ,
    FOREIGN KEY (scenario_id, scenario_version) REFERENCES scenario_versions (scenario_id, version),
    FOREIGN KEY (scenario_id, account_id) REFERENCES scenarios (id, account_id),
    CONSTRAINT scenario_runs_state CHECK (state IN ('queued', 'running', 'valid', 'degraded', 'blocked', 'failed')),
    CONSTRAINT scenario_runs_result_hash CHECK (result_hash IS NULL OR result_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT scenario_runs_request_hash CHECK (request_hash ~ '^[0-9a-f]{64}$')
);

CREATE TABLE scenario_run_positions (
    run_id UUID NOT NULL REFERENCES scenario_runs (id),
    snapshot_line_id UUID NOT NULL REFERENCES portfolio_snapshot_lines (id),
    instrument_id UUID NOT NULL REFERENCES instruments (id),
    state TEXT NOT NULL,
    reason_codes TEXT[] NOT NULL DEFAULT '{}',
    pre_value_try NUMERIC(38,18), post_value_try NUMERIC(38,18), pnl_try NUMERIC(38,18),
    pre_value_usd NUMERIC(38,18), post_value_usd NUMERIC(38,18), pnl_usd NUMERIC(38,18),
    price_return NUMERIC(38,18), yield_return NUMERIC(38,18),
    fx_multiplier_try NUMERIC(38,18), fx_multiplier_usd NUMERIC(38,18),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (run_id, snapshot_line_id),
    CONSTRAINT scenario_run_positions_state CHECK (state IN ('valid', 'blocked'))
);

CREATE TABLE scenario_run_metrics (
    run_id UUID NOT NULL REFERENCES scenario_runs (id),
    metric_key TEXT NOT NULL,
    pre_value NUMERIC(38,18),
    post_value NUMERIC(38,18),
    state TEXT NOT NULL,
    reason_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (run_id, metric_key),
    CONSTRAINT scenario_run_metrics_state CHECK (state IN ('valid', 'degraded', 'blocked'))
);

CREATE INDEX scenarios_account_idx ON scenarios (account_id, created_at DESC);
CREATE INDEX scenario_runs_account_idx ON scenario_runs (account_id, created_at DESC);

CREATE OR REPLACE FUNCTION scenario_runs_validate_snapshot_account()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM accounts a
        JOIN portfolio_snapshots ps ON ps.portfolio_id = a.portfolio_id
        WHERE a.id = NEW.account_id AND ps.id = NEW.snapshot_id
    ) THEN
        RAISE EXCEPTION 'scenario run snapshot does not belong to account portfolio' USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$function$;
CREATE TRIGGER scenario_runs_snapshot_account
    BEFORE INSERT OR UPDATE OF account_id, snapshot_id ON scenario_runs
    FOR EACH ROW EXECUTE FUNCTION scenario_runs_validate_snapshot_account();

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
CREATE TRIGGER scenario_runs_preserve_identity
    BEFORE UPDATE OR DELETE ON scenario_runs
    FOR EACH ROW EXECUTE FUNCTION scenario_runs_preserve_identity();

CREATE OR REPLACE FUNCTION scenario_run_positions_validate_snapshot_line()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM scenario_runs r
        JOIN portfolio_snapshot_lines l
          ON l.id = NEW.snapshot_line_id
         AND l.snapshot_id = r.snapshot_id
         AND l.account_id = r.account_id
         AND l.instrument_id = NEW.instrument_id
        WHERE r.id = NEW.run_id
    ) THEN
        RAISE EXCEPTION 'scenario result position does not match run snapshot line' USING ERRCODE = '23503';
    END IF;
    RETURN NEW;
END;
$function$;
CREATE TRIGGER scenario_run_positions_snapshot_line
    BEFORE INSERT ON scenario_run_positions
    FOR EACH ROW EXECUTE FUNCTION scenario_run_positions_validate_snapshot_line();

CREATE OR REPLACE FUNCTION scenario_versions_append_only()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    RAISE EXCEPTION 'scenario versions are append-only' USING ERRCODE = '55000';
END;
$function$;
CREATE TRIGGER scenario_versions_immutable
    BEFORE UPDATE OR DELETE ON scenario_versions
    FOR EACH ROW EXECUTE FUNCTION scenario_versions_append_only();

CREATE OR REPLACE FUNCTION scenario_results_append_only()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    RAISE EXCEPTION 'scenario run evidence is append-only' USING ERRCODE = '55000';
END;
$function$;
CREATE TRIGGER scenario_run_positions_immutable
    BEFORE UPDATE OR DELETE ON scenario_run_positions
    FOR EACH ROW EXECUTE FUNCTION scenario_results_append_only();
CREATE TRIGGER scenario_run_metrics_immutable
    BEFORE UPDATE OR DELETE ON scenario_run_metrics
    FOR EACH ROW EXECUTE FUNCTION scenario_results_append_only();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS scenario_runs_snapshot_account ON scenario_runs;
DROP FUNCTION IF EXISTS scenario_runs_validate_snapshot_account();
DROP TRIGGER IF EXISTS scenario_runs_preserve_identity ON scenario_runs;
DROP FUNCTION IF EXISTS scenario_runs_preserve_identity();
DROP TRIGGER IF EXISTS scenario_run_positions_snapshot_line ON scenario_run_positions;
DROP FUNCTION IF EXISTS scenario_run_positions_validate_snapshot_line();
DROP TRIGGER IF EXISTS scenario_run_metrics_immutable ON scenario_run_metrics;
DROP TRIGGER IF EXISTS scenario_run_positions_immutable ON scenario_run_positions;
DROP TRIGGER IF EXISTS scenario_versions_immutable ON scenario_versions;
DROP FUNCTION IF EXISTS scenario_results_append_only();
DROP FUNCTION IF EXISTS scenario_versions_append_only();
DROP TABLE IF EXISTS scenario_run_metrics;
DROP TABLE IF EXISTS scenario_run_positions;
DROP TABLE IF EXISTS scenario_runs;
DROP TABLE IF EXISTS scenario_versions;
DROP TABLE IF EXISTS scenarios;
-- +goose StatementEnd
