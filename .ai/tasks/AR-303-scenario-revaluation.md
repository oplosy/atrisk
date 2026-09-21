---
id: AR-303
title: Implement versioned scenario revaluation
status: draft
phase: 3
depends_on: [AR-301]
branch: task/AR-303-scenario-revaluation
owned_paths: [risk-engine/src/atlasrisk/scenarios/, risk-engine/tests/scenarios/, internal/application/scenarios/]
shared_paths: [contracts/jobs/, db/migrations/, db/queries/scenarios/, test/fixtures/risk/]
adrs: [ADR-011, ADR-013, ADR-015]
---

# AR-303: Implement versioned scenario revaluation

## Outcome

AtlasRisk can create immutable scenario versions and fully revalue supported
positions under the three V1 templates with explicit coverage and post-shock risk.

## In scope

- Scenario/version persistence and jobs for TRY depreciation, rates up, and risk-off.
- Spot price/FX revaluation, bond duration-convexity approximation, factor mappings,
  volatility/correlation post-shock metrics, and blocked/unmapped states.

## Out of scope

- Options, Monte Carlo, user code, or treating volatility/correlation as spot cash P&L.

## Acceptance criteria

- [ ] A scenario edit creates a new version and old runs keep the old version.
- [ ] Position P&L sums to portfolio P&L within recorded tolerance.
- [ ] Missing required factor mappings are listed and block according to policy.
- [ ] Rates use explicit basis-point units and stored duration/convexity assumptions.
- [ ] Vol/correlation shocks alter risk metrics without inventing spot P&L.

## Required verification

```text
task test-python TEST=scenarios
task test-go TEST=Scenarios
task test-golden TEST=scenarios
task test-contract
```
