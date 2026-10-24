# AR-706 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-706-risk-repair.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-008, ADR-009, ADR-010, ADR-011, ADR-012, ADR-013, ADR-015
- Owned paths: `internal/application/scenarios/`, `internal/jobs/`, `risk-engine/src/atlasrisk/jobs/`, `risk-engine/src/atlasrisk/metrics/`, `risk-engine/src/atlasrisk/scenarios/`, `risk-engine/tests/`, `.ai/reports/AR-706-risk-repair.md`
- Shared paths changed and justification: `test/integration/risk_repair_test.go` was explicitly serialized for the required lifecycle, atomic shock, cash metric regressions, and fixture cleanup. Contract sources were explicitly serialized for the public shock contract; generated contract targets were regenerated and unchanged.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| Lease recovery and stale ownership | Go and Python recovery update attempt evidence and linked scenario state; Go and Python completion/failure/renewal predicates include attempt identity; worker renewal suppresses stale success and failure writes and waits for the renewal thread to finish. |
| Cash metrics | Sealed metric inputs support constant USD cash and dated foreign-currency FX history with separate `cash_revision_history`; mixed USD/EUR integration coverage confirms missing FX omits NAV and blocks default quality; constant cash has valid zero volatility and explicit undefined pair correlations. |
| Exact shocks | Go enqueue/version validation and Python execution validation require bounded decimal strings, fixed fields/shapes, semantic bounds, and storage-safe outputs; syntax, empty-key, numeric, oversized, and atomic-no-queue cases are covered; scenario engine version is `1.1.0`. |
| Pairwise metrics | Capacity is checked before any return computation; identical valid-date grids reuse returns; differing grids retain pairwise alignment; no-overlap and deterministic instrument/pair capacity states remain explicit. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: hosted `task verify` remains parent-owned. The fresh disposable PostgreSQL selector still reports two existing-suite issues: `TestRiskJobLifecycle` sees queued jobs left by preceding tests, and `TestRiskEndToEnd` returns `INVALID_REQUEST` from its pre-existing manual-spot fixture. The AR-706 repair selectors pass independently; parent should run the hosted gate with its isolated lifecycle and fixture setup.

## Verification

| Command | Result |
|---|---|
| `go test ./internal/application/scenarios ./internal/jobs -count=1` | pass |
| `uv run --project risk-engine --locked pytest risk-engine/tests -q` | pass, 74 tests |
| `uv run --project risk-engine --locked ruff check risk-engine/src risk-engine/tests` | pass |
| `node --test test/contract/contract.test.mjs contracts/jobs/contract.test.mjs` | pass, 15 tests |
| `go test ./test/integration -run 'TestRiskRepair|TestScenarioInputProvenance|TestRiskJobLifecycle|TestRiskEndToEnd' -count=1` | repair/provenance selectors pass; existing `TestRiskJobLifecycle` is affected by queued-job interference when run after repair tests, and existing `TestRiskEndToEnd` rejects its manual-spot fixture with `INVALID_REQUEST` |
| `go test ./test/integration -run 'TestRiskJobLifecycleRepair|TestRiskEndToEndRepair' -count=1` | pass against isolated PostgreSQL; lifecycle fence, atomic shocks, USD/EUR cash coverage |
| `task verify` | parent-owned hosted CI pending |

## Change inventory

- Files changed: queue lifecycle and worker renewal/recovery, scenario service cash history and shock validation, Python scenario execution and metrics, persisted attribution quantization, focused tests, public schemas.
- Schema/API changes: fixed shock field/object shapes and bounded decimal-string leaves; cash revision provenance fields in scenario metric inputs.
- Generated artifacts: `node scripts/generate/contract-models.mjs` passed; generated files had no diff.

## Git state

- Branch: `task/AR-706-risk-repair`
- Commit SHA: `5f8720ed177d87b7b40468068e32b3ab901e40b6` (attribution storage quantization and queue recovery capability repair) and `11a0966bd44f6bde1618e068845fdc0de8ac075b` (cash fixture isolation; this report is committed separately on the same task branch)
- Remote branch: pending parent task-branch synchronization
- Worktree: clean after commit

## Assumptions and risks

- Final integration evidence depends on parent-owned hosted `task verify` and its isolated database lifecycle. No product or schema migration is required.
