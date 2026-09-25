---
id: AR-201
title: Implement instruments and portfolio snapshots
status: ready
phase: 2
depends_on: [AR-101]
branch: task/AR-201-portfolio-snapshots
base_sha: d4afc4b911997e0f302157a7391c898128af6d25
owned_paths: [internal/domain/portfolio/, internal/application/portfolio/, db/queries/portfolio/, apps/api/handlers/portfolio/]
shared_paths: [db/migrations/, db/queries/core/sqlc.yaml, internal/platform/database/, apps/api/cmd/api/, contracts/openapi/, test/integration/, .github/workflows/ci.yml]
adrs: [ADR-007, ADR-010, ADR-016, ADR-024]
---

# AR-201: Implement instruments and portfolio snapshots

## Outcome

AtlasRisk stores stable instruments, accounts, portfolios, and immutable manual
position snapshots with exact quantities and explicit supported-risk attributes.

## In scope

- Cash/currency, spot crypto, manually priced spot, and fixed-rate bond types.
- Instrument identifiers/lifecycle, account/portfolio configuration, snapshots,
  lines, linked corrections, TRY/USD reporting preferences, and resource APIs.
- Preserve existing immutable instrument identity; only its lifecycle status can
  be changed after creation.

## V1 storage and API contract

- An account belongs to exactly one portfolio. A portfolio snapshot is an
  immutable UTC-timestamped view of its accounts; each position line belongs to
  one of those accounts and references one instrument.
- Supported instrument types are `cash`, `currency`, `spot_crypto`,
  `manual_spot`, and `fixed_rate_bond`. Instrument identity (canonical symbol,
  type, native unit, and external identifiers) is immutable after creation;
  lifecycle status is mutable only among `active`, `inactive`, and `delisted`.
- External instrument identifiers are unique by `(namespace, external_id)` and
  enforced in PostgreSQL. Native instrument units use normalized uppercase
  codes per ADR-024; reporting currency is restricted to `TRY` or `USD`.
- Persist quantity, total cost basis, modified duration, and convexity as exact
  `NUMERIC(38,18)` values. API decimal inputs and outputs are decimal strings,
  never binary floating-point JSON numbers. Cost basis is the total amount in
  the instrument's native unit. Fixed-rate bonds accept a positive
  `modified_duration_years` and optional non-negative `convexity_years_squared`;
  risk attributes unsupported for an instrument type are rejected. Quantity
  sign is preserved as supplied; this task adds no borrow, margin, or execution
  mechanics.
- Committed snapshots and their lines are append-only at the database boundary:
  ordinary SQL `UPDATE` and `DELETE` are rejected. A correction inserts a new
  snapshot with a `supersedes_snapshot_id` reference to a snapshot of the same
  portfolio; prior snapshots remain queryable. A line is unique per
  `(snapshot, account, instrument)`.
- Instrument APIs create/list/read identities and allow lifecycle-status changes
  only; portfolio/account APIs create/list/read/update configuration and allow
  deletion only before referenced. Snapshot APIs create/list/read committed
  snapshots and corrections; they expose no update/delete operation.
- Snapshot creation is atomic. Invalid references, duplicate line identities,
  invalid decimals, unsupported risk fields, or conflicting external identifiers
  return a structured client error and leave no partial snapshot or identifier.

## Out of scope

- Transactions, tax lots, realized P&L, broker sync, valuation, or risk metrics.

## Acceptance criteria

- [ ] Integration proves committed snapshots and lines reject `UPDATE`/`DELETE` through the application API and direct SQL; snapshot API has no mutating route.
- [ ] Corrections create a same-portfolio linked replacement; the original and each prior correction remain unchanged and queryable.
- [ ] Tests preserve quantity, total cost basis, modified duration, and convexity at 18 fractional digits through API and `NUMERIC(38,18)` storage; API contracts use decimal strings.
- [ ] Duplicate external IDs are rejected by a database uniqueness constraint for the same namespace, while equal IDs in different namespaces are accepted, including concurrent inserts.
- [ ] Every supported instrument type accepts its allowed attributes; invalid types, unsupported risk attributes, invalid status, wrong-portfolio account references, wrong-instrument references, and malformed decimal strings return structured errors without partial writes.
- [ ] Portfolio/account CRUD and reporting-currency validation are covered; resources cannot be deleted after snapshots or lines reference them.
- [ ] Migrations pass from empty schema and the current previous schema, including append-only and identifier-uniqueness constraints.
- [ ] OpenAPI and generated contracts describe resource/snapshot/correction behavior without generated-file drift.

## Required verification

```text
task migrate-test
task test-go TEST=Portfolio
task test-go-integration TEST=PortfolioAPI
task test-contract
task verify
```
