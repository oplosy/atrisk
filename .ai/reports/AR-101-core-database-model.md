# AR-101 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-101-core-database-model.md`
- Packet status at start: `active` (review follow-up)
- Referenced ADRs: `ADR-004`, `ADR-005`, `ADR-006`, `ADR-007`, `ADR-010`
- Owned paths: `db/migrations/`, `db/queries/core/`, `internal/platform/database/`, `internal/domain/marketdata/`
- Shared paths changed and justification: `Taskfile.yml` adds the sqlc generator invocation and the packet-required migration/Go integration targets; `test/integration/` contains the CoreDatabase acceptance fixtures.

## Result

`needs-review`

The implementation and isolated PostgreSQL acceptance are complete. Runtime
verification used only the explicitly created `atrisk_test` database on the
declared loopback PostgreSQL port; the running `atrisk` development database was
not migrated or reset. Docker settings and services were not changed.

Handoff remains `needs-review` only because the repository-wide Windows
generated-file/Node web gates fail on pre-existing environment/tooling state;
there is no AR-101 runtime failure.

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| Changed values insert new revisions; historical rows cannot be updated/deleted. | `db/migrations/00001_core_database.sql` gives observation, price, and FX revisions immutable triggers; `TestCoreDatabase` inserts changed observation/price/FX values and asserts UPDATE/DELETE fail when an isolated DSN is provided. |
| Identical normalized/raw identities are idempotent under concurrent insert. | `UNIQUE NULLS NOT DISTINCT` identity constraints plus `ON CONFLICT DO NOTHING` query methods; `TestCoreDatabase` races eight identical raw-object, observation, price, and FX inserts and expects one affected row for each identity. |
| Exact values round-trip without precision loss. | All financial values use `NUMERIC(38,18)`; the integration fixture checks `123.456789012345678901` after a database round-trip. |
| Source and system knowledge timestamps may differ or source time may be null. | Revision tables contain nullable `source_known_at`, non-null `system_known_at`, and checked `knowledge_time_basis`; the fixture inserts both source-known and first-observed-by-system rows. |
| Query plans use intended indexes for series/time/as-of fixture queries. | Time, system-as-of, and source-as-of indexes are defined for each revision family; the fixture seeds selective rows, runs default `EXPLAIN (ANALYZE, BUFFERS)`, and asserts observation, price, and FX time/system/source index names without disabling sequential scans. |
| Previous-version migrations upgrade without mutating unrelated data. | `TestCoreDatabasePreviousVersionUpgrade` creates a unique schema in isolated `atrisk_test`, applies v1 with Goose `UpToContext`, verifies no v2 constraint, then calls production `database.MigrateInSchema` and verifies v2 plus the composite FK; cleanup drops only that schema. |
| Source metadata is immutable and ingestion source/dataset identity is enforced. | `00002_core_database_hardening.sql` adds immutable source/dataset/series triggers and a composite `(source_id, dataset_id)` FK; changed metadata inserts return zero and direct source/dataset/series UPDATEs fail. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: none for AR-101 runtime checks. The isolated runtime database was explicitly named and checked before migration.

## Verification

| Command | Result |
|---|---|
| `sqlc generate -f db/queries/core/sqlc.yaml` | pass; generated query layer is deterministic and compiles. |
| `go test ./...` | pass. |
| `go vet ./apps/... ./internal/... ./test/integration` | pass. |
| `go test ./test/integration -run TestTestDatabaseDSNValidation -count=1` | pass; missing, substring-only, remote-host, missing-port, missing-user, query override, duplicate, service, and unsafe SSL DSNs are rejected without opening a connection; explicit URI fields remain isolated under conflicting PostgreSQL environment defaults; `database.Migrate` rejects them at its entrypoint too. |
| `go test -race ./test/integration` | pass against isolated `atrisk_test`; concurrent raw/observation/price/FX idempotency, metadata immutability, source/dataset FK, exact as-of values, and default planner assertions passed. |
| `go test ./test/integration -run TestCoreDatabasePreviousVersionUpgrade -count=1` with required isolated DSN | pass; Goose applied v1 and production `database.MigrateInSchema` applied v2 in a unique schema, while the public `atrisk_test` schema remained separate. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 migrate-test` without DSN | fail closed as required: `ATLASRISK_TEST_DATABASE_URL is required`. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 test-go-integration TEST=CoreDatabase` without DSN | fail closed as required: selected integration tests reject the missing DSN. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 migrate-test` with isolated DSN | pass against `atrisk_test`; migration version 2 and all eight core tables verified, repeated migration is a no-op. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 test-go-integration TEST=CoreDatabase` with isolated DSN | pass against `atrisk_test`; revision, metadata, composite-FK, exact numeric, immutability, concurrency, as-of, and all price/FX index-plan assertions passed. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 check-generated` | the existing AR-005 contract generator leaves the Windows checkout’s tracked generated files marked dirty despite identical content; those unrelated files were restored. sqlc regeneration itself is clean. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 verify` | blocked by the pre-existing web-toolchain environment: `npm run format:check` cannot find `node_modules/.bin/prettier.cmd`; Go and Python format checks passed before that step. |
| `psql ... -d atrisk ... to_regclass('public.goose_db_version')` | pass; returned `f`, confirming the running dev database was not migrated. Temporary `atrisk_test` was dropped after runtime acceptance. |
| `git diff --check` | pass. |

## Change inventory

- Files changed: `db/migrations/00002_core_database_hardening.sql`, `db/queries/core/core.sql`, generated query files under `internal/platform/database/`, `internal/platform/database/migrate.go`, `internal/platform/database/test_target.go`, `test/integration/core_database_test.go`, `Taskfile.yml`, and this report; the original AR-101 implementation remains in the same branch.
- Schema/API changes: immutable source metadata triggers; immutable instrument identity fields with mutable lifecycle status; source/dataset composite FK; immutable/idempotent insert query layer; broader source/system-as-of query coverage; price/FX time/as-of plans and acceptance fixtures; schema-scoped migration helper for isolated previous-version upgrades.
- Generated artifacts: `internal/platform/database/db.go`, `models.go`, `querier.go`, and `core.sql.go` generated by sqlc 1.31.1 from `db/queries/core/`.

## Git state

- Branch: `task/AR-101-core-database-model`
- Reviewed implementation commits: `300ba630941b1a7468b6a0e819e66d5ccc7d6810`, `c10dca2e6fc72d10b2237c6a0c7ec2d08328b901`, `6a8b2280c4dd35adc53f85f8665613029e5d0922`
- Branch SHA at the start of this review: `6e0a545d9ba6a6ae0a1d165ffcabc6f1e1a99402`
- Current report/lifecycle commit: this report-correction commit (final HEAD reported in handoff)
- Remote branch: push succeeded from review SHA `6e0a545` to `0bd2262`; live `ls-remote` was not independently verified because the proxy/remote endpoint remains unavailable.
- Worktree: clean after the report-correction commit

## Assumptions and risks

- The integration target and `database.Migrate` entrypoint accept only `ATLASRISK_TEST_DATABASE_URL` with exact database name `atrisk_test`, loopback host `127.0.0.1`, explicit port, PostgreSQL scheme, and explicit user; credentials are never printed.
- The required Taskfile targets set `ATLASRISK_REQUIRE_TEST_DATABASE=1`, so missing or unsafe DSNs fail rather than silently skipping.
- `00002_core_database_hardening.sql` is forward-only and preserves the already-created `00001` migration for previous-version upgrades.
- The repository-wide generated-file gate has a pre-existing Windows line-ending/index-stat incompatibility in the AR-005 contract generator; CI/Linux should be used as the authoritative cross-platform gate. No AR-005 generated output is included in this task.
- The aggregate verify gate also needs the repository’s npm dependencies installed; this worker did not alter or install web tooling.
