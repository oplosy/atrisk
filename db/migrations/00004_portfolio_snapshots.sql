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
-- relational uniqueness boundary. The old Binance writer stores provider
-- metadata, not a unique external identifier; canonical symbols are the
-- stable bridge identifier for those rows.
INSERT INTO instrument_external_identifiers (instrument_id, namespace, external_id)
SELECT i.id, entry.key, entry.value
FROM instruments AS i
CROSS JOIN LATERAL jsonb_each_text(CASE WHEN jsonb_typeof(i.external_ids) = 'object' THEN i.external_ids ELSE '{}'::jsonb END) AS entry
WHERE i.external_ids->>'provider' = 'binance'
  AND entry.key = 'binance_symbol'
  AND length(btrim(entry.value)) > 0
ON CONFLICT DO NOTHING;

INSERT INTO instrument_external_identifiers (instrument_id, namespace, external_id)
SELECT i.id, 'binance.symbol', i.canonical_symbol
FROM instruments AS i
WHERE i.external_ids->>'provider' = 'binance'
ON CONFLICT DO NOTHING;

-- Other legacy JSON keys are backfilled only when their pair is unambiguous.
-- Conflicting provider/status metadata remains intact in JSON and cannot make
-- an upgrade fail or silently claim a false unique identity.
WITH legacy AS (
    SELECT i.id AS instrument_id, entry.key AS namespace, entry.value AS external_id
    FROM instruments AS i
    CROSS JOIN LATERAL jsonb_each_text(CASE WHEN jsonb_typeof(i.external_ids) = 'object' THEN i.external_ids ELSE '{}'::jsonb END) AS entry
    WHERE entry.key NOT IN ('provider', 'upstream_status', 'binance_symbol')
      AND length(btrim(entry.key)) > 0
      AND length(btrim(entry.value)) > 0
), unambiguous AS (
    SELECT namespace, external_id
    FROM legacy
    GROUP BY namespace, external_id
    HAVING count(*) = 1
)
INSERT INTO instrument_external_identifiers (instrument_id, namespace, external_id)
SELECT legacy.instrument_id, legacy.namespace, legacy.external_id
FROM legacy
JOIN unambiguous USING (namespace, external_id)
ON CONFLICT DO NOTHING;

CREATE OR REPLACE FUNCTION ensure_binance_external_identifier()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    IF NEW.external_ids->>'provider' = 'binance' THEN
        IF EXISTS (
            SELECT 1 FROM instrument_external_identifiers
            WHERE instrument_id = NEW.id
              AND namespace = 'binance.symbol'
              AND external_id <> NEW.canonical_symbol
        ) THEN
            RAISE EXCEPTION 'Binance canonical symbol conflicts with normalized identifier'
                USING ERRCODE = '23514';
        END IF;
        INSERT INTO instrument_external_identifiers (instrument_id, namespace, external_id)
        VALUES (NEW.id, 'binance.symbol', NEW.canonical_symbol);
    END IF;
    RETURN NEW;
END;
$function$;

CREATE TRIGGER instruments_binance_external_identifier
    AFTER INSERT OR UPDATE ON instruments
    FOR EACH ROW EXECUTE FUNCTION ensure_binance_external_identifier();

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
DROP TRIGGER IF EXISTS instruments_binance_external_identifier ON instruments;
DROP FUNCTION IF EXISTS ensure_binance_external_identifier();
DROP FUNCTION IF EXISTS validate_portfolio_snapshot_line();
DROP TABLE IF EXISTS portfolio_snapshot_lines;
DROP TABLE IF EXISTS portfolio_snapshots;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS portfolios;
DROP TABLE IF EXISTS instrument_external_identifiers;

-- +goose StatementEnd
