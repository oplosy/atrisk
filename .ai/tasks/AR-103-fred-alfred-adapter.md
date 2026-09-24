---
id: AR-103
title: Add FRED and ALFRED vintage ingestion
status: ready
phase: 1
depends_on: [AR-102]
branch: task/AR-103-fred-alfred-adapter
base_sha: 8ef045173a4e768cef88ab805cd4cdc90ec2fcc4
owned_paths: [internal/sources/fred/, test/fixtures/fred/]
shared_paths: [apps/collector/, db/queries/, contracts/, test/integration/]
adrs: [ADR-005, ADR-006, ADR-007, ADR-011]
---

# AR-103: Add FRED and ALFRED vintage ingestion

## Outcome

Configured FRED series ingest metadata, observations, vintage dates, and real-time
periods without losing revisions or confusing latest and historical knowledge.

## In scope

- Series metadata and observations with explicit vintage/realtime parameters.
- Pagination/limits, API-key redaction, backfill checkpoints, and rate handling.
- Fixtures containing initial release, revision, missing value, and duplicate fetch.

## Out of scope

- Arbitrary catalog discovery UI or transformations not supplied by the source.

## Acceptance criteria

- [ ] Initial and revised values coexist and retain raw provenance.
- [ ] Source-as-of returns the value known on the requested vintage date.
- [ ] System-as-of excludes data not yet ingested by this installation.
- [ ] FRED missing markers are not coerced to zero.
- [ ] A rerun is idempotent and resumes from a persisted checkpoint.

## Required verification

```text
task test-go TEST=FRED
task test-go-integration TEST=FREDVintage
task test-contract
```
