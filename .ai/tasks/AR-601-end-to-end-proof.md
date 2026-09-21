---
id: AR-601
title: Prove the complete AtlasRisk journeys
status: draft
phase: 6
depends_on: [AR-502, AR-503, AR-504, AR-505]
branch: task/AR-601-end-to-end-proof
owned_paths: [test/e2e/, test/fixtures/system/, docs/evidence/]
shared_paths: [Taskfile.yml, .github/workflows/, apps/, risk-engine/, internal/]
adrs: [ADR-006, ADR-007, ADR-011, ADR-012, ADR-013, ADR-014]
---

# AR-601: Prove the complete AtlasRisk journeys

## Outcome

Deterministic end-to-end tests prove the four stated product guarantees and the
complete ingest-to-decision journey against real local dependencies.

## In scope

- Seed/replay fixtures, API and browser journeys, failure/degraded cases, evidence
  capture, calculation/hash reconciliation, and CI integration.
- Explicit proofs for revision preservation, valuation/FX reconciliation, no
  false-green missing data, and explainable stress loss.

## Out of scope

- Live third-party API reliability, visual polish redesign, or production deployment.

## Acceptance criteria

- [ ] A source revision leaves the prior source/system-as-of answer queryable.
- [ ] TRY/USD valuation reconciles and exposes exact price/FX lineage.
- [ ] Missing/delayed required data produces blocked/degraded UI and API state.
- [ ] Stress total equals positions and factors plus visible residual.
- [ ] Create portfolio -> ingest -> value -> stress -> decide -> review passes.
- [ ] Tests are deterministic, isolated, retry-free, and upload useful failure artifacts.

## Required verification

```text
task test-e2e
task verify
git status --short
```
