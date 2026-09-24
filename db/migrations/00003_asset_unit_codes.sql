-- +goose Up
-- +goose StatementBegin

-- Instrument denominations and price quote units may be provider asset codes
-- such as USDT. Existing values are preserved byte-for-byte by the widening.
ALTER TABLE instruments
    DROP CONSTRAINT instruments_currency_upper,
    ALTER COLUMN native_currency TYPE TEXT USING native_currency::text,
    ADD CONSTRAINT instruments_currency_code CHECK (
        native_currency = upper(native_currency)
        AND native_currency ~ '^[A-Z][A-Z0-9]{2,15}$'
    );

-- PostgreSQL does not allow changing a column type while a dependent view
-- exists. Recreate the projection with its original definition below.
DROP VIEW latest_price_revisions;

ALTER TABLE price_revisions
    DROP CONSTRAINT price_revisions_quote_currency_upper,
    ALTER COLUMN quote_currency TYPE TEXT USING quote_currency::text,
    ADD CONSTRAINT price_revisions_quote_code CHECK (
        quote_currency = upper(quote_currency)
        AND quote_currency ~ '^[A-Z][A-Z0-9]{2,15}$'
    );

CREATE VIEW latest_price_revisions AS
SELECT DISTINCT ON (instrument_id, quote_currency, observation_time) *
FROM price_revisions
ORDER BY instrument_id, quote_currency, observation_time, system_known_at DESC, id DESC;

-- FX quote revisions intentionally retain ISO 4217 fiat-only CHAR(3) fields.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DO $function$
BEGIN
    IF EXISTS (SELECT 1 FROM instruments WHERE length(native_currency) > 3)
       OR EXISTS (SELECT 1 FROM price_revisions WHERE length(quote_currency) > 3) THEN
        RAISE EXCEPTION 'cannot narrow asset unit codes while values longer than three characters exist';
    END IF;
END
$function$;

DROP VIEW latest_price_revisions;

ALTER TABLE price_revisions
    DROP CONSTRAINT price_revisions_quote_code,
    ALTER COLUMN quote_currency TYPE CHAR(3) USING quote_currency::char(3),
    ADD CONSTRAINT price_revisions_quote_currency_upper CHECK (quote_currency = upper(quote_currency));

ALTER TABLE instruments
    DROP CONSTRAINT instruments_currency_code,
    ALTER COLUMN native_currency TYPE CHAR(3) USING native_currency::char(3),
    ADD CONSTRAINT instruments_currency_upper CHECK (native_currency = upper(native_currency));

CREATE VIEW latest_price_revisions AS
SELECT DISTINCT ON (instrument_id, quote_currency, observation_time) *
FROM price_revisions
ORDER BY instrument_id, quote_currency, observation_time, system_known_at DESC, id DESC;

-- +goose StatementEnd
