---
id: AR-302
title: Implement core portfolio risk metrics
status: draft
phase: 3
depends_on: [AR-301]
branch: task/AR-302-core-risk-metrics
owned_paths: [risk-engine/src/atlasrisk/returns/, risk-engine/src/atlasrisk/metrics/, risk-engine/tests/metrics/]
shared_paths: [contracts/jobs/, test/fixtures/risk/]
adrs: [ADR-010, ADR-011, ADR-013]
---

# AR-302: Implement core portfolio risk metrics

## Outcome

The risk engine deterministically computes documented returns, volatility,
correlation/coverage, drawdown, leverage, and concentration from a sealed bundle.

## In scope

- Weekday cross-asset and seven-day crypto-only calendars.
- Log returns, 63-observation annualized volatility, 252-window correlation with
  60-overlap minimum, base-currency NAV drawdown, gross/net leverage, top weights,
  and HHI.
- Property/golden tests and structured quality/coverage output.

## Out of scope

- VaR/CVaR, forecasts, optimization, forward filling, or scenario shocks.

## Acceptance criteria

- [ ] Constant, sparse, all-missing, negative/zero NAV, and misaligned calendars are tested.
- [ ] No tradable price is forward-filled to manufacture returns.
- [ ] Every correlation coefficient includes overlap count and state.
- [ ] Annualization/calendar choice is present in the result.
- [ ] Golden results declare numerical tolerances and are stable across reruns.

## Required verification

```text
task test-python TEST=metrics
task test-contract
task test-golden TEST=risk_metrics
```
