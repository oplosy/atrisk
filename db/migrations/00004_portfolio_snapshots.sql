-- +goose Up
-- +goose StatementBegin

CREATE TABLE instrument_external_identifiers (
    instrument_id UUID NOT NULL REFERENCES instruments (id),
    namespace TEXT NOT NULL,
    external_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (instrument_id, namespace),
    CONSTRAINT instrument_external_identifiers_namespace_id_key UNIQUE (namespace, external_id),
    CONSTRAINT instrument_external_identifiers_namespace_not_blank CHECK (length(btrim(namespace)) > 0),
    CONSTRAINT instrument_external_identifiers_id_not_blank CHECK (length(btrim(external_id)) > 0)
);

-- Keep the pre-AR-201 JSON representation compatible while introducing a
-- relational uniqueness boundary. New writes update both representations in
-- one transaction; this backfills identities that already existed.
INSERT INTO instrument_external_identifiers (instrument_id, namespace, external_id)
SELECT i.id, entry.key, entry.value
FROM instruments AS i
CROSS JOIN LATERAL jsonb_each_text(i.external_ids) AS entry
WHERE length(btrim(entry.key)) > 0 AND length(btrim(entry.value)) > 0
;

CREATE TRIGGER instrument_external_identifiers_immutable
    BEFORE UPDATE OR DELETE ON instrument_external_identifiers
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE TABLE portfolios (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    reporting_currency TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT portfolios_name_not_blank CHECK (length(btrim(name)) > 0),
    CONSTRAINT portfolios_reporting_currency CHECK (reporting_currency IN ('TRY', 'USD'))
);

CREATE TABLE accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    portfolio_id UUID NOT NULL REFERENCES portfolios (id),
    name TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT accounts_portfolio_name_key UNIQUE (portfolio_id, name),
    CONSTRAINT accounts_portfolio_id_id_key UNIQUE (portfolio_id, id),
    CONSTRAINT accounts_name_not_blank CHECK (length(btrim(name)) > 0)
);

CREATE TABLE portfolio_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    portfolio_id UUID NOT NULL REFERENCES portfolios (id),
    captured_at TIMESTAMPTZ NOT NULL,
    supersedes_snapshot_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT portfolio_snapshots_portfolio_id_id_key UNIQUE (portfolio_id, id),
    CONSTRAINT portfolio_snapshots_not_self_superseding CHECK (supersedes_snapshot_id IS NULL OR supersedes_snapshot_id <> id),
    CONSTRAINT portfolio_snapshots_supersedes_same_portfolio_fk
        FOREIGN KEY (portfolio_id, supersedes_snapshot_id)
        REFERENCES portfolio_snapshots (portfolio_id, id)
);

CREATE TABLE portfolio_snapshot_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    portfolio_id UUID NOT NULL,
    snapshot_id UUID NOT NULL,
    account_id UUID NOT NULL,
    instrument_id UUID NOT NULL REFERENCES instruments (id),
    quantity NUMERIC(38,18) NOT NULL,
    total_cost_basis NUMERIC(38,18),
    modified_duration_years NUMERIC(38,18),
    convexity_years_squared NUMERIC(38,18),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT portfolio_snapshot_lines_snapshot_fk
        FOREIGN KEY (portfolio_id, snapshot_id)
        REFERENCES portfolio_snapshots (portfolio_id, id),
    CONSTRAINT portfolio_snapshot_lines_account_fk
        FOREIGN KEY (portfolio_id, account_id)
        REFERENCES accounts (portfolio_id, id),
    CONSTRAINT portfolio_snapshot_lines_identity_key UNIQUE (snapshot_id, account_id, instrument_id),
    CONSTRAINT portfolio_snapshot_lines_convexity_nonnegative CHECK (
        convexity_years_squared IS NULL OR convexity_years_squared >= 0
    )
);

CREATE OR REPLACE FUNCTION validate_portfolio_snapshot_line()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
DECLARE
    instrument_type_value TEXT;
BEGIN
    SELECT instrument_type INTO instrument_type_value
    FROM instruments WHERE id = NEW.instrument_id;
    IF instrument_type_value IS NULL THEN
        RAISE EXCEPTION 'snapshot line instrument does not exist' USING ERRCODE = '23503';
    END IF;
    IF instrument_type_value = 'fixed_rate_bond' THEN
        IF NEW.modified_duration_years IS NULL OR NEW.modified_duration_years <= 0 THEN
            RAISE EXCEPTION 'fixed-rate bonds require positive modified duration' USING ERRCODE = '23514';
        END IF;
    ELSIF NEW.modified_duration_years IS NOT NULL OR NEW.convexity_years_squared IS NOT NULL THEN
        RAISE EXCEPTION 'risk attributes are unsupported for this instrument type' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE TRIGGER portfolio_snapshot_lines_validate
    BEFORE INSERT ON portfolio_snapshot_lines
    FOR EACH ROW EXECUTE FUNCTION validate_portfolio_snapshot_line();

CREATE TRIGGER portfolio_snapshots_immutable
    BEFORE UPDATE OR DELETE ON portfolio_snapshots
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE TRIGGER portfolio_snapshot_lines_immutable
    BEFORE UPDATE OR DELETE ON portfolio_snapshot_lines
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE INDEX accounts_portfolio_idx ON accounts (portfolio_id, created_at, id);
CREATE INDEX portfolio_snapshots_portfolio_idx ON portfolio_snapshots (portfolio_id, captured_at DESC, id DESC);
CREATE INDEX portfolio_snapshot_lines_snapshot_idx ON portfolio_snapshot_lines (snapshot_id, account_id, instrument_id);
CREATE INDEX instrument_external_identifiers_instrument_idx ON instrument_external_identifiers (instrument_id, namespace);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS portfolio_snapshot_lines_immutable ON portfolio_snapshot_lines;
DROP TRIGGER IF EXISTS portfolio_snapshots_immutable ON portfolio_snapshots;
DROP TRIGGER IF EXISTS portfolio_snapshot_lines_validate ON portfolio_snapshot_lines;
DROP TRIGGER IF EXISTS instrument_external_identifiers_immutable ON instrument_external_identifiers;
DROP FUNCTION IF EXISTS validate_portfolio_snapshot_line();
DROP TABLE IF EXISTS portfolio_snapshot_lines;
DROP TABLE IF EXISTS portfolio_snapshots;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS portfolios;
DROP TABLE IF EXISTS instrument_external_identifiers;

-- +goose StatementEnd
