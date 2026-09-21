---
id: AR-201
title: Implement instruments and portfolio snapshots
status: draft
phase: 2
depends_on: [AR-101]
branch: task/AR-201-portfolio-snapshots
owned_paths: [internal/domain/portfolio/, internal/application/portfolio/, db/queries/portfolio/, apps/api/handlers/portfolio/]
shared_paths: [db/migrations/, contracts/openapi/]
adrs: [ADR-007, ADR-010, ADR-016]
---

# AR-201: Implement instruments and portfolio snapshots

## Outcome

AtlasRisk stores stable instruments, accounts, portfolios, and immutable manual
position snapshots with exact quantities and explicit supported-risk attributes.

## In scope

- Cash, currency, spot crypto, manually priced spot, and fixed-rate bond types.
- Instrument identifiers/lifecycle, account/portfolio configuration, snapshots,
  lines, amendments, TRY/USD reporting preferences, and CRUD API.

## Out of scope

- Transactions, tax lots, realized P&L, broker sync, valuation, or risk metrics.

## Acceptance criteria

- [ ] A committed snapshot cannot be edited or deleted through API or SQL paths.
- [ ] Corrections create linked replacement snapshots without losing the original.
- [ ] Quantity/cost/duration precision round-trips exactly.
- [ ] Duplicate external instrument IDs are constrained within their namespace.
- [ ] Unsupported risk attributes produce validation errors, not ignored fields.

## Required verification

```text
task migrate-test
task test-go TEST=Portfolio
task test-go-integration TEST=PortfolioAPI
task test-contract
```
