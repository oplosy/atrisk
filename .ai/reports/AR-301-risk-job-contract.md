# AR-301 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-301-risk-job-contract.md`
- Packet status at start: `active` (activation commit supplied by orchestrator)
- Referenced ADRs: ADR-008, ADR-009, ADR-010, ADR-013 (accepted summaries in `docs/decisions/README.md`)
- Owned paths: `internal/jobs/`, `risk-engine/src/atlasrisk/jobs/`, `contracts/jobs/`, `test/fixtures/risk/`
- Shared paths changed and justification: `db/migrations/00008_risk_jobs.sql` adds the PostgreSQL durable queue; `test/integration/risk_job_test.go` provides lifecycle evidence; `test/integration/core_database_test.go` advances migration expectations to 8; `Taskfile.yml` routes `TEST=...` to the selected lifecycle test.

## Result

`merged`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `internal/jobs/queue.go` claims with `FOR UPDATE SKIP LOCKED`, lease owner, and expiry; `test/integration/risk_job_test.go` exercises two concurrent workers (compile-only locally because no isolated DB). |
| AC-2 | `Complete`/`Fail` require an unexpired lease and run job transition plus attempt evidence in one transaction; `RecoverExpired` requeues or permanently fails expired attempts and records `LEASE_EXPIRED`; cancellation closes active attempt evidence. |
| AC-3 | Python `validate_job` returns permanent `ATLAS_UNKNOWN_SCHEMA_VERSION` and `ATLAS_UNKNOWN_JOB_KIND` errors; `risk-engine/tests/test_jobs.py` covers both. |
| AC-4 | Go `Failure` distinguishes retryable from permanent states and stores error evidence; attempt identity is immutable while a single unfinished evidence row may be closed exactly once. |
| AC-5 | Go/Python tests agree on sorted compact UTF-8 JSON, normalize integral floats (`1.0 -> 1`) and negative zero, and hash adversarial `<é>` payloads (`c1e614c0...5dc098c`), plus job (`200a097d...bec1bb`) and result (`bb078d...aee3`) fixtures. |
| AC-6 | Python `PostgresQueueClient` provides a concrete DB-API bridge for claim, execute, complete, and fail; `run_claimed_once` tests prove both completion and permanent-failure forwarding. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: local `task` executable and isolated PostgreSQL DSN are unavailable; Docker was not started or changed. Live PostgreSQL lifecycle evidence remains for CI/orchestrator.

## Verification

| Command | Result |
|---|---|
| `go test ./internal/jobs` | pass |
| `go test ./...` | pass |
| `uv run ruff format --check src tests` (from `risk-engine`) | pass; 8 files already formatted |
| `uv run ruff check src tests` (from `risk-engine`) | pass |
| `uv run pytest -q` (from `risk-engine`) | pass; 8 tests |
| `node --test test/contract/contract.test.mjs` | pass; 11 tests |
| `node --test contracts/jobs/contract.test.mjs` | pass; 2 tests |
| `go vet ./apps/... ./internal/...` | pass |
| `task test-go TEST=Jobs` | unavailable: `task` executable not installed |
| `task test-python TEST=jobs` | unavailable: `task` executable not installed |
| `task test-integration TEST=RiskJobLifecycle` | unavailable: `task` executable not installed; direct test fails closed because `ATLASRISK_TEST_DATABASE_URL` is missing |
| `go test ./test/integration -run 'TestCoreDatabase' -count=1` | pass locally with database fixtures skipped because no test DSN; `verify` now supplies `TEST: CoreDatabase` explicitly |
| `task test-contract` | unavailable: `task` executable not installed; both direct contract suites passed |
| Hosted CI #37 | PASS: [run 36340518342](https://github.com/oplosy/atrisk/actions/runs/36340518342) at `9272fa207b9a78321fd0882b5eb4ddf58118066`; ephemeral service setup, database migrations, the `CoreDatabase` and `RiskJobLifecycle` integration selectors through the verification gate all succeeded; sequential selectors eliminated the fixture collision in the earlier failed run |
| `git diff --check` | pass |

## Change inventory

- Files changed: durable queue migration; Go queue, unit, and PostgreSQL lifecycle tests; Python canonical/contract/worker/PostgreSQL bridge and tests; risk job/result schemas and schema tests; golden fixtures; migration expectations/task routing; this report.
- Schema/API changes: `risk_jobs` and immutable `risk_job_attempts`; no HTTP API change.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-301-risk-job-contract`
- Implementation commit: `9272fa207b9a78321fd0882b5eb4ddf58118066` (`fix(jobs): verify lifecycle integration sequentially [AR-301]`)
- Implementation PR: [#37](https://github.com/oplosy/atrisk/pull/37), merged to `main` as `d92fd8becbc8f58a86a0afc313ef00f5e6c7878b`; task branch was pushed and merged
- Metadata-only status finalization is recorded in its own follow-up PR.
- Implementation worktree: clean at merge; Docker Desktop and local Docker settings were not changed.

## Assumptions and risks

- PostgreSQL integration must be run by the orchestrator/CI against an isolated database. The local direct lifecycle invocation correctly failed closed because `ATLASRISK_TEST_DATABASE_URL` was absent. The canonicalization rule is UTF-8 JSON with sorted keys, compact separators, integral-float normalization, and no non-finite values.
