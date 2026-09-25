# AR-201 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-201-portfolio-snapshots.md`
- Packet status at start: `active` (orchestrator transition from `ready`)
- Referenced ADRs: ADR-007, ADR-010, ADR-016, ADR-024
- Owned paths: `internal/domain/portfolio/`, `internal/application/portfolio/`, `db/queries/portfolio/`, `apps/api/handlers/portfolio/`
- Shared paths changed and justification: `db/migrations/00004_portfolio_snapshots.sql` adds the normalized external-identifier, portfolio/account, immutable snapshot/line schema and constraints; `db/queries/core/sqlc.yaml` and generated `internal/platform/database/` expose the SQLC bindings; `apps/api/cmd/api/main.go` mounts the resource routes; `contracts/openapi/openapi.json` documents the resource and correction contract; `test/integration/` verifies database-backed behavior; `.github/workflows/ci.yml` runs `PortfolioAPI` in hosted CI.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `test/integration/portfolio_api_test.go` exercises direct SQL UPDATE/DELETE rejection for snapshots/lines and asserts PUT/PATCH/DELETE snapshot routes return 405. Database execution requires the isolated hosted PostgreSQL gate. |
| AC-2 | `TestPortfolioAPI` creates a same-portfolio correction through `CreateSnapshot`, checks `supersedes_snapshot_id`, then reads the original and verifies its exact quantity remains unchanged. |
| AC-3 | `NUMERIC(38,18)` columns in migration plus `TestPortfolioAPI` exact 18-fraction quantity/cost/duration/convexity assertions. `total_cost_basis` remains nullable per `docs/architecture/DATA_AND_RISK_MODEL.md`; omitted cost is returned as omitted/null rather than fabricated zero. |
| AC-4 | `instrument_external_identifiers` has PostgreSQL `UNIQUE (namespace, external_id)` and `(instrument_id, namespace)` constraints; integration covers same/different namespaces and concurrent identical inserts. New service writes keep legacy JSON and normalized rows consistent in one transaction; migration backfills legacy JSON. |
| AC-5 | Domain/service validation covers all five supported types, lifecycle status and uppercase unit codes; integration covers malformed type/unit, unsupported risk attributes, wrong account portfolio, wrong instrument, and transactional instrument creation. PostgreSQL trigger rechecks fixed-bond duration/convexity rules. |
| AC-6 | Portfolio/account create/list/read/update/delete methods and routes are implemented; reporting currency is restricted to TRY/USD; integration proves referenced account/portfolio deletion is rejected. |
| AC-7 | `contracts/openapi/openapi.json` describes instrument, portfolio, account, snapshot, line and correction routes with decimal-string fields; SQLC output was regenerated from the migration/query source and the contract/generated drift check was run. |

## Stop-condition check

- Decision or scope conflict: `none`. Optional cost basis follows the accepted data model; no valuation, transaction ledger, or risk computation was added.
- Missing dependency, unsafe migration, or unavailable verification: isolated PostgreSQL was not available locally, so migration/integration execution remains a hosted-CI requirement. No Docker/Desktop or local services were changed.

## Verification

| Command | Result |
|---|---|
| `sqlc generate -f db/queries/core/sqlc.yaml` | pass; SQLC v1.31.1 generated portfolio bindings and shared interface/model output |
| `go test ./... -count=1` | pass; all local Go unit/packages; integration test compiles and skips only without isolated DSN |
| `go test ./test/integration -run '^$' -count=1` | pass; integration package compile gate |
| `go vet ./apps/... ./internal/...` | pass |
| `go build ./apps/...` | pass |
| `node --test test/contract/contract.test.mjs` | pass; 10 tests |
| `node scripts/verify/check-generated.mjs` | pass after Git stat refresh; contract generator leaves no generated drift |
| `git diff --check` | pass |
| `task migrate-test` | not run locally; `task` executable unavailable and no isolated PostgreSQL DSN; hosted CI runs the required migration gate |
| `task test-go TEST=Portfolio` | equivalent local `go test ./... -count=1` passed; Task executable unavailable locally |
| `task test-go-integration TEST=PortfolioAPI` | not run locally; requires isolated PostgreSQL; hosted CI workflow step added |
| `task test-contract` | equivalent contract test and generator checks passed; Task executable unavailable locally |
| `task verify` | not run locally; Task executable unavailable locally; hosted CI runs `task verify` |

## Change inventory

- Files changed: AR-201 domain model/service/handler/tests; `db/migrations/00004_portfolio_snapshots.sql`; `db/queries/portfolio/portfolio.sql`; SQLC config/generated database files; API mount; OpenAPI; integration test; CI integration step; this report; core migration-version assertions.
- Schema/API changes: normalized immutable instrument identifiers; portfolios/accounts; immutable snapshots and lines; same-portfolio correction FK; exact decimal storage; structured resource APIs and read-only snapshot contract.
- Generated artifacts: SQLC `internal/platform/database/{models.go,querier.go,portfolio.sql.go}` and regenerated contract outputs (content-stable); no generated drift remains.

## Git state

- Branch: `task/AR-201-portfolio-snapshots`
- Commit SHA: to be filled after commit
- Remote branch: `origin/task/AR-201-portfolio-snapshots` is not updated by this worker
- Worktree: clean after commit

## Assumptions and risks

- Hosted PostgreSQL 18 is the authoritative migration/integration environment; local Docker was intentionally not started or changed.
- The existing `instruments.native_currency` column is the persisted native-unit field established by ADR-024/AR-105; the public API names it `native_unit` without adding a divergent duplicate column.
- Legacy `instruments.external_ids` JSON objects are backfilled into the normalized uniqueness table; migration fails closed on conflicting pre-existing `(namespace, external_id)` values rather than silently dropping one.
