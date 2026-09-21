---
id: AR-504
title: Build risk and stress analysis UI
status: draft
phase: 5
depends_on: [AR-305, AR-501]
branch: task/AR-504-risk-stress-ui
owned_paths: [apps/web/src/features/risk/, apps/web/src/routes/risk/]
shared_paths: [apps/web/src/components/, apps/web/src/generated/]
adrs: [ADR-011, ADR-013, ADR-014, ADR-015]
---

# AR-504: Build risk and stress analysis UI

## Outcome

Users can run and inspect risk/stress calculations with coverage, input versions,
loss reconciliation, factor attribution, and unmapped-input warnings.

## In scope

- Job lifecycle, metric cards/tables, correlation coverage, drawdown, leverage,
  concentration, scenario/version selector, loss waterfall, factor/position detail,
  interaction residual, and provenance drawer.

## Out of scope

- Client-side recomputation, trading language, or generated explanatory claims.

## Acceptance criteria

- [ ] Queued/running/retrying/failed/blocked/degraded/valid states are explicit.
- [ ] Total, position, factor, and residual figures visibly reconcile.
- [ ] Coverage/sample windows and calendar/annualization choices are visible.
- [ ] Unmapped instruments cannot disappear from summary results.
- [ ] Dense correlation and attribution views remain keyboard/screen-reader usable.

## Required verification

```text
task test-web TEST=risk
task test-web-a11y ROUTE=/risk
task test-e2e TEST=risk_stress
```
