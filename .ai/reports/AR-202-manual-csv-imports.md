# AR-202 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-202-manual-csv-imports.md`
- Packet status at start: `active` (orchestrator dispatched this implementation from the ready queue)
- Referenced ADRs: `ADR-009`, `ADR-010`, `ADR-016`
- Owned paths: `internal/imports/`, `apps/api/handlers/imports/`, `contracts/imports/`, `test/fixtures/imports/`
- Shared paths changed and justification: `db/migrations/00005_manual_csv_imports.sql` adds durable preview-token and idempotency state; `contracts/openapi/openapi.json` publishes the HTTP boundary; `test/integration/import_api_test.go` proves PostgreSQL atomicity and replay behavior. No unrelated shared files were changed.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `contracts/imports/positions-v1.csv`, `manual-prices-v1.csv`, matching JSON Schemas/examples, and OpenAPI import paths. |
| AC-2 | `internal/imports/parser.go` and `parser_test.go`: UTF-8/BOM, RFC4180, strict headers, UUIDs, exact decimal bounds, timestamps, formulas/control bytes, duplicate keys, and bounded diagnostics. |
| AC-3 | `Service.Commit` uses one PostgreSQL transaction for raw-object registration, snapshot/line or price-revision writes, result, and token consumption; `test/integration/import_api_test.go` covers successful snapshot and replay. |
| AC-4 | `import_preview_tokens` stores SHA-256, target, schema, expiry and consumption state; commit rechecks all bindings and bytes. |
| AC-5 | `import_results` has database-enforced `(import_kind,idempotency_key)` uniqueness and replay/conflict handling. |
| AC-6 | Position writes use `NUMERIC` strings and nullable optional fields; price writes preserve quote code, knowledge basis, exact price, and raw object id. |
| AC-7 | `archive.ArchivePayload` is called before transactional raw-object registration; database lineage uses the content hash. |
| AC-8 | Handler returns structured 400/409/413/415 errors and does not log CSV bytes. |
| AC-9 | Migration is ordered after AR-201/AR-105 and integration test is included; hosted migration execution remains required. |

## Stop-condition check

- Decision or scope conflict: main API S3/Garage wiring was not changed because the execution environment blocked a patch that connects uploaded payloads to an environment-configured external archive endpoint; parent/orchestrator must resolve this before merge.
- Missing dependency, unsafe migration, or unavailable verification: local PostgreSQL/Docker was not started; hosted CI must run migration and `ImportAPI` integration. `task` was not invoked locally because infrastructure is intentionally untouched.

## Verification

| Command | Result |
|---|---|
| `go test ./internal/imports ./apps/api/handlers/imports ./test/integration -run 'TestImportAPI\|TestParse' -count=1` | pass |
| `go vet ./apps/... ./internal/...` | pass |
| `go build ./apps/...` | pass |
| `node --test test/contract/contract.test.mjs` | pass, 11 tests (run with elevated child-process permission after sandbox `spawn EPERM`) |
| `git diff --check` | pass |
| `task test-go TEST=CSVImport` | not run; Task CLI/local DB unavailable |
| `task test-go-integration TEST=ImportAPI` | not run; local PostgreSQL intentionally not started |
| `task migrate-test` | not run; local PostgreSQL intentionally not started |
| `task verify` | not run; local infrastructure intentionally untouched |

## Change inventory

- Files changed: strict parser/service/handler, migration, CSV templates/examples/schemas, OpenAPI, PostgreSQL integration test, this report.
- Schema/API changes: `import_preview_tokens`, `import_results`; four preview/commit multipart endpoints.
- Generated artifacts: none; no generated contract source is owned by this packet.

## Git state

- Branch: `task/AR-202-manual-csv-imports`
- Commit SHA: `41827c4eea63a3578e32c0672962386698a55e60`
- Remote branch: not pushed by this worker
- Worktree: clean after commit

## Assumptions and risks

- `target_id` binds manual-price imports to an opaque UUID; position imports use it as `portfolio_id`. Manual-price instrument existence is enforced by the existing foreign key.
- `captured_at` is required for position preview/commit and is persisted as the snapshot capture clock.
- Main API construction still needs safe archive-store wiring before hosted end-to-end API use; direct service/integration coverage is present.
