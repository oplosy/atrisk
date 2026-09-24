---
id: AR-101
title: Implement the core point-in-time database model
status: active
phase: 1
depends_on: [AR-004, AR-005]
branch: task/AR-101-core-database-model
base_sha: dff9950f4b24e7d2c942599c1cc56e8122aea636
owned_paths: [db/migrations/, db/queries/core/, internal/platform/database/, internal/domain/marketdata/]
shared_paths: [Taskfile.yml, test/integration/]
adrs: [ADR-004, ADR-005, ADR-006, ADR-007, ADR-010]
---

# AR-101: Implement the core point-in-time database model

## Outcome

PostgreSQL can persist immutable source metadata, raw-object references, ingestion
runs, observation revisions, price revisions, and FX revisions with exact types.

## In scope

- Forward SQL migrations, constraints, indexes, and generated query layer.
- Three-clock fields and explicit knowledge-time basis.
- Append-only enforcement and idempotency identities.
- Empty-database and previous-version migration tests.

## Out of scope

- Source adapters, object bytes, API endpoints, portfolio tables, or partitioning
  without an explained query-plan need.

## Acceptance criteria

- [ ] Changed values insert new revisions; historical rows cannot be updated/deleted.
- [ ] Identical normalized/raw identities are idempotent under concurrent insert.
- [ ] Exact values round-trip without precision loss.
- [ ] Source and system knowledge timestamps may differ or source time may be null.
- [ ] Query plans use intended indexes for series/time/as-of fixture queries.

## Required verification

```text
task migrate-test
task test-go-integration TEST=CoreDatabase
task check-generated
```
