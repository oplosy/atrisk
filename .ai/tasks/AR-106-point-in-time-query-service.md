---
id: AR-106
title: Expose point-in-time data queries
status: merged
phase: 1
depends_on: [AR-103, AR-104, AR-105]
branch: task/AR-106-point-in-time-query-service
base_sha: bdc6ab0d5c0b8a8039df3614a1d57027c281f25a
owned_paths: [internal/application/timeline/, apps/api/handlers/timeline/, apps/api/cmd/api/, db/queries/timeline/, test/integration/]
shared_paths: [contracts/openapi/, apps/web/src/generated/, db/queries/core/sqlc.yaml, internal/platform/database/, .github/workflows/ci.yml]
adrs: [ADR-006, ADR-007, ADR-009, ADR-011]
---

# AR-106: Expose point-in-time data queries

## Outcome

The API exposes stable latest, source-as-of, system-as-of, revision, and combined
timeline queries with explicit semantics and provenance.

## In scope

- Series list/detail, observation windows, revision comparison, provenance, and
  cross-source timeline endpoints.
- Cursor pagination, stable ordering, cutoff validation, and unsupported-source
  semantics when source vintages do not exist.

## Out of scope

- Charts, risk calculations, or implicit fallback between as-of modes.

## Acceptance criteria

- [x] The same fixture produces distinct latest/source/system answers where expected.
- [x] Unsupported source-as-of requests return an explicit capability response.
- [x] Every value includes unit, frequency, clocks, quality, and raw provenance ID.
- [x] Pagination is stable across identical immutable snapshots.
- [x] OpenAPI and generated clients contain no drift.

## Required verification

```text
task test-go TEST=Timeline
task test-go-integration TEST=PointInTimeAPI
task test-contract
task check-generated
```
