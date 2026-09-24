-- name: GetDataSourceByCode :one
SELECT * FROM data_sources
WHERE code = $1;

-- name: InsertDataSource :execrows
INSERT INTO data_sources (code, name, adapter_version, metadata)
VALUES ($1, $2, $3, $4)
ON CONFLICT (code) DO NOTHING;

-- name: GetDataset :one
SELECT * FROM datasets
WHERE id = $1;

-- name: GetDatasetByExternalKey :one
SELECT * FROM datasets
WHERE source_id = $1 AND external_key = $2;

-- name: InsertDataset :execrows
INSERT INTO datasets (source_id, external_key, name, metadata)
VALUES ($1, $2, $3, $4)
ON CONFLICT (source_id, external_key) DO NOTHING;

-- name: GetSeries :one
SELECT * FROM series
WHERE id = $1;

-- name: ListSeriesByDataset :many
SELECT * FROM series
WHERE dataset_id = $1
ORDER BY source_code, id;

-- name: GetSeriesBySourceCode :one
SELECT * FROM series
WHERE dataset_id = $1 AND source_code = $2;

-- name: InsertSeries :execrows
INSERT INTO series (
    dataset_id, source_code, name, unit, frequency, seasonal_adjustment,
    source_timezone, freshness_policy
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (dataset_id, source_code) DO NOTHING;

-- name: GetInstrument :one
SELECT * FROM instruments
WHERE id = $1;

-- name: GetInstrumentBySymbol :one
SELECT * FROM instruments
WHERE canonical_symbol = $1;

-- name: InsertInstrument :execrows
INSERT INTO instruments (
    canonical_symbol, instrument_type, native_currency, external_ids, status
)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (canonical_symbol) DO NOTHING;

-- name: GetRawObjectBySHA256 :one
SELECT * FROM raw_objects
WHERE content_sha256 = $1;

-- name: InsertRawObject :execrows
INSERT INTO raw_objects (
    content_sha256, object_key, media_type, byte_length, retrieved_at,
    request_uri, request_metadata, ingestion_run_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (content_sha256) DO NOTHING;

-- name: GetObservationRevision :one
SELECT * FROM observation_revisions
WHERE id = $1;

-- name: ListObservationRevisions :many
SELECT * FROM observation_revisions
WHERE series_id = $1
  AND observation_time >= $2
  AND observation_time < $3
ORDER BY observation_time, system_known_at, id;

-- name: ListObservationsSystemAsOf :many
SELECT * FROM (
    SELECT DISTINCT ON (observation_time) *
    FROM observation_revisions
    WHERE series_id = $1
      AND observation_time >= $2
      AND observation_time < $3
      AND system_known_at <= $4
    ORDER BY observation_time, system_known_at DESC, id DESC
) AS revisions
ORDER BY observation_time, id;

-- name: ListObservationsSourceAsOf :many
SELECT * FROM (
    SELECT DISTINCT ON (observation_time) *
    FROM observation_revisions
    WHERE series_id = $1
      AND observation_time >= $2
      AND observation_time < $3
      AND source_known_at IS NOT NULL
      AND source_known_at <= $4
    ORDER BY observation_time, source_known_at DESC, system_known_at DESC, id DESC
) AS revisions
ORDER BY observation_time, id;

-- name: InsertObservationRevision :execrows
INSERT INTO observation_revisions (
    series_id, observation_time, value, value_text, source_known_at,
    knowledge_time_basis, raw_object_id, quality_flags
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (series_id, observation_time, value, value_text, source_known_at, raw_object_id)
DO NOTHING;

-- name: GetPriceRevision :one
SELECT * FROM price_revisions
WHERE id = $1;

-- name: ListPriceRevisions :many
SELECT * FROM price_revisions
WHERE instrument_id = $1
  AND quote_currency = $2
  AND observation_time >= $3
  AND observation_time < $4
ORDER BY observation_time, system_known_at, id;

-- name: ListPricesSystemAsOf :many
SELECT * FROM (
    SELECT DISTINCT ON (quote_currency, observation_time) *
    FROM price_revisions
    WHERE instrument_id = $1
      AND quote_currency = $2
      AND observation_time >= $3
      AND observation_time < $4
      AND system_known_at <= $5
    ORDER BY quote_currency, observation_time, system_known_at DESC, id DESC
) AS revisions
ORDER BY observation_time, id;

-- name: ListPricesSourceAsOf :many
SELECT * FROM (
    SELECT DISTINCT ON (quote_currency, observation_time) *
    FROM price_revisions
    WHERE instrument_id = $1
      AND quote_currency = $2
      AND observation_time >= $3
      AND observation_time < $4
      AND source_known_at IS NOT NULL
      AND source_known_at <= $5
    ORDER BY quote_currency, observation_time, source_known_at DESC, system_known_at DESC, id DESC
) AS revisions
ORDER BY observation_time, id;

-- name: InsertPriceRevision :execrows
INSERT INTO price_revisions (
    instrument_id, quote_currency, observation_time, price, source_known_at,
    knowledge_time_basis, raw_object_id, quality_flags
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (instrument_id, quote_currency, observation_time, price, source_known_at, raw_object_id)
DO NOTHING;

-- name: GetFXQuoteRevision :one
SELECT * FROM fx_quote_revisions
WHERE id = $1;

-- name: ListFXQuoteRevisions :many
SELECT * FROM fx_quote_revisions
WHERE base_currency = $1
  AND quote_currency = $2
  AND observation_time >= $3
  AND observation_time < $4
ORDER BY observation_time, system_known_at, id;

-- name: ListFXQuotesSystemAsOf :many
SELECT * FROM (
    SELECT DISTINCT ON (base_currency, quote_currency, observation_time) *
    FROM fx_quote_revisions
    WHERE base_currency = $1
      AND quote_currency = $2
      AND observation_time >= $3
      AND observation_time < $4
      AND system_known_at <= $5
    ORDER BY base_currency, quote_currency, observation_time, system_known_at DESC, id DESC
) AS revisions
ORDER BY observation_time, id;

-- name: ListFXQuotesSourceAsOf :many
SELECT * FROM (
    SELECT DISTINCT ON (base_currency, quote_currency, observation_time) *
    FROM fx_quote_revisions
    WHERE base_currency = $1
      AND quote_currency = $2
      AND observation_time >= $3
      AND observation_time < $4
      AND source_known_at IS NOT NULL
      AND source_known_at <= $5
    ORDER BY base_currency, quote_currency, observation_time, source_known_at DESC, system_known_at DESC, id DESC
) AS revisions
ORDER BY observation_time, id;

-- name: InsertFXQuoteRevision :execrows
INSERT INTO fx_quote_revisions (
    base_currency, quote_currency, observation_time, rate, source_known_at,
    knowledge_time_basis, raw_object_id, quality_flags
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (base_currency, quote_currency, observation_time, rate, source_known_at, raw_object_id)
DO NOTHING;
