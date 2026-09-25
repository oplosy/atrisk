# AR-201 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-201-portfolio-snapshots.md`
- Packet status at start: `active` (orchestrator transition from `ready`)
- Referenced ADRs: ADR-007, ADR-010, ADR-016, ADR-024
- Owned paths: `internal/domain/portfolio/`, `internal/application/portfolio/`, `db/queries/portfolio/`, `apps/api/handlers/portfolio/`
- Shared paths changed and justification: `db/migrations/00004_portfolio_snapshots.sql` adds the normalized external-identifier, portfolio/account, immutable snapshot/line schema and constraints; `db/queries/core/sqlc.yaml` and generated `internal/platform/database/` expose the SQLC bindings; `apps/api/cmd/api/main.go` mounts the resource routes; `contracts/openapi/openapi.json` documents the resource and correction contract; `test/integration/` verifies database-backed behavior; `.github/workflows/ci.yml` runs `PortfolioAPI` in hosted CI.

## Result

`independent-review-approved; hosted verification pending`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `test/integration/portfolio_api_test.go` exercises direct SQL UPDATE/DELETE rejection for snapshots/lines and asserts PUT/PATCH/DELETE snapshot routes return 405. Database execution requires the isolated hosted PostgreSQL gate. |
| AC-2 | `TestPortfolioAPI` creates a same-portfolio correction through `CreateSnapshot`, checks `supersedes_snapshot_id`, then reads the original and verifies its exact quantity remains unchanged. |
| AC-3 | `NUMERIC(38,18)` columns in migration plus `TestPortfolioAPI` exact 18-fraction quantity/cost/duration/convexity assertions. `total_cost_basis` remains nullable per `docs/architecture/DATA_AND_RISK_MODEL.md`; omitted cost is returned as omitted/null rather than fabricated zero. |
| AC-4 | `instrument_external_identifiers` has PostgreSQL `UNIQUE (namespace, external_id)` and `(instrument_id, namespace)` constraints; integration covers same/different namespaces and concurrent identical inserts. New service writes keep legacy JSON and normalized rows consistent in one transaction. The migration backfills legacy identifiers without treating Binance provider metadata as an identifier, and its PostgreSQL trigger is idempotent only for the same instrument/namespace while preserving cross-instrument `(namespace, external_id)` conflicts. The previous-schema upgrade regression updates and replays a Binance upsert while preserving one normalized identifier; the portfolio integration regression proves a duplicate Binance identifier on another instrument fails.
| AC-5 | Domain/service validation covers all five supported types, lifecycle status and uppercase unit codes; lowercase native units are normalized to uppercase per ADR-024. Integration covers malformed type/unit, unsupported risk attributes, wrong account portfolio, wrong instrument, and transactional instrument creation. Decimal parsing rejects values outside NUMERIC(38,18) precision before database access; PostgreSQL trigger rechecks fixed-bond duration/convexity rules. |
| AC-6 | Portfolio/account create/list/read/update/delete methods and routes are implemented; reporting currency is restricted to TRY/USD; integration proves referenced account/portfolio deletion is rejected. |
| AC-7 | `contracts/openapi/openapi.json` describes instrument, portfolio, account, snapshot, line and correction routes with decimal-string fields. The contract generator now emits all portfolio/account/snapshot API schemas in Go, TypeScript, and Python, with a drift test covering the generated targets. SQLC output was regenerated from the migration/query source. |

## Stop-condition check

- Decision or scope conflict: `none`. Optional cost basis follows the accepted data model; no valuation, transaction ledger, or risk computation was added.
- Missing dependency, unsafe migration, or unavailable verification: isolated PostgreSQL was not available locally, so migration/integration execution remains a hosted-CI requirement. No Docker/Desktop or local services were changed.

## Verification

| Command | Result |
|---|---|
| `sqlc generate -f db/queries/core/sqlc.yaml` | pass; SQLC v1.31.1 generated portfolio bindings and shared interface/model output |
| `go test ./... -count=1` | pass; all local Go unit/packages; integration test compiles and skips only without isolated DSN |
| `GOCACHE=<workspace>/.cache/ar201-go-build go test ./... -count=1` (final orchestrator rerun) | pass; used workspace-local cache after the default Windows Go cache returned Access denied; PostgreSQL-backed tests still skip without isolated DSN |
| `go test ./test/integration -run '^$' -count=1` | pass; integration package compile gate |
| `go test ./test/integration -run '^TestCoreDatabasePreviousVersionUpgrade$' -count=1 -v` | pass with an explicit skip because no test database URL is configured; hosted CI executes the upgrade/update/upsert regression |
| `go vet ./apps/... ./internal/...` | pass |
| `go build ./apps/...` | pass |
| `node --test test/contract/contract.test.mjs` | pass; 11 tests, including generated Portfolio/Account/Snapshot model coverage |
| Final orchestrator rerun: `node test/contract/contract.test.mjs` | 10/11 checks passed, including portfolio generated-model coverage; Python compile/import subprocess was blocked by environment `spawn EPERM` |
| `node scripts/verify/check-generated.mjs` | pass after committing generated outputs; pre-commit invocation correctly detected the intentional new generated artifacts |
| Final orchestrator rerun: `node scripts/verify/check-generated.mjs` | blocked by environment `spawn EPERM` while starting its child process; prior post-commit check passed |
| `git diff --check` | pass |
| `task migrate-test` | not run locally; `task` executable unavailable and no isolated PostgreSQL DSN; hosted CI runs the required migration gate |
| `task test-go TEST=Portfolio` | not runnable locally because `task` executable is unavailable; equivalent `go test ./... -count=1` passed |
| `task test-go-integration TEST=PortfolioAPI` | not runnable locally because `task` executable is unavailable; `go test ./test/integration -run '^TestPortfolioAPI$' -count=1 -v` passed with an explicit skip because no test database URL is configured |
| `task test-contract` | not runnable locally because `task` executable is unavailable; equivalent contract test and generator checks passed |
| `task verify` | not runnable locally because `task` executable is unavailable; hosted CI runs `task verify` |

## Change inventory

- Files changed: AR-201 domain model/service/handler/tests; `db/migrations/00004_portfolio_snapshots.sql`; `db/queries/portfolio/portfolio.sql`; SQLC config/generated database files; API mount; OpenAPI; integration test; CI integration step; contract generator/generated outputs; this report; core migration-version assertions.
- Schema/API changes: normalized immutable instrument identifiers; portfolios/accounts; immutable snapshots and lines; same-portfolio correction FK; exact decimal storage; structured resource APIs and read-only snapshot contract.
- Generated artifacts: SQLC `internal/platform/database/{models.go,querier.go,portfolio.sql.go}` and regenerated contract outputs, including Portfolio/Account/Snapshot models in Go/TypeScript/Python; no generated drift remains after commit.

## Git state

- Branch: `task/AR-201-portfolio-snapshots`
- Latest implementation SHA: `449697d` (Binance trigger conflict-target fix)
- Review checkpoint before this metadata transition: local HEAD `972c85b`, remote `origin/task/AR-201-portfolio-snapshots` at `20ec449`, 10 local commits ahead; push/PR deferred to orchestrator
- Worktree: clean at implementation review; metadata-only lifecycle/report update is being committed separately

## Assumptions and risks

- Hosted PostgreSQL 18 is the authoritative migration/integration environment; local Docker was intentionally not started or changed.
- The existing `instruments.native_currency` column is the persisted native-unit field established by ADR-024/AR-105; the public API names it `native_unit` without adding a divergent duplicate column.
- Legacy `instruments.external_ids` JSON objects are backfilled into the normalized uniqueness table. Provider/status metadata is excluded, ambiguous legacy pairs are preserved only in JSON, and new Binance-writer inserts fail on normalized uniqueness conflicts instead of silently dropping the row.
- Local Docker/Desktop and services were not started, stopped, reset, or reconfigured; hosted PostgreSQL remains the unresolved runtime gate.
