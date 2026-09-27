# AR-304 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-304-factor-attribution.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-013, ADR-014
- Owned paths: `risk-engine/src/atlasrisk/attribution/`, `risk-engine/tests/attribution/`
- Shared paths changed and justification: `none`

## Result

`complete`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `attribute_scenario` returns exact Decimal Shapley contributions plus `interaction_residual`, `reconciles`, and tolerance; golden test proves the -1280 TRY stress loss reconciles. |
| AC-2 | Position-level factor contributions and residuals are emitted; unsupported positions remain in `unmapped_positions` and cannot look healthy. |
| AC-3 | `test_core.py` covers symmetry, dummy-factor, efficiency, and permutation invariance. |
| AC-4 | Factor count is checked before subset enumeration; max V1 bound is 8 and output records subset/permutation counts. |
| AC-5 | Output records `method`, `method_version`, `tolerance`, and sorted unmapped position records. |

## Stop-condition check

- Decision or scope conflict: `none`.
- Missing dependency, unsafe migration, or unavailable verification: `task` executable is not installed locally; its contract-test commands were run directly. Docker was not started or modified.

## Verification

| Command | Result |
|---|---|
| `uv run --project risk-engine --locked pytest risk-engine/tests/attribution -q` | pass: 7 tests |
| `uv run --project risk-engine --locked pytest risk-engine/tests/attribution/test_golden.py -q` | pass: 1 test |
| `uv run --project risk-engine --locked pytest risk-engine/tests -q` | pass: 44 tests |
| `uv run --project risk-engine --locked ruff check risk-engine/src/atlasrisk/attribution risk-engine/tests/attribution` | pass |
| `uv run --project risk-engine --locked ruff format --check risk-engine/src/atlasrisk/attribution risk-engine/tests/attribution` | pass |
| `node --test test/contract/contract.test.mjs contracts/jobs/contract.test.mjs` | pass: 14 tests |
| `task test-contract` | unavailable: `task` is not installed; direct equivalent passed |

## Change inventory

- Files changed: `risk-engine/src/atlasrisk/attribution/{__init__.py,core.py}`, `risk-engine/tests/attribution/{__init__.py,test_core.py,test_golden.py}`.
- Schema/API changes: none.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-304-factor-attribution`
- Commit SHA: `3e057eb` (implementation commit)
- Remote branch: pending push
- Worktree: clean after commit

## Assumptions and risks

- V1 attribution includes declared asset-return, yield-shift, FX, volatility, and correlation shocks; metric-only factors are represented as deterministic dummy contributions when they do not change P&L.
- The bounded V1 maximum is eight factors; larger sets are rejected before any scenario evaluation.
