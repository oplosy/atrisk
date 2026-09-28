---
id: AR-306
title: Seal risk run inputs and persist stress attribution
status: merged
phase: 3
depends_on: [AR-203, AR-302, AR-303, AR-304, AR-305]
branch: task/AR-306-sealed-risk-inputs
base_sha: bfd36bd5e49a433dbd3657548019b18a4e2a0b58
owned_paths: [internal/application/scenarios/, internal/application/risk/, apps/api/handlers/risk/, risk-engine/src/atlasrisk/jobs/, risk-engine/src/atlasrisk/scenarios/, risk-engine/src/atlasrisk/attribution/, risk-engine/src/atlasrisk/metrics/, risk-engine/tests/jobs/, risk-engine/tests/scenarios/, risk-engine/tests/attribution/, risk-engine/tests/metrics/]
shared_paths: [db/migrations/, db/queries/risk/, contracts/openapi/, contracts/jobs/, contracts/generated/, scripts/generate/, test/integration/, Taskfile.yml, docs/plans/MASTER_PLAN.md, .ai/tasks/AR-504-risk-stress-ui.md]
adrs: [ADR-009, ADR-010, ADR-011, ADR-012, ADR-013, ADR-014, ADR-015]
---

# AR-306: Seal risk run inputs and persist stress attribution

## Outcome

Risk submissions are bound to immutable, server-selected valuation and market
history inputs. Completed stress results include deterministic factor and
position attribution whose totals reconcile or expose an explicit residual.

## In scope

- Require a valuation-run identifier that belongs to the submitted account and
  snapshot; reject mismatches and non-usable valuation quality without creating
  a scenario version or job.
- Build worker positions and pre-shock risk metrics from persisted valuation,
  snapshot, and point-in-time market observations. Do not accept client-authored
  positions, values, metric results, or provenance as calculation inputs.
- Preserve the selected valuation run, price/FX quote paths, market observation
  revisions, cutoff/knowledge semantics, and engine/contract versions in the
  immutable job/run provenance.
- Invoke the existing AR-304 attribution calculation for completed scenario
  outputs and persist factor contributions, position contributions, and visible
  interaction residual in the immutable result/API contract.
- Keep idempotency bound to the canonical sealed inputs and scenario version.
- Add migration/query/API/worker/contract tests, including forged-input,
  cross-account, stale/missing-data, deterministic replay, and reconciliation
  cases.

## Out of scope

- New risk metrics or shock formulas, client-side calculations, broker or order
  execution, and changes to accepted accounting or point-in-time semantics.
- Rewriting historical completed runs; existing result payloads remain readable.

## Acceptance criteria

- [ ] HTTP callers cannot influence position values, risk metrics, or quote
  provenance by supplying them in the request body.
- [ ] A run is rejected atomically when its valuation is foreign, mismatched,
  blocked, or lacks required persisted inputs; no false healthy result is emitted.
- [ ] The sealed job input can be reconstructed byte-identically from persisted
  references and records the valuation, snapshot, price/FX paths, and temporal
  cutoffs used.
- [ ] Repeated submission with the same idempotency key and canonical inputs
  returns the same run; changed inputs with that key are rejected.
- [ ] Completed results expose factor and position attribution and reconcile
  total P&L within the recorded tolerance, or explicitly expose a residual and
  degraded/blocked state.
- [ ] Worker/API results record data-quality, input, scenario, engine, and
  attribution method versions; missing or stale required data cannot appear
  healthy.
- [ ] Prior immutable run results remain unchanged and readable after migration.

## Required verification

```text
uv run --project risk-engine --locked pytest risk-engine/tests/jobs risk-engine/tests/scenarios risk-engine/tests/attribution risk-engine/tests/metrics -q
go test ./apps/api/handlers/risk ./internal/application/risk ./internal/application/scenarios -count=1
node --test test/contract/contract.test.mjs contracts/jobs/contract.test.mjs
node scripts/generate/contract-models.mjs
node scripts/verify/check-generated.mjs
go test ./test/integration -run 'TestRiskEndToEnd|TestScenarioInputProvenance' -count=1
task migrate-test
task verify
```

Integration and migration commands must use an isolated PostgreSQL database.
