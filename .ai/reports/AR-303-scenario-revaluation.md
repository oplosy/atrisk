# AR-303 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-303-scenario-revaluation.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-011, ADR-013, ADR-015
- Owned paths: scenario risk-engine core/jobs/tests and Go application service
- Shared paths changed and justification: job contract, migration, golden fixture, and DB integration coverage required by task acceptance.

## Result

`merged`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | Go service and PostgreSQL integration tests cover immutable versions, idempotent replay, concurrent version allocation, and prior-run version binding. |
| AC-2 | Python scenario tests and golden fixture reconcile position P&L to portfolio totals. |
| AC-3 | Scenario tests cover unmapped-factor behavior, coverage policy, and blocked/degraded states. |
| AC-4 | Versioned scenario contract and core tests cover explicit basis-point yield shifts with duration/convexity. |
| AC-5 | Golden test covers volatility/correlation shocks independently from spot P&L. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: local PostgreSQL execution was skipped because `ATLASRISK_TEST_DATABASE_URL` is unset; hosted PostgreSQL CI passed. Docker Desktop was not changed.
- CI runs 36346467014 and 36346675493 exposed two test-fixture issues; both were corrected. Final hosted run 36346926647 passed all checks.

## Verification

| Command | Result |
|---|---|
| `uv run --group dev pytest -q` (from `risk-engine`) | pass: 37 tests |
| `uv run --group dev ruff check src tests` | pass |
| `uv run --group dev ruff format --check src tests` | pass: 21 files |
| `go test ./apps/... ./internal/...` | pass |
| `go vet ./apps/... ./internal/...` | pass |
| `go test ./test/integration -run '^TestCoreDatabaseMigrations$' -count=1 -v` | skipped: test DB URL required |
| `node --test --test-concurrency=1 test/contract/contract.test.mjs contracts/jobs/contract.test.mjs` | pass: 14 tests |
| `node scripts/verify/check-generated.mjs` | pass: no generated-file drift |
| `git diff --check` | pass |
| GitHub Actions run 36346926647 | pass: DB migration verification and full `verify` gate |

## Change inventory

- Files changed: scenario revaluation core, worker/job persistence, Go scenario application service, job schema, migration, integration/unit/golden tests, task packet, and this report.
- Schema/API changes: versioned scenario/job contract, scenario/version/run and immutable result evidence tables, guarded run identity and snapshot-line binding.
- Generated artifacts: none changed by hand; generated drift check passes.

## Git state

- Branch: `task/AR-303-scenario-revaluation`
- Implementation commit SHA: `8ea64f26d949710627e25c4db67902e9da2e80fd`
- Merge commit SHA: `5ba3f3d3c7d7f82d959dd5498efc1ee20d0ca9fa`
- Remote branch: `task/AR-303-scenario-revaluation`, pushed and merged through [PR #43](https://github.com/oplosy/atrisk/pull/43)
- Implementation worktree: clean at merge

## Assumptions and risks

- Database trigger behavior passed hosted PostgreSQL migration/integration validation.
- Docker Desktop was not restarted or reconfigured.
- Independent reviewer verdict: ready for merge; no blocking findings remain.
