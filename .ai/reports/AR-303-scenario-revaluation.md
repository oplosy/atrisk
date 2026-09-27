# AR-303 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-303-scenario-revaluation.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-011, ADR-013, ADR-015
- Owned paths: scenario risk-engine core/jobs/tests and Go application service
- Shared paths changed and justification: job contract, migration, golden fixture, and DB integration coverage required by task acceptance.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | Go service tests cover new immutable versions, idempotent replay, concurrent version allocation; DB integration assertions cover persisted prior version and identity guard, but DB execution awaits hosted CI. |
| AC-2 | Python scenario tests and golden fixture reconcile position P&L to portfolio totals. |
| AC-3 | Scenario tests cover unmapped-factor behavior, coverage policy, and blocked/degraded states. |
| AC-4 | Versioned scenario contract and core tests cover explicit basis-point yield shifts with duration/convexity. |
| AC-5 | Golden test covers volatility/correlation shocks independently from spot P&L. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: live PostgreSQL migration/integration execution is unavailable locally because `ATLASRISK_TEST_DATABASE_URL` is unset. Docker was intentionally left untouched. Hosted CI must execute the DB checks before merge.
- Hosted CI run 36346467014 exposed a test-fixture setup issue: the foreign-snapshot case did not reuse the existing scenario ID and hit the account/name uniqueness constraint before reaching snapshot validation. The test now reuses the scenario ID; rerun CI is required.

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
| GitHub Actions run 36346467014 | fail at DB migration test due to test-fixture scenario uniqueness; corrected locally, rerun pending |

## Change inventory

- Files changed: scenario revaluation core, worker/job persistence, Go scenario application service, job schema, migration, integration/unit/golden tests, task packet, and this report.
- Schema/API changes: versioned scenario/job contract, scenario/version/run and immutable result evidence tables, guarded run identity and snapshot-line binding.
- Generated artifacts: none changed by hand; generated drift check passes.

## Git state

- Branch: `task/AR-303-scenario-revaluation`
- Commit SHA: `e8d223a` (local commit; final SHA may change if the handoff record is amended)
- Remote branch: not pushed; external push was blocked by the approval gate for this branch payload/destination
- Worktree: clean after commit

## Assumptions and risks

- Database trigger behavior is covered by integration assertions but remains unexecuted locally; merge is contingent on hosted PostgreSQL CI.
- Docker Desktop was not restarted or reconfigured.
- Independent reviewer verdict: ready for merge; no blocking findings remain.
- PR: [#43](https://github.com/oplosy/atrisk/pull/43); follow-up fix awaits hosted CI.
