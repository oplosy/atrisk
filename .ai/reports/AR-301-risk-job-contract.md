# AR-301 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-301-risk-job-contract.md`
- Packet status at start: `active` (activation commit supplied by orchestrator)
- Referenced ADRs: ADR-008, ADR-009, ADR-010, ADR-013 (accepted summaries in `docs/decisions/README.md`)
- Owned paths: `internal/jobs/`, `risk-engine/src/atlasrisk/jobs/`, `contracts/jobs/`, `test/fixtures/risk/`
- Shared paths changed and justification: `db/migrations/00008_risk_jobs.sql` adds the PostgreSQL durable queue required by ADR-008

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `internal/jobs/queue.go` claims with `FOR UPDATE SKIP LOCKED`, lease owner, and expiry; live concurrency evidence requires PostgreSQL integration execution. |
| AC-2 | `Complete` is owner/lease guarded and idempotent; `RecoverExpired` requeues or permanently fails expired attempts and records `LEASE_EXPIRED`. |
| AC-3 | Python `validate_job` returns permanent `ATLAS_UNKNOWN_SCHEMA_VERSION` and `ATLAS_UNKNOWN_JOB_KIND` errors; `risk-engine/tests/test_jobs.py` covers both. |
| AC-4 | Go `Failure` distinguishes retryable from permanent states and stores error evidence; migration stores immutable attempt records. |
| AC-5 | Go/Python tests hash the same sorted compact golden JSON (`200a097d...bec1bb` job hash); result fixture hash is `bb078d...aee3`. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: local `task` executable and isolated PostgreSQL DSN are unavailable; Docker was not started or changed.

## Verification

| Command | Result |
|---|---|
| `go test ./internal/jobs` | pass |
| `go test ./...` | pass |
| `uv run ruff format --check src tests` (from `risk-engine`) | pass; 7 files already formatted |
| `uv run ruff check src tests` (from `risk-engine`) | pass |
| `uv run pytest -q` (from `risk-engine`) | pass; 5 tests |
| `node --test test/contract/contract.test.mjs` | pass; 11 tests |
| `task test-go TEST=Jobs` | unavailable: `task` executable not installed |
| `task test-python TEST=jobs` | unavailable: `task` executable not installed |
| `task test-integration TEST=RiskJobLifecycle` | unavailable: `task` executable not installed; no `ATLASRISK_TEST_DATABASE_URL` |
| `task test-contract` | unavailable: `task` executable not installed; direct contract test passed |
| `git diff --check` | pass |

## Change inventory

- Files changed: durable queue migration; Go queue and unit tests; Python canonical/contract/worker modules and tests; risk job/result schemas; golden fixtures; this report.
- Schema/API changes: `risk_jobs` and immutable `risk_job_attempts`; no HTTP API change.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-301-risk-job-contract`
- Commit SHA: final commit is the output of `git rev-parse HEAD` after this report amendment; subject `feat(jobs): add durable risk job contract and worker [AR-301]`
- Remote branch: push attempted, rejected by egress policy; no remote update
- Worktree: clean after final commit

## Assumptions and risks

- PostgreSQL integration must be run by the orchestrator/CI against an isolated database. Existing `test/integration/core_database_test.go` currently asserts latest migration version 7 and may need the orchestrator's serialized migration update for migration 8.
