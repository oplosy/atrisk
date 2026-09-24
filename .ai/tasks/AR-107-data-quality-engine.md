---
id: AR-107
title: Implement data-quality gates
status: ready
phase: 1
depends_on: [AR-101, AR-106]
branch: task/AR-107-data-quality-engine
base_sha: 7ca863714061a96e4eff394003de0916345844e6
owned_paths: [internal/quality/, db/queries/quality/, apps/api/handlers/quality/]
shared_paths: [contracts/openapi/, db/migrations/, test/fixtures/quality/]
adrs: [ADR-011]
---

# AR-107: Implement data-quality gates

## Outcome

Series and calculation inputs receive deterministic fresh/stale/missing/partial/
suspect/revised classifications and valid/degraded/blocked aggregate states.

## In scope

- Versioned freshness/expected-frequency policies and source calendars.
- Structured reason codes, affected interval/entity references, and API endpoints.
- Aggregation rules distinguishing required and optional inputs.

## Out of scope

- UI styling or silently repairing missing observations.

## Acceptance criteria

- [ ] Fixed-clock tests cover fresh/stale/missing/partial/suspect/revised inputs and expected-period behavior across holidays, weekends, late releases, and gaps.
- [ ] Required missing, stale, or suspect input always blocks dependent calculations; blocked results cannot be represented as healthy/valid.
- [ ] Optional stale/partial input degrades and never reports valid; fresh required inputs can produce valid results.
- [ ] Every evaluation identifies its policy version and UTC evaluation cutoff and returns stable reason codes with affected series/entity/interval references.
- [ ] Quality API responses are deterministic and machine-readable; missing or invalid policy/configuration fails closed rather than producing a healthy result.

## Required verification

```text
task test-go TEST=Quality
task test-go-integration TEST=QualityAPI
task test-contract
```
