-- name: CreatePortfolioInstrument :one
INSERT INTO instruments (canonical_symbol, instrument_type, native_currency, external_ids, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListPortfolioInstruments :many
SELECT * FROM instruments
ORDER BY canonical_symbol, id;

-- name: GetPortfolioInstrument :one
SELECT * FROM instruments WHERE id = $1;

-- name: UpdatePortfolioInstrumentStatus :one
UPDATE instruments
SET status = $2
WHERE id = $1
RETURNING *;

-- name: ListInstrumentExternalIdentifiers :many
SELECT namespace, external_id
FROM instrument_external_identifiers
WHERE instrument_id = $1
ORDER BY namespace, external_id;

-- name: InsertInstrumentExternalIdentifier :execrows
INSERT INTO instrument_external_identifiers (instrument_id, namespace, external_id)
VALUES ($1, $2, $3)
ON CONFLICT (instrument_id, namespace) DO NOTHING;

-- name: CreatePortfolio :one
INSERT INTO portfolios (name, reporting_currency, metadata)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListPortfolios :many
SELECT * FROM portfolios ORDER BY name, id;

-- name: GetPortfolio :one
SELECT * FROM portfolios WHERE id = $1;

-- name: UpdatePortfolio :one
UPDATE portfolios
SET name = $2, reporting_currency = $3, metadata = $4, updated_at = clock_timestamp()
WHERE id = $1
RETURNING *;

-- name: DeletePortfolio :execrows
DELETE FROM portfolios WHERE id = $1;

-- name: CreateAccount :one
INSERT INTO accounts (portfolio_id, name, metadata)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListAccounts :many
SELECT * FROM accounts WHERE portfolio_id = $1 ORDER BY name, id;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = $1;

-- name: UpdateAccount :one
UPDATE accounts
SET name = $2, metadata = $3, updated_at = clock_timestamp()
WHERE id = $1
RETURNING *;

-- name: DeleteAccount :execrows
DELETE FROM accounts WHERE id = $1;

-- name: CreatePortfolioSnapshot :one
INSERT INTO portfolio_snapshots (portfolio_id, captured_at, supersedes_snapshot_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListPortfolioSnapshots :many
SELECT * FROM portfolio_snapshots
WHERE portfolio_id = $1
ORDER BY captured_at DESC, id DESC;

-- name: GetPortfolioSnapshot :one
SELECT * FROM portfolio_snapshots WHERE id = $1;

-- name: InsertPortfolioSnapshotLine :one
INSERT INTO portfolio_snapshot_lines (
    portfolio_id, snapshot_id, account_id, instrument_id, quantity,
    total_cost_basis, modified_duration_years, convexity_years_squared
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListPortfolioSnapshotLines :many
SELECT
    l.id, l.portfolio_id, l.snapshot_id, l.account_id, l.instrument_id,
    l.quantity, l.total_cost_basis, l.modified_duration_years,
    l.convexity_years_squared, l.created_at,
    a.name AS account_name,
    i.canonical_symbol, i.instrument_type, i.native_currency, i.status
FROM portfolio_snapshot_lines AS l
JOIN accounts AS a ON a.id = l.account_id
JOIN instruments AS i ON i.id = l.instrument_id
WHERE l.snapshot_id = $1
ORDER BY a.name, i.canonical_symbol, l.id;

-- name: GetPortfolioSnapshotLineCount :one
SELECT count(*)::bigint
FROM portfolio_snapshot_lines
WHERE snapshot_id = $1;
