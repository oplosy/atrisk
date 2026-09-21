---
id: AR-503
title: Build portfolio valuation and reconciliation UI
status: draft
phase: 5
depends_on: [AR-204, AR-501]
branch: task/AR-503-portfolio-ui
owned_paths: [apps/web/src/features/portfolio/, apps/web/src/routes/portfolio/]
shared_paths: [apps/web/src/components/, apps/web/src/generated/]
adrs: [ADR-010, ADR-011, ADR-012, ADR-016]
---

# AR-503: Build portfolio valuation and reconciliation UI

## Outcome

Users can safely create/import position snapshots, inspect native/TRY/USD valuation
lineage, and record reconciliation checkpoints with explicit completeness state.

## In scope

- Manual snapshot form, CSV preview/commit, valuation table, currency selector,
  price/FX-path inspection, unpriced lines, and reconciliation comparison.

## Out of scope

- Brokerage sync, inline risk formulas, trade entry, or automatic data correction.

## Acceptance criteria

- [ ] CSV commit cannot occur before preview for the same content hash.
- [ ] Every displayed converted line exposes selected price and ordered FX path.
- [ ] Blocked/incomplete valuation is prominent and never summarized as full NAV.
- [ ] Reconciliation displays tolerance, absolute/relative difference, and cutoff.
- [ ] Forms preserve user input on recoverable validation/server errors.

## Required verification

```text
task test-web TEST=portfolio
task test-web-a11y ROUTE=/portfolio
task test-e2e TEST=portfolio_reconciliation
```
