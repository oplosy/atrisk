---
id: AR-502
title: Build the point-in-time data timeline
status: draft
phase: 5
depends_on: [AR-106, AR-501]
branch: task/AR-502-data-timeline-ui
owned_paths: [apps/web/src/features/timeline/, apps/web/src/routes/data/]
shared_paths: [apps/web/src/components/, apps/web/src/generated/]
adrs: [ADR-006, ADR-007, ADR-011]
---

# AR-502: Build the point-in-time data timeline

## Outcome

Users can inspect latest, source-as-of, and system-as-of observations, revisions,
quality, units, and raw provenance without confusing their semantics.

## In scope

- Series search/list/detail, mode/cutoff selector, observation chart/table,
  revision comparison, timeline, freshness and provenance panels.

## Out of scope

- Editing source data, macro forecasts, or hiding unsupported source-as-of mode.

## Acceptance criteria

- [ ] Mode and cutoff remain visible next to all values/charts.
- [ ] Revision comparison shows old/new value and both knowledge clocks.
- [ ] Unsupported source vintages have a direct explanatory state.
- [ ] Missing/stale/partial/suspect status is visible without opening a tooltip.
- [ ] Route works for empty, one-point, dense, error, and paginated fixtures.

## Required verification

```text
task test-web TEST=timeline
task test-web-a11y ROUTE=/data
task test-e2e TEST=timeline
```
