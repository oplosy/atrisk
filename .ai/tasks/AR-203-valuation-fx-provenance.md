---
id: AR-203
title: Implement valuation and FX provenance
status: review
phase: 2
depends_on: [AR-106, AR-201]
branch: task/AR-203-valuation-fx-provenance
base_sha: 4d8f8be908e2986dfeae3b852b834f8df754d808
owned_paths: [internal/domain/valuation/, internal/application/valuation/, db/queries/valuation/, apps/api/handlers/valuation/]
shared_paths: [db/migrations/, db/queries/core/sqlc.yaml, internal/platform/database/, apps/api/cmd/api/, contracts/openapi/, apps/web/src/generated/, test/integration/, test/fixtures/valuation/, .github/workflows/ci.yml]
adrs: [ADR-010, ADR-011, ADR-012]
---

# AR-203: Implement valuation and FX provenance

## Outcome

An immutable portfolio snapshot is valued reproducibly in native currency, TRY,
and USD using cutoff/freshness-aware prices and an inspectable deterministic FX path.

## In scope

- Price selection, direct/USD-bridge FX graph, exact line/NAV arithmetic, persisted
  quote lineage, immutable valuation runs/results, API and quality aggregation.
- Request-scoped freshness policy, persisted with every valuation run; do not add
  mutable per-instrument freshness configuration in this task.
- API/OpenAPI/generated contract, migration/query generation, and PostgreSQL-backed
  integration coverage for the valuation endpoint. The valuation service reads
  raw-object provenance through immutable revision rows; archive storage behavior
  remains covered by AR-102 and is not an S3 dependency of this endpoint.

## V1 valuation contract

- `POST /api/v1/valuations` accepts an immutable `snapshot_id`, an explicit UTC
  observation `cutoff`, an explicit `knowledge_mode` (`system_as_of` or
  `source_as_of`) and matching `known_at`, a required `price_max_age_seconds`, and
  a required `fx_max_age_seconds`. Persist the complete normalized request/policy
  on the immutable valuation run. Do not default either as-of clock or freshness.
- Resolve prices and FX only at/before `cutoff`, within their respective max ages,
  and visible at the selected knowledge clock. `system_as_of` filters
  `system_known_at <= known_at`; `source_as_of` filters non-null
  `source_known_at <= known_at` and never falls back to system time. Stable ties
  use knowledge time descending, then `system_known_at` descending, then UUID
  ascending. If source-as-of yields no eligible quote, report it missing.
- A priced line's native amount is quantity times the selected price revision;
  retain the exact `price_revision_id` and quote currency. A `cash` or `currency`
  line uses the explicit `identity` method (unit price `1`, no price revision ID)
  and native amount equal to quantity. Identity does not imply any peg or FX
  conversion.
- FX graph edges come only from persisted fiat `fx_quote_revisions`; traverse a
  quote forward at its stored rate or backward at its reciprocal. For a requested
  conversion, prefer a one-edge direct path over a two-edge USD bridge; the bridge
  is allowed only when neither direct orientation is eligible. Do not search for
  best rates, use more than two edges, or treat asset tickers (including USDT) as
  fiat currencies. Persist quote IDs in traversal order and each edge direction.
- Compute native, TRY, and USD line/NAV amounts with exact decimal arithmetic and
  fixed `NUMERIC(38,18)` persistence; API decimal values are strings. If a required
  price or conversion path is unavailable/stale, retain the line and reason code,
  mark the run `blocked`, and never present a complete NAV. Same normalized request
  and selected immutable input IDs must yield the same canonical result hash.
- A valuation run is append-only and links its source portfolio snapshot. Store
  per-line native/reporting amounts, quality state/reasons, selected price method
  and ID (nullable only for identity), ordered FX quote IDs/directions, and the
  raw provenance reachable through each revision.

## Out of scope

- Risk statistics, scenario shocks, best-rate routing, or forward FX.

## Acceptance criteria

- [ ] Each priced line records its price revision; each identity line explicitly
  records method `identity` and no price ID; each conversion records ordered FX
  revision IDs and edge direction.
- [ ] Tests prove future, stale, and not-yet-known revisions are excluded for both
  knowledge modes, including no source-time fallback.
- [ ] Direct forward/reverse paths outrank USD bridge and ties resolve stably;
  no path is inferred through asset tickers or more than two edges.
- [ ] Cash identity valuation works without a price revision and does not peg
  convert; missing USDT-to-USD explicit path is blocked.
- [ ] TRY/USD totals equal the sum of persisted line amounts at the stored scale;
  18-digit inputs survive API and PostgreSQL storage.
- [ ] Required missing/stale prices or FX block the run with stable reason codes;
  successful and blocked run records remain immutable.
- [ ] Identical normalized inputs and selected revision IDs produce the same
  canonical result hash.
- [ ] OpenAPI/generated types are in sync; PostgreSQL integration exercises the
  actual HTTP endpoint and proves selected revisions retain reachable raw-object
  provenance. S3/archive behavior is covered by AR-102 archive/ingestion tests.

## Required verification

```text
task test-go TEST=Valuation
task test-go-integration TEST=ValuationAPI
task test-contract
task check-generated
task migrate-test
task verify
```
