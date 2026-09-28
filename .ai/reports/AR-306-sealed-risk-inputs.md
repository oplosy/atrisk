# AR-306 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-306-sealed-risk-inputs.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-009 through ADR-015, accepted in `docs/decisions/README.md`.
- Owned paths: scenario/risk application services, risk-engine scenario/jobs/attribution/metrics tests and implementations.
- Shared paths changed and justification: migration `00012` (the prior `00011` number was occupied by AR-402), OpenAPI-generated contracts and job schemas, integration fixtures/tests, and the migration version assertion required by the new schema.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `SubmitRequest` no longer contains client positions or pre-metrics; the risk handler rejects unknown body fields. `apps/api/handlers/risk/handler_test.go` covers the strict request boundary. |
| AC-2 | `loadSealedValuation` checks account/snapshot ownership, valid valuation state/hash, every account snapshot line, and valid non-null valuation amounts. Migration `00012_sealed_risk_inputs.sql` adds the database valuation-binding trigger. |
| AC-3 | Job payload contains server-selected `sealed_input`, valuation/snapshot identifiers, selected price/FX evidence paths including quote pairs, temporal cutoffs, full historical price and explicit fiat FX revision provenance (ID, observation/source/system clocks, price/rate, direction, pair, and knowledge basis), scenario version, and canonical `input_hash`. Daily metric aggregation is deterministic while all selected revisions remain auditable. TRY/USD direct or inverse history is converted explicitly; missing/stale/non-fiat FX remains blocked. |
| AC-4 | Request fingerprint includes valuation binding and scenario inputs; the database job input hash includes the canonical metric bundle and the same bundle is nested in persisted `scenario_runs.input_provenance`. `internal/application/scenarios/service_test.go` proves metric changes alter the hash. |
| AC-5 | `handle_scenario_revaluation` invokes AR-304 attribution, emits factor/position rows and residuals, and downgrades a nominally valid result when attribution does not reconcile. PostgreSQL persistence is implemented in `risk-engine/src/atlasrisk/jobs/postgres.py`. |
| AC-6 | Versioned schema/engine and data-quality fields remain in the job/result envelope. The Go service seals PIT USD-converted price history, common NAV history, and signed exposures when available; the worker invokes AR-302 `calculate_metrics` from that bundle. Missing history, unsupported/non-fiat FX, or incomplete common observations retain an explicit `PRE_SHOCK_METRICS_INPUT_HISTORY_UNAVAILABLE` blocked marker. `risk-engine/tests/jobs/test_scenario_handler.py` proves a complete sealed bundle reaches a healthy worker result; `test/integration/core_database_test.go` inspects the service payload. |
| AC-7 | Existing scenario rows retain nullable valuation binding and remain readable; new sealed fields and attribution tables use immutable triggers. Migration numbering is now `00012` after AR-402's `00011`. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: isolated PostgreSQL URL was not available; Docker was not started or reconfigured. The repository's `task` executable was also unavailable in this environment. Historical non-USD metric conversion is limited to explicit eligible fiat/USD revisions; no USDT peg or implicit FX is inferred.

## Verification

| Command | Result |
|---|---|
| `uv run --project risk-engine --locked pytest risk-engine/tests/jobs risk-engine/tests/scenarios risk-engine/tests/attribution risk-engine/tests/metrics -q` | pass, 39 tests |
| `python -m ruff check risk-engine/src risk-engine/tests` / `python -m ruff format --check risk-engine/src risk-engine/tests` | pass with existing project venv (26 files formatted) |
| `go test ./apps/api/handlers/risk ./internal/application/risk ./internal/application/scenarios -count=1` | pass |
| `go test ./contracts/jobs -count=1` | pass |
| `node --test test/contract/contract.test.mjs contracts/jobs/contract.test.mjs` | pass, 15 tests |
| `node scripts/generate/contract-models.mjs` | pass |
| `go test -run '^$' ./apps/... ./internal/...` | pass |
| `go vet ./apps/... ./internal/...` | pass |
| `go test ./test/integration -run 'TestRiskEndToEnd\|TestScenarioInputProvenance' -count=1` | compile/pass; `TestRiskEndToEnd` skipped without an isolated DB URL |
| `go test ./test/integration -run '^TestCoreDatabaseMigrations$' -count=1` with `ATLASRISK_REQUIRE_TEST_DATABASE=1` | fail closed: `test database URL is required` |
| `task migrate-test` / `task verify` | unavailable: `task` executable not installed; no Docker fallback was used |
| `git diff --check` | pass |
| `go test ./internal/application/scenarios ./test/integration -run 'TestScenariosRequestValidation\|TestScenarioInputProvenance\|TestRiskEndToEnd' -count=1` | pass; integration cases skip without isolated DSN |
| `python -m pytest risk-engine/tests/jobs risk-engine/tests/scenarios risk-engine/tests/attribution risk-engine/tests/metrics -q` | pass, 39 tests with project venv |
| `go test ./internal/application/scenarios -count=1` | pass; revision/day aggregation with UUID-ascending ties, TRY/USD FX provenance/conversion, metric-hash binding, and missing-FX fail-closed regressions |

## Change inventory

- Files changed: AR-306 application/risk binding, migration `00012_sealed_risk_inputs.sql`, generated API contracts, scenario job schema/fixtures, risk-engine sealed provenance validation and attribution quality gate, server-side AR-302 metric-input assembly/derivation, complete historical revision provenance and metric-bundle hashing, FX path validation, and integration fixtures for persisted valuation binding.
- Schema/API changes: required `valuation_id` on risk submission/result; client-authored `positions` and `pre_metrics` removed; sealed valuation/provenance and attribution persistence added.
- Generated artifacts: `contracts/generated/go/contracts.go`, `contracts/generated/typescript/contracts.ts`, `contracts/generated/python/contracts.py` and its package export regenerated by `scripts/generate/contract-models.mjs`.

## Git state

- Branch: `task/AR-306-sealed-risk-inputs`
- Implementation commit SHA: `7c9d6ac` (`fix(risk): seal historical FX inputs [AR-306]`).
- Remote branch: not pushed.
- Worktree: clean after the implementation and report commits; no generated drift.

## Assumptions and risks

- Hosted/isolated PostgreSQL migration, end-to-end worker persistence, and `task verify` remain to be run in CI because no isolated DSN and no task executable were available locally.
- AR-302 metric derivation uses complete USD-converted price histories with a common NAV calendar. Explicit TRY/USD FX history is supported; missing, stale, or non-fiat FX is explicit blocked output, never a fabricated healthy result.
- Historical metric revisions are retained in the sealed bundle even though AR-302 consumes deterministic UTC-day aggregates; the selected revision clocks and knowledge basis are not discarded.
- Docker was not run, restarted, or reconfigured.
