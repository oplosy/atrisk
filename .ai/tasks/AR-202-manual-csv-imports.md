---
id: AR-202
title: Add safe manual CSV imports
status: merged
phase: 2
depends_on: [AR-201]
branch: task/AR-202-manual-csv-imports
base_sha: ff60b94dbd31a623e4ade6fe6eb3950e01e02ae0
owned_paths: [internal/imports/, apps/api/handlers/imports/, contracts/imports/, test/fixtures/imports/]
shared_paths: [contracts/openapi/, contracts/generated/, scripts/generate/contract-models.mjs, test/contract/, test/integration/, apps/api/cmd/api/, db/migrations/, db/queries/portfolio/, db/queries/imports/, db/queries/core/sqlc.yaml, internal/platform/database/, .github/workflows/ci.yml]
adrs: [ADR-009, ADR-010, ADR-016]
---

# AR-202: Add safe manual CSV imports

## Outcome

The user can preview and atomically commit versioned position and manual-price CSV
imports with row-level diagnostics and no partial or executable content.

## In scope

- Publish the exact UTF-8/RFC 4180 templates `positions-v1.csv` and
  `manual-prices-v1.csv` under `contracts/imports/`, with matching JSON Schemas
  and examples. Multipart form field `schema_version` is exactly `1.0`; headers
  are strict and ordered as listed below. Uploaded bytes use the file field
  named `file`.
- Positions template headers: `account_id,instrument_id,quantity,total_cost_basis,modified_duration_years,convexity_years_squared`.
  One file creates one portfolio snapshot; `portfolio_id` and `captured_at`
  (RFC 3339 timestamp with explicit offset) are request fields. Optional numeric
  cells may be empty; all other cells are required.
- Manual-price template headers: `instrument_id,quote_currency,observation_time,price,source_known_at`.
  `observation_time` and optional `source_known_at` are RFC 3339 timestamps
  with explicit offsets; price must be positive. Empty `source_known_at` means
  `first_observed_by_system`; a value maps to `source_effective_at`. Instrument
  identities must exist; imports do not create them.
- Preview/commit APIs use `POST /api/v1/imports/positions/{preview,commit}` and
  `POST /api/v1/imports/manual-prices/{preview,commit}` with multipart CSV body.
  Preview returns an opaque single-use token expiring after 30 minutes, content
  SHA-256, schema version, row count, validity, and bounded diagnostics (maximum
  100 plus a truncation flag). Invalid previews return no commit token. Commit
  requires the token, same target/schema, identical uploaded bytes, and an
  `Idempotency-Key` header. A replay with the same key returns the original
  result even though its preview token has already been consumed.
- Accept files up to 10 MiB and 25,000 data rows; allow optional UTF-8 BOM,
  require valid UTF-8 and comma-delimited RFC 4180 records; reject NUL/control
  bytes, binary/non-CSV content, unknown/duplicate/reordered headers, and extra
  cells. Decimal cells use `.` with no grouping separators and fit exact
  `NUMERIC(38,18)` (20 integer digits, 18 fractional digits). No locale guessing.
- Normalize unit codes to uppercase per ADR-024. Reject formula prefixes (`=`,
  `+`, `@`) and leading control characters in text fields; parse signed decimals
  only in numeric fields. Duplicate position keys `(account_id,instrument_id)`
  and price revision keys `(instrument_id,quote_currency,observation_time,source_known_at)` fail validation.
- Persist preview-token digests/expiry and committed idempotency results in
  PostgreSQL. Uniqueness is `(import_kind,idempotency_key)`; replay with same
  target/schema/content returns the original result, while a changed request
  returns structured `409 Conflict`.
- Commit the complete position snapshot or all manual-price revisions atomically
  with the import result. On successful commit, retain byte-identical original
  CSV in the immutable content-addressed raw archive and link the raw object to
  every imported price revision / import result. Failed preview/commit creates
  no snapshot, price revision, or committed-result row.
- Never execute or log CSV cell content. Diagnostics contain row, column,
  stable code, and safe message but not raw values. Parsing is synchronous and
  bounded; no background job is introduced.

## Out of scope

- XLSX, automatic column guessing, broker-specific formats, or background execution.

## Acceptance criteria

- [x] Published templates, examples, OpenAPI and JSON Schemas agree on version, ordered headers, cell rules, and decimal strings.
- [x] Preview covers valid imports and invalid encoding/BOM/CSV quoting, MIME, headers/cells, row/file bounds, timestamps, UUIDs, precision/scale, locale ambiguity, formulas, and duplicates; diagnostics cap at 100 without raw values.
- [x] Failed preview/commit leaves no snapshots, lines, price revisions, or committed-result rows; multi-row writes are transactionally atomic.
- [x] Preview token binds kind, target, schema, content SHA-256, expiry, and position `captured_at`; changed bytes/target/schema/captured time, reused or expired token, and token-kind mismatch fail without writes.
- [x] Concurrent same-kind commits with the same idempotency key create one result; identical replay returns the original result and changed request returns structured `409`.
- [x] Position import creates one immutable snapshot with exact values and optional cost/risk attributes; price import creates immutable revisions with correct raw object, knowledge-time basis, and exact decimals.
- [x] Successful imports archive byte-identical CSV under its content hash and persist a content-addressed raw-object row; failed imports create no committed raw/domain lineage.
- [x] Both import kinds expose preview/commit APIs with structured `400/409/413/415` errors, no raw CSV listing, and no CSV content in logs.
- [x] PostgreSQL migration passes from empty and AR-201 schemas; token/idempotency constraints are database-enforced and concurrent tests pass.
- [x] `task verify` and generated-contract drift checks pass; report separates local and hosted evidence.

## Required verification

```text
task test-go TEST=Parse
task test-go-integration TEST=ImportAPI
task test-contract
task migrate-test
task verify
```
