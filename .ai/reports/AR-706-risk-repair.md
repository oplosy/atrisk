# AR-706 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-706-risk-repair.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-008, ADR-009, ADR-010, ADR-011, ADR-012, ADR-013, ADR-015
- Owned paths: `internal/application/scenarios/`, `internal/jobs/`, `risk-engine/src/atlasrisk/jobs/`, `risk-engine/src/atlasrisk/metrics/`, `risk-engine/src/atlasrisk/scenarios/`, `risk-engine/tests/`, `.ai/reports/AR-706-risk-repair.md`
- Shared paths changed and justification: `contracts/openapi/openapi.json` and `contracts/jobs/scenario-revalue.schema.json` were explicitly serialized in the packet for the public shock contract; generated contract targets were regenerated and unchanged.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| Lease recovery and stale ownership | Go and Python recovery update attempt evidence and linked scenario state; Go completion/failure/renewal predicates include attempt ownership; worker renewal suppresses stale success and failure writes. |
| Cash metrics | Sealed metric inputs support constant USD cash and dated foreign-currency FX history with separate `cash_revision_history`; missing FX and workload overflow fail closed; constant cash has valid zero volatility and explicit undefined pair correlations. |
| Exact shocks | Go enqueue/version validation and Python execution validation require bounded decimal strings, fixed fields/shapes, semantic bounds, and storage-safe outputs; scenario engine version is `1.1.0`. |
| Pairwise metrics | Identical valid-date grids reuse returns; differing grids retain pairwise alignment; no-overlap and deterministic instrument/pair capacity states remain explicit. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: hosted `task verify` remains parent-owned. The latest local `TestRiskEndToEnd` rerun was blocked by two pre-existing fixed idempotency keys in the isolated database (`risk-api-e2e-key`, `risk-api-e2e-completed-key`); reset the test database before the final selector run.

## Verification

| Command | Result |
|---|---|
| `go test ./internal/application/scenarios ./internal/jobs -count=1` | pass |
| `uv run --project risk-engine --locked pytest risk-engine/tests -q` | pass, 69 tests |
| `uv run --project risk-engine --locked ruff check risk-engine/src risk-engine/tests` | pass |
| `node --test test/contract/contract.test.mjs contracts/jobs/contract.test.mjs` | pass, 15 tests |
| `go test ./test/integration -run 'TestRiskRepair|TestScenarioInputProvenance|TestRiskJobLifecycle|TestRiskEndToEnd' -count=1` | needs rerun after isolated DB idempotency-key cleanup; other selected tests passed before the residue was encountered |
| `task verify` | parent-owned hosted CI pending |

## Change inventory

- Files changed: queue lifecycle and worker renewal/recovery, scenario service cash history and shock validation, Python scenario execution and metrics, focused tests, public schemas.
- Schema/API changes: fixed shock field/object shapes and bounded decimal-string leaves; cash revision provenance fields in scenario metric inputs.
- Generated artifacts: `node scripts/generate/contract-models.mjs` passed; generated files had no diff.

## Git state

- Branch: `task/AR-706-risk-repair`
- Commit SHA: `b8ee60a` (implementation commit)
- Remote branch: pending parent task-branch synchronization
- Worktree: clean after commit

## Assumptions and risks

- Final integration evidence depends on parent resetting the dedicated test database's fixed idempotency keys; no product or schema migration is required.
