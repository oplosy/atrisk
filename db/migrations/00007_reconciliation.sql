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
DROP TRIGGER IF EXISTS reconciliation_checkpoints_immutable ON reconciliation_checkpoints;
DROP TRIGGER IF EXISTS reconciliation_tolerance_versions_immutable ON reconciliation_tolerance_versions;
DROP TABLE IF EXISTS reconciliation_line_checks;
DROP TABLE IF EXISTS reconciliation_checkpoints;
DROP TABLE IF EXISTS reconciliation_tolerance_versions;
-- +goose StatementEnd
