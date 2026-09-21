---
id: AR-104
title: Add TCMB EVDS ingestion
status: draft
phase: 1
depends_on: [AR-102]
branch: task/AR-104-tcmb-adapter
owned_paths: [internal/sources/tcmb/, test/fixtures/tcmb/]
shared_paths: [apps/collector/, db/queries/, contracts/]
adrs: [ADR-005, ADR-006, ADR-007, ADR-011]
---

# AR-104: Add TCMB EVDS ingestion

## Outcome

Configured TCMB EVDS macro and FX series ingest with exact source metadata,
honest publication-time precision, and visible delayed/missing periods.

## In scope

- Series/value requests, units, frequency, date parsing, backfill checkpoints,
  API-key redaction, source errors, and official aggregation metadata.
- Fixtures covering holiday gaps, missing tokens, late observations, and revisions.

## Out of scope

- Treating retrieval time as a fabricated official publication timestamp.

## Acceptance criteria

- [ ] Unknown source publication time remains null with an explicit basis.
- [ ] Decimal separators and locale/date rules normalize deterministically.
- [ ] Missing/late expected periods create quality evidence, not zero values.
- [ ] Re-fetched changed values create revisions and unchanged values are idempotent.
- [ ] Logs and raw metadata contain no EVDS key.

## Required verification

```text
task test-go TEST=TCMB
task test-go-integration TEST=TCMBIngestion
task test-contract
```
