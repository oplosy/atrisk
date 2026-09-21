---
id: AR-203
title: Implement valuation and FX provenance
status: draft
phase: 2
depends_on: [AR-106, AR-201]
branch: task/AR-203-valuation-fx-provenance
owned_paths: [internal/domain/valuation/, internal/application/valuation/, db/queries/valuation/, apps/api/handlers/valuation/]
shared_paths: [db/migrations/, contracts/openapi/, test/fixtures/valuation/]
adrs: [ADR-010, ADR-011, ADR-012]
---

# AR-203: Implement valuation and FX provenance

## Outcome

An immutable portfolio snapshot is valued reproducibly in native currency, TRY,
and USD using cutoff/freshness-aware prices and an inspectable deterministic FX path.

## In scope

- Price selection, direct/USD-bridge FX graph, exact line/NAV arithmetic, persisted
  quote lineage, valuation snapshot/result APIs, and quality aggregation.
- Instrument-specific freshness configuration and unpriced-line behavior.

## Out of scope

- Risk statistics, scenario shocks, best-rate routing, or forward FX.

## Acceptance criteria

- [ ] Every line records selected price ID and ordered FX quote IDs.
- [ ] Quotes later than the cutoff or outside freshness policy are never selected.
- [ ] Direct paths outrank USD bridge according to documented deterministic rules.
- [ ] TRY and USD totals reconcile to exact line sums within stored decimal scale.
- [ ] Required unpriced/stale lines make the valuation blocked with reason codes.
- [ ] Re-running identical inputs yields the same canonical result hash.

## Required verification

```text
task test-go TEST=Valuation
task test-go-integration TEST=ValuationAPI
task test-contract
```
