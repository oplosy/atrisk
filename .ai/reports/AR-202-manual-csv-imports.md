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
| AC-4 | `import_preview_tokens` stores SHA-256, target, schema, captured_at, expiry and consumption state; commit rechecks all bindings and bytes before archive access. Invalid token/target/domain requests are rejected without archive writes. |
| AC-5 | `import_results` has database-enforced `(import_kind,idempotency_key)` uniqueness and captures position `captured_at`; replay is checked before archive access and returns the stored result during archive outage, while changed request clocks conflict. |
| AC-6 | Position writes use `NUMERIC` strings and nullable optional fields; price writes preserve normalized uppercase quote code, knowledge basis, exact price, and raw object id. |
| AC-7 | `archive.ArchivePayload` is called before transactional raw-object registration; database lineage uses the content hash. |
| AC-8 | Handler returns structured 400/409/413/415 errors with a local `import-<UnixNano>` request_id and does not log CSV bytes; `apps/api/cmd/api/main.go` now registers both import route prefixes and configures the established S3/Garage store from CI environment variables. |
| AC-9 | Migration is ordered after AR-201/AR-105 and integration test is included; hosted migration execution remains required. |

## Stop-condition check

- Decision or scope conflict: none. Main API routes now use the existing `ATLASRISK_S3_*`/AWS environment contract.
- Local PostgreSQL/Docker was intentionally not started. Hosted CI must run the migration and `ImportAPI` integration test before merge.

## Verification

| Command | Result |
|---|---|
| `go test ./internal/imports ./apps/api/handlers/imports ./test/integration -run 'TestImportAPI\|TestParse' -count=1` | pass (focused parser/service tests; DB integration requires hosted services) |
| `go test ./test/integration -run '^$' -count=1` | pass, integration package compiles |
| `go test ./apps/api/cmd/api ./internal/imports ./apps/api/handlers/imports -count=1` | pass |
| `go test ./internal/imports -count=1` | pass, including invalid-target and lowercase quote normalization regressions |
| `go test ./apps/api/handlers/imports -count=1` | pass, error envelope request_id regression |
| OpenAPI import commit header validation | pass; both operations reference required `ImportIdempotencyKey` |
| `go test ./test/integration -run '^$' -count=1` | pass; integration package compiles. Captured-at conflict, archive-outage replay, invalid-token no-archive, and positions/manual-price HTTP preview/commit assertions are queued in the hosted `ImportAPI` PostgreSQL job |
| `go vet ./apps/... ./internal/...` | pass |
| `go build ./apps/...` | pass |
| `node --test test/contract/contract.test.mjs` | pass, 11 tests (run with elevated child-process permission after sandbox `spawn EPERM`) |
| `git diff --check` | pass |
| `task test-go TEST=Parse` | not run; Task CLI unavailable locally |
| `task test-go-integration TEST=ImportAPI` | not run; local PostgreSQL intentionally not started |
| `task migrate-test` | not run locally; CI now runs this against its ephemeral PostgreSQL service |
| `task verify` | not run; local infrastructure intentionally untouched |

## Change inventory

- Files changed: strict parser/service/handler, API route/archive wiring, migration, CSV templates/examples/schemas, OpenAPI/CI, PostgreSQL/HTTP integration test, regression tests, this report.
- Schema/API changes: `import_preview_tokens`, `import_results`; four preview/commit multipart endpoints.
- Generated artifacts: none; no generated contract source is owned by this packet.

## Git state

- Branch: `task/AR-202-manual-csv-imports`
- Commit SHA: `cdc14f7915a27dc02f6b1944028dffcfd5ecf45d`
- Remote branch: not pushed by this worker
- Worktree: clean after commit

## Assumptions and risks

- `target_id` binds manual-price imports to an opaque UUID; position imports use it as `portfolio_id`. Manual-price instrument existence is enforced by the existing foreign key.
- `captured_at` is required for position preview/commit and is persisted as the snapshot capture clock.
- Main API construction reads the existing `ATLASRISK_S3_*`/AWS credential environment variables and leaves imports unavailable when archive configuration is absent; this preserves existing API startup behavior while making hosted import commits operational. Multipart regression uses an explicit `text/csv` part because Go's `CreateFormFile` default is `application/octet-stream`. Replay now returns the stored result before archive access, and failed token/domain validation cannot create an archive object.
