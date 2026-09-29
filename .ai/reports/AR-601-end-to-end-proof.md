# AR-601 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-601-end-to-end-proof.md`
- Packet status at start: `ready` (merged readiness PR #60)
- Referenced ADRs: ADR-006, ADR-007, ADR-011, ADR-012, ADR-013, ADR-014 in `docs/decisions/README.md`
- Owned paths: `test/e2e/`, `test/fixtures/system/`, `docs/evidence/`
- Shared paths changed and justification: `.github/workflows/ci.yml` and `Taskfile.yml` add the task-scoped hosted E2E gate and capture its failure log.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | The journey appends a revised price through the import API and verifies old/new values by both source-as-of and system-as-of query times; hosted DB execution pending. |
| AC-2 | The journey imports 64 deterministic price observations plus USD/TRY FX evidence, then asserts exact TRY/USD totals and price/FX lineage; hosted DB execution pending. |
| AC-3 | The journey asserts a missing-price valuation is blocked; route UI tests cover blocked/degraded states, 4 files / 25 tests passed locally. |
| AC-4 | The production Python `PostgresQueueClient.run_claimed_once()` path claims/completes the job; expected `-1304 TRY`, one position/factor, persisted attribution/metrics, position sum, and factor-plus-residual reconciliation are asserted; hosted DB/worker execution pending. |
| AC-5 | API HTTP handlers create the portfolio/snapshot, ingest prices, value, stress, seal the decision, append a review, reconstruct evidence, and verify the timeline; hosted DB/S3 execution pending. |
| AC-6 | Fixed fixture/as-of values, isolated database DSN validation, deterministic worker, no retries, and CI-uploaded `.task/ci-failure/e2e.log`; hosted verification pending. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: local isolated test DB URL and Garage/S3 endpoint are absent; live E2E and `task verify` must run in hosted CI. No Docker commands or workstation Docker settings were used.

## Verification

| Command | Result |
|---|---|
| `go test ./test/e2e -run '^$'` | pass; package compiles. |
| `go test ./test/e2e -count=1 -v` | test safely skipped: `ATLASRISK_TEST_DATABASE_URL` is not set. |
| `uv run --locked pytest -q` (from `risk-engine/`) | pass; 47 tests. |
| `npm test -- --run apps/web/src/routes` | pass; 4 files / 25 tests. Initial sandbox attempt hit `spawn EPERM`; elevated rerun passed. |
| `npm run typecheck` | pass. |
| `npm run build` | pass; 48 modules. |
| `npm run lint` | pass. |
| `npm run format:check` | pass. |
| `git diff --check` | pass. |
| `task test-e2e`, `task verify` | hosted CI pending. |

## Change inventory

- Files changed: `test/e2e/journey_test.go`, `test/fixtures/system/journey.json`, `docs/evidence/AR-601-end-to-end-proof.md`, `Taskfile.yml`, `.github/workflows/ci.yml`, `risk-engine/pyproject.toml`, `risk-engine/uv.lock`, this report.
- Schema/API changes: none; the E2E invokes existing handlers and queue persistence adapter.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-601-end-to-end-proof`
- Commit SHA: pending.
- Remote branch: pending.
- Worktree: dirty; implementation and review in progress.

## Assumptions and risks

- The route tests run in Testing Library's DOM environment with controlled API responses; the database-backed journey calls handlers through `httptest` rather than launching the production API executable or a live browser. This limitation is stated in `docs/evidence/AR-601-end-to-end-proof.md`.
- Hosted CI must prove the end-to-end DB and Python worker path before merge.
