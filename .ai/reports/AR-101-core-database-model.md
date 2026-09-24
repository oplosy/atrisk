# AR-101 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-101-core-database-model.md`
- Packet status at start: `active` (review follow-up)
- Referenced ADRs: `ADR-004`, `ADR-005`, `ADR-006`, `ADR-007`, `ADR-010`
- Owned paths: `db/migrations/`, `db/queries/core/`, `internal/platform/database/`, `internal/domain/marketdata/`
- Shared paths changed and justification: `Taskfile.yml` replaces the foundation scope placeholder with the packet-owned CoreDatabase integration runner and sets fail-closed test-DSN enforcement; `.github/workflows/ci.yml` provides the required ephemeral PostgreSQL service for the strict CI runner; `test/integration/` contains the CoreDatabase acceptance fixtures.

## Result

`complete`

The implementation and isolated PostgreSQL acceptance are complete. Runtime
verification used only the explicitly created `atrisk_test` database on the
declared loopback PostgreSQL port; the running `atrisk` development database was
not migrated or reset. Docker settings and services were not changed.
GitHub Actions now provisions only its hosted-job PostgreSQL 18.6 service,
using the approved digest, exact `atrisk_test` database, CI-only credentials,
loopback exposure, and a health check.

The required GitHub Actions `Verify` gate passed on PR #10 after CI was configured
with the pinned PostgreSQL service and sqlc generator. The local Windows
`check-generated` command still reports four pre-existing AR-005 contract outputs
rewritten by line-ending normalization; those outputs were restored, and the
Linux CI gate passes on the merged commit.

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| Changed values insert new revisions; historical rows cannot be updated/deleted. | `db/migrations/00001_core_database.sql` gives observation, price, and FX revisions immutable triggers; `TestCoreDatabase` inserts changed observation/price/FX values and asserts UPDATE/DELETE fail when an isolated DSN is provided. |
| Identical normalized/raw identities are idempotent under concurrent insert. | `UNIQUE NULLS NOT DISTINCT` identity constraints plus `ON CONFLICT DO NOTHING` query methods; `TestCoreDatabase` races eight identical raw-object, observation, price, and FX inserts and expects one affected row for each identity. |
| Exact values round-trip without precision loss. | All financial values use `NUMERIC(38,18)`; the integration fixture checks `123.456789012345678901` after a database round-trip. |
| Source and system knowledge timestamps may differ or source time may be null. | Revision tables contain nullable `source_known_at`, non-null `system_known_at`, and checked `knowledge_time_basis`; the fixture inserts both source-known and first-observed-by-system rows. |
| Query plans use intended indexes for series/time/as-of fixture queries. | Time, system-as-of, and source-as-of indexes are defined for each revision family; the fixture seeds selective rows, runs default `EXPLAIN (ANALYZE, BUFFERS)`, and asserts observation, price, and FX time/system/source index names without disabling sequential scans. |
| Previous-version migrations upgrade without mutating unrelated data. | `TestCoreDatabasePreviousVersionUpgrade` creates a unique schema in isolated `atrisk_test`, applies v1 with Goose `UpToContext`, inserts a v1 sentinel row, verifies no v2 constraint, then calls production `database.MigrateInSchema` and verifies v2, the composite FK, and sentinel preservation; cleanup drops only that schema. |
| Source metadata is immutable and ingestion source/dataset identity is enforced. | `00002_core_database_hardening.sql` adds immutable source/dataset/series triggers and a composite `(source_id, dataset_id)` FK; changed metadata inserts return zero and direct source/dataset/series UPDATEs fail. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: none for AR-101 runtime checks. The isolated runtime database was explicitly named and checked before migration.

## Verification

| Command | Result |
|---|---|
| `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate -f db/queries/core/sqlc.yaml` | pass; pinned sqlc v1.31.1 generated deterministic query output with no tracked query-file drift. |
| `go test ./...` | pass. |
| `go vet ./apps/... ./internal/... ./test/integration` | pass. |
| `go test ./test/integration -run TestTestDatabaseDSNValidation -count=1` | pass; missing, substring-only, remote-host, missing-port, missing-user, query override, duplicate, service, and unsafe SSL DSNs are rejected without opening a connection; explicit URI fields remain isolated under conflicting PostgreSQL environment defaults; `database.Migrate` rejects them at its entrypoint too. |
| `go test -race ./test/integration` | pass against isolated `atrisk_test`; concurrent raw/observation/price/FX idempotency, metadata immutability, source/dataset FK, exact as-of values, and default planner assertions passed. |
| `go test ./test/integration -run TestCoreDatabasePreviousVersionUpgrade -count=1` with required isolated DSN | pass; Goose applied v1 and production `database.MigrateInSchema` applied v2 in a unique schema, while the public `atrisk_test` schema remained separate. |
| `go test ./test/integration -run 'TestCoreDatabase' -count=1` with `ATLASRISK_REQUIRE_TEST_DATABASE=1` and no DSN | fail closed as required: all selected tests report `test database URL is required`. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 migrate-test` with isolated DSN | pass against `atrisk_test`; migration version 2 and all eight core tables verified, repeated migration is a no-op. The isolated database was removed after tests with zero active sessions. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 test-go-integration TEST=CoreDatabase` with isolated DSN | pass against `atrisk_test`; revision, metadata, composite-FK, exact numeric, immutability, concurrency, as-of, and all price/FX index-plan assertions passed. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 check-generated` | fails at the existing AR-005 contract generator: exactly four tracked contract outputs are rewritten on this Windows checkout; only those four outputs were restored. sqlc regeneration and all preceding gates are clean. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 verify` with isolated DSN | format, lint, typecheck, unit, contract, AR-101 integration, and build stages pass; final `check-generated` fails only on the same four AR-005 contract outputs. No Docker or service lifecycle command ran. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 migrate-test` and `test-go-integration TEST=CoreDatabase` after pinned-generator change | both pass against isolated `atrisk_test`; the database was removed afterward with zero active sessions. |
| `.github/workflows/ci.yml` service definition | pinned PostgreSQL 18.6-bookworm digest, exact `atrisk_test` initialization, dedicated CI-only role/password, loopback port mapping, and `pg_isready` health check; local Docker/services were not used. |
| GitHub Actions `Verify` after final task commit | pass on PR #10 run `36026465017`; all `task verify` stages passed with the isolated PostgreSQL service. |
| GitHub Actions `Verify` for PR #10 | pass on run `36025959556` after adding the pinned sqlc invocation and isolated PostgreSQL service; includes full `task verify` with the migration and CoreDatabase integration suite. |
| `psql ... -d atrisk ... to_regclass('public.goose_db_version')` | pass; returned `f`, confirming the running dev database was not migrated. The explicitly isolated `atrisk_test` database was removed after runtime acceptance with no active sessions. |
| `git diff --check` | pass. |

## Change inventory

- Files changed: `db/migrations/00002_core_database_hardening.sql`, `db/queries/core/core.sql`, generated query files under `internal/platform/database/`, `internal/platform/database/migrate.go`, `internal/platform/database/test_target.go`, `test/integration/core_database_test.go`, `Taskfile.yml`, and this report; the original AR-101 implementation remains in the same branch.
- Schema/API changes: immutable source metadata triggers; immutable instrument identity fields with mutable lifecycle status; source/dataset composite FK; immutable/idempotent insert query layer; broader source/system-as-of query coverage; price/FX time/as-of plans and acceptance fixtures; schema-scoped migration helper for isolated previous-version upgrades.
- Generated artifacts: `internal/platform/database/db.go`, `models.go`, `querier.go`, and `core.sql.go` generated by sqlc 1.31.1 from `db/queries/core/`.

## Git state

- Branch: `task/AR-101-core-database-model`
- Commit SHA: `4b3b2b6c1284c2f61878b31edd4b940b11915194` (final reviewed AR-101 branch commit before merge)
- Pull request: [#10](https://github.com/oplosy/atrisk/pull/10), merged with commit `0f87efe1bfcc2f29c7bf490000e808cbcc93e730`.
- Remote task branch: pushed successfully; PR #10 and merge commit preserve the branch history.
- Primary `main`: fast-forwarded to `0f87efe1bfcc2f29c7bf490000e808cbcc93e730` after merge.
- Worktree: clean at the final reviewed task commit before merge.
- Live `ls-remote` was not independently verified because the proxy/remote endpoint remains unavailable.
- Worktree: clean after restoring the four verification-generated contract outputs; report snapshot is recorded separately above.

## Assumptions and risks

- The integration target and `database.Migrate` entrypoint accept only `ATLASRISK_TEST_DATABASE_URL` with exact database name `atrisk_test`, loopback host `127.0.0.1`, explicit port, PostgreSQL scheme, and explicit user; credentials are never printed.
- The required Taskfile targets set `ATLASRISK_REQUIRE_TEST_DATABASE=1`, so missing or unsafe DSNs fail rather than silently skipping; CI supplies the exact validated loopback URL through the ephemeral service.
- `00002_core_database_hardening.sql` is forward-only and preserves the already-created `00001` migration for previous-version upgrades.
- The repository-wide generated-file gate has a pre-existing Windows line-ending/index-stat incompatibility in the AR-005 contract generator; CI/Linux should be used as the authoritative cross-platform gate. No AR-005 generated output is included in this task.
- The aggregate verify gate was run with repository npm dependencies present; its only failure is the pre-existing four-file AR-005 contract-generator drift described above.
