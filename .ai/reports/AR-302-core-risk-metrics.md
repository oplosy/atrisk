# AR-302 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-302-core-risk-metrics.md`
- Packet status at start: `ready` (readiness PR #39 merged)
- Referenced ADRs: ADR-010, ADR-011, ADR-013 (accepted summaries in `docs/decisions/README.md`)
- Owned paths: `risk-engine/src/atlasrisk/returns/`, `risk-engine/src/atlasrisk/metrics/`, `risk-engine/tests/metrics/`
- Shared paths changed and justification: `test/fixtures/risk/risk-metrics-golden.json` provides the task's language-neutral golden input and numerical expectations.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `risk-engine/tests/metrics/test_core.py` covers constant, sparse, all-missing, zero/negative NAV, and misaligned series calendars. |
| AC-2 | Returns are emitted only between adjacent expected calendar observations; business-day calendar excludes weekend crypto observations and sparse gaps cannot become one-day returns. |
| AC-3 | Every instrument pair uses identical shared price intervals and includes `overlap_count`, `required_overlap`, canonical `valid`/`degraded`/`blocked` state, and a separate reason code. |
| AC-4 | Result includes the selected calendar and annualization factor; tests cover `sqrt(252)` and `sqrt(365)`. |
| AC-5 | Golden fixture declares `absolute_tolerance: 1e-12`; test compares deterministic reruns and expected drawdown, volatility, leverage, concentration, and coverage. |

## Stop-condition check

- Decision or scope conflict: the packet originally named `task test-python` and `task test-golden` targets that do not exist in `Taskfile.yml`. The orchestrator corrected the packet to equivalent direct `uv pytest` commands; `Taskfile.yml` remains untouched and out of scope.
- Missing dependency, unsafe migration, or unavailable verification: local `task` executable is absent. The direct packet-equivalent Python and Node contract commands passed; hosted CI is the complete repository `verify` gate.

## Verification

| Command | Result |
|---|---|
| `uv run --project risk-engine --locked pytest risk-engine/tests/metrics -q` | pass; 12 tests after review corrections |
| `uv run --project risk-engine --locked pytest risk-engine/tests/metrics/test_golden.py -q` | pass; 1 test |
| `uv run --locked ruff format --check src tests` (from `risk-engine`) | pass; 14 files already formatted |
| `uv run --locked ruff check src tests` (from `risk-engine`) | pass |
| `uv run --locked pytest -q` (from `risk-engine`) | pass; 20 tests after review corrections |
| `node --test test/contract/contract.test.mjs contracts/jobs/contract.test.mjs` | pass; 13 tests |
| `task test-contract` | unavailable; `task` executable is not installed locally |
| `git diff --check` | pass |
| Hosted CI | pending on implementation PR |

The independent reviewer identified misaligned correlation intervals, sparse-return annualization, and noncanonical correlation states. All three were corrected before merge; paired returns now share both endpoints and missing expected observations degrade or block coverage.

## Change inventory

- Files changed: pure log-return/calendar functions, volatility/correlation/drawdown/leverage/concentration metrics, deterministic tests, golden fixture, task verification command correction, this report.
- Schema/API changes: none.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-302-core-risk-metrics`
- Base SHA: `2d941bf92912f3e3e684356ed6a592a14ca5890e`
- Latest implementation commit SHA: `b8eb9e57b02ae9b15661df5419457c78260aee09` (`fix(risk): align risk metric observation windows [AR-302]`)
- Remote branch: `task/AR-302-core-risk-metrics`, PR #40 open; corrected commit is queued for push and hosted CI rerun.
- Worktree: clean after this report update is committed.

## Assumptions and risks

- `price_history` values are finite positive market prices when valid; weekends are omitted only for the cross-asset business-weekday calendar. Missing/invalid observations are not repaired or forward-filled.
- This task implements pure analytics only; callers remain responsible for supplying immutable, base-currency NAV and signed exposures from the selected snapshots.
