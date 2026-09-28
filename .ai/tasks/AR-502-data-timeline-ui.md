---
id: AR-502
title: Build the point-in-time data timeline
status: review
phase: 5
depends_on: [AR-106, AR-501]
branch: task/AR-502-data-timeline-ui
owned_paths: [apps/web/src/features/timeline/, apps/web/src/routes/timeline/]
shared_paths: [apps/web/src/components/, apps/web/src/routes/root/, apps/web/src/app/, apps/web/src/styles.css, apps/web/src/App.test.tsx]
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
- [ ] `/timeline` route remains compatible with shell navigation and browser history.

## Required verification

```text
npm test -- --run
npm run typecheck
npm run build
npm run lint
npm run format:check
```

Record browser evidence for `/timeline` at desktop and 320px, including keyboard
navigation and empty/error/paginated states. Hosted PR CI must pass `task verify`.
