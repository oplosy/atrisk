-- Valuation persistence is intentionally kept as explicit SQL in the
-- application service so immutable run/line writes share one transaction.
-- These statements document the contract for future sqlc extraction.

-- name: InsertValuationRun :one
INSERT INTO valuation_runs (snapshot_id, cutoff, knowledge_mode, known_at,
    price_max_age_seconds, fx_max_age_seconds, request, state, result_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: InsertValuationLine :exec
INSERT INTO valuation_lines (run_id, snapshot_line_id, native_currency,
    native_amount, try_amount, usd_amount, state, reason_codes, price_method,
    price_revision_id, fx_quote_revision_ids, fx_directions)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);
