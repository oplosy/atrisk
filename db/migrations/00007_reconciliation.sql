-- +goose Up
-- +goose StatementBegin

CREATE TABLE reconciliation_tolerance_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts (id),
    version INTEGER NOT NULL,
    tolerance_amount NUMERIC(38,18) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT reconciliation_tolerance_versions_amount_nonnegative CHECK (tolerance_amount >= 0 AND tolerance_amount NOT IN ('NaN'::numeric, 'Infinity'::numeric, '-Infinity'::numeric)),
    CONSTRAINT reconciliation_tolerance_versions_account_version_key UNIQUE (account_id, version)
);

CREATE TABLE reconciliation_checkpoints (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    valuation_id UUID NOT NULL REFERENCES valuation_runs (id),
    account_id UUID NOT NULL REFERENCES accounts (id),
    source_label TEXT NOT NULL,
    currency TEXT NOT NULL,
    cutoff TIMESTAMPTZ NOT NULL,
    external_nav NUMERIC(38,18) NOT NULL,
    valuation_nav NUMERIC(38,18) NOT NULL,
    absolute_difference NUMERIC(38,18) NOT NULL,
    relative_difference NUMERIC(38,18),
    effective_tolerance NUMERIC(38,18) NOT NULL,
    tolerance_version INTEGER NOT NULL,
    state TEXT NOT NULL,
    reason_code TEXT,
    line_check_state TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT reconciliation_checkpoints_currency CHECK (currency IN ('TRY', 'USD')),
    CONSTRAINT reconciliation_checkpoints_external_nav_finite CHECK (external_nav NOT IN ('NaN'::numeric, 'Infinity'::numeric, '-Infinity'::numeric)),
    CONSTRAINT reconciliation_checkpoints_difference_nonnegative CHECK (absolute_difference >= 0),
    CONSTRAINT reconciliation_checkpoints_tolerance_nonnegative CHECK (effective_tolerance >= 0 AND effective_tolerance NOT IN ('NaN'::numeric, 'Infinity'::numeric, '-Infinity'::numeric)),
    CONSTRAINT reconciliation_checkpoints_relative_nonnegative CHECK (relative_difference IS NULL OR relative_difference >= 0),
    CONSTRAINT reconciliation_checkpoints_tolerance_version_nonnegative CHECK (tolerance_version >= 0),
    CONSTRAINT reconciliation_checkpoints_state CHECK (state IN ('reconciled', 'unreconciled')),
    CONSTRAINT reconciliation_checkpoints_line_state CHECK (line_check_state IN ('none', 'partial', 'complete'))
);

CREATE TABLE reconciliation_line_checks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reconciliation_id UUID NOT NULL REFERENCES reconciliation_checkpoints (id),
    snapshot_line_id UUID NOT NULL REFERENCES portfolio_snapshot_lines (id),
    external_amount NUMERIC(38,18) NOT NULL,
    valuation_amount NUMERIC(38,18) NOT NULL,
    difference NUMERIC(38,18) NOT NULL,
    absolute_difference NUMERIC(38,18) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT reconciliation_line_checks_difference_nonnegative CHECK (absolute_difference >= 0),
    CONSTRAINT reconciliation_line_checks_unique_line UNIQUE (reconciliation_id, snapshot_line_id)
);

ALTER TABLE valuation_lines
    ADD CONSTRAINT valuation_lines_run_snapshot_line_key UNIQUE (run_id, snapshot_line_id);

CREATE OR REPLACE FUNCTION validate_valuation_line_snapshot()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
DECLARE
    run_snapshot_id UUID;
    line_snapshot_id UUID;
BEGIN
    SELECT snapshot_id INTO run_snapshot_id FROM valuation_runs WHERE id = NEW.run_id;
    SELECT snapshot_id INTO line_snapshot_id FROM portfolio_snapshot_lines WHERE id = NEW.snapshot_line_id;
    IF run_snapshot_id IS NULL OR line_snapshot_id IS NULL OR run_snapshot_id <> line_snapshot_id THEN
        RAISE EXCEPTION 'valuation line does not belong to the valuation snapshot' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE TRIGGER valuation_lines_snapshot_consistency
    BEFORE INSERT ON valuation_lines
    FOR EACH ROW EXECUTE FUNCTION validate_valuation_line_snapshot();

CREATE OR REPLACE FUNCTION validate_reconciliation_checkpoint()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
DECLARE
    valuation_snapshot_id UUID;
    valuation_portfolio_id UUID;
    account_portfolio_id UUID;
BEGIN
    SELECT ps.id, ps.portfolio_id INTO valuation_snapshot_id, valuation_portfolio_id
    FROM valuation_runs vr
    JOIN portfolio_snapshots ps ON ps.id = vr.snapshot_id
    WHERE vr.id = NEW.valuation_id;
    SELECT portfolio_id INTO account_portfolio_id FROM accounts WHERE id = NEW.account_id;
    IF valuation_snapshot_id IS NULL OR account_portfolio_id IS NULL OR valuation_portfolio_id <> account_portfolio_id THEN
        RAISE EXCEPTION 'reconciliation account and valuation portfolio do not match' USING ERRCODE = '23514';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM portfolio_snapshot_lines
        WHERE snapshot_id = valuation_snapshot_id AND account_id = NEW.account_id
    ) THEN
        RAISE EXCEPTION 'reconciliation account is not in valuation snapshot' USING ERRCODE = '23514';
    END IF;
    IF NEW.cutoff <> (SELECT cutoff FROM valuation_runs WHERE id = NEW.valuation_id) THEN
        RAISE EXCEPTION 'reconciliation cutoff does not match valuation cutoff' USING ERRCODE = '23514';
    END IF;
    IF NEW.tolerance_version > 0 AND NOT EXISTS (
        SELECT 1 FROM reconciliation_tolerance_versions
        WHERE account_id = NEW.account_id AND version = NEW.tolerance_version
          AND tolerance_amount = NEW.effective_tolerance
    ) THEN
        RAISE EXCEPTION 'reconciliation tolerance version does not belong to account' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE TRIGGER reconciliation_checkpoint_consistency
    BEFORE INSERT ON reconciliation_checkpoints
    FOR EACH ROW EXECUTE FUNCTION validate_reconciliation_checkpoint();

CREATE OR REPLACE FUNCTION validate_reconciliation_line_check()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
DECLARE
    checkpoint_snapshot_id UUID;
    checkpoint_account_id UUID;
    checkpoint_valuation_id UUID;
BEGIN
    SELECT ps.id, rc.account_id, rc.valuation_id
      INTO checkpoint_snapshot_id, checkpoint_account_id, checkpoint_valuation_id
    FROM reconciliation_checkpoints rc
    JOIN valuation_runs vr ON vr.id = rc.valuation_id
    JOIN portfolio_snapshots ps ON ps.id = vr.snapshot_id
    WHERE rc.id = NEW.reconciliation_id;
    IF checkpoint_snapshot_id IS NULL OR NOT EXISTS (
        SELECT 1 FROM portfolio_snapshot_lines
        WHERE id = NEW.snapshot_line_id
          AND snapshot_id = checkpoint_snapshot_id
          AND account_id = checkpoint_account_id
    ) OR NOT EXISTS (
        SELECT 1 FROM valuation_lines
        WHERE run_id = checkpoint_valuation_id AND snapshot_line_id = NEW.snapshot_line_id
    ) THEN
        RAISE EXCEPTION 'reconciliation line check is outside checkpoint account snapshot' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE TRIGGER reconciliation_line_check_consistency
    BEFORE INSERT ON reconciliation_line_checks
    FOR EACH ROW EXECUTE FUNCTION validate_reconciliation_line_check();

CREATE INDEX reconciliation_tolerance_versions_account_idx ON reconciliation_tolerance_versions (account_id, version DESC);
CREATE INDEX reconciliation_checkpoints_valuation_idx ON reconciliation_checkpoints (valuation_id, created_at DESC, id DESC);
CREATE INDEX reconciliation_checkpoints_account_idx ON reconciliation_checkpoints (account_id, created_at DESC, id DESC);
CREATE INDEX reconciliation_line_checks_checkpoint_idx ON reconciliation_line_checks (reconciliation_id, snapshot_line_id);

CREATE TRIGGER reconciliation_tolerance_versions_immutable BEFORE UPDATE OR DELETE ON reconciliation_tolerance_versions
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();
CREATE TRIGGER reconciliation_checkpoints_immutable BEFORE UPDATE OR DELETE ON reconciliation_checkpoints
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();
CREATE TRIGGER reconciliation_line_checks_immutable BEFORE UPDATE OR DELETE ON reconciliation_line_checks
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS reconciliation_line_checks_immutable ON reconciliation_line_checks;
DROP TRIGGER IF EXISTS reconciliation_line_check_consistency ON reconciliation_line_checks;
DROP TRIGGER IF EXISTS reconciliation_checkpoint_consistency ON reconciliation_checkpoints;
DROP TRIGGER IF EXISTS valuation_lines_snapshot_consistency ON valuation_lines;
DROP FUNCTION IF EXISTS validate_reconciliation_line_check();
DROP FUNCTION IF EXISTS validate_reconciliation_checkpoint();
DROP FUNCTION IF EXISTS validate_valuation_line_snapshot();
DROP TRIGGER IF EXISTS reconciliation_checkpoints_immutable ON reconciliation_checkpoints;
DROP TRIGGER IF EXISTS reconciliation_tolerance_versions_immutable ON reconciliation_tolerance_versions;
DROP TABLE IF EXISTS reconciliation_line_checks;
DROP TABLE IF EXISTS reconciliation_checkpoints;
DROP TABLE IF EXISTS reconciliation_tolerance_versions;
ALTER TABLE valuation_lines DROP CONSTRAINT IF EXISTS valuation_lines_run_snapshot_line_key;
-- +goose StatementEnd
