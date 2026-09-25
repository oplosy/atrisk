-- +goose Up
-- +goose StatementBegin

CREATE TABLE import_preview_tokens (
    token_digest CHAR(64) PRIMARY KEY,
    import_kind TEXT NOT NULL,
    target_id UUID NOT NULL,
    captured_at TIMESTAMPTZ,
    schema_version TEXT NOT NULL,
    content_sha256 CHAR(64) NOT NULL,
    row_count INTEGER NOT NULL,
    diagnostics JSONB NOT NULL DEFAULT '[]'::jsonb,
    diagnostics_truncated BOOLEAN NOT NULL DEFAULT false,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT import_preview_tokens_kind CHECK (import_kind IN ('positions', 'manual-prices')),
    CONSTRAINT import_preview_tokens_schema CHECK (schema_version = '1.0'),
    CONSTRAINT import_preview_tokens_sha CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT import_preview_tokens_rows CHECK (row_count >= 0 AND row_count <= 25000)
);

CREATE TABLE import_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    import_kind TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    target_id UUID NOT NULL,
    schema_version TEXT NOT NULL,
    content_sha256 CHAR(64) NOT NULL,
    row_count INTEGER NOT NULL,
    raw_object_id UUID NOT NULL REFERENCES raw_objects (id),
    response JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT import_results_kind CHECK (import_kind IN ('positions', 'manual-prices')),
    CONSTRAINT import_results_schema CHECK (schema_version = '1.0'),
    CONSTRAINT import_results_rows CHECK (row_count >= 0 AND row_count <= 25000),
    CONSTRAINT import_results_kind_idempotency UNIQUE (import_kind, idempotency_key)
);

CREATE INDEX import_preview_tokens_expiry_idx ON import_preview_tokens (expires_at);

-- Preview and result rows are application-owned and append-only. No delete or
-- update endpoint exists; the database trigger protects historical evidence.
CREATE TRIGGER import_results_immutable
    BEFORE UPDATE OR DELETE ON import_results
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS import_results_immutable ON import_results;
DROP TABLE IF EXISTS import_results;
DROP TABLE IF EXISTS import_preview_tokens;
-- +goose StatementEnd
