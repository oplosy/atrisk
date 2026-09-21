---
id: AR-107
title: Implement data-quality gates
status: draft
phase: 1
depends_on: [AR-101, AR-106]
branch: task/AR-107-data-quality-engine
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

- [ ] Fixed-clock tests cover holidays, weekends, late releases, gaps, and revisions.
- [ ] Required missing/suspect input always blocks dependent calculations.
- [ ] Optional stale/partial input degrades but never reports valid.
- [ ] Quality evaluation records policy version and evaluation cutoff.
- [ ] Responses are deterministic and machine-readable.

## Required verification

```text
task test-go TEST=Quality
task test-go-integration TEST=QualityAPI
task test-contract
```
