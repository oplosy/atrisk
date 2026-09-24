---
id: AR-104
title: Add TCMB EVDS ingestion
status: review
phase: 1
depends_on: [AR-102]
branch: task/AR-104-tcmb-adapter
base_sha: 94a4c3eb689ee82e3a835b21ebb5344d62650fc5
owned_paths: [internal/sources/tcmb/, test/fixtures/tcmb/]
shared_paths: [apps/collector/, db/queries/, contracts/, test/integration/, internal/ingestion/]
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
- Confirm the endpoint generation, authentication, and response semantics against
  the [official EVDS documentation](https://evds3.tcmb.gov.tr/dokumanlar); keep
  the API base URL configurable and do not assume an endpoint is permanent.
- Shared-path rationale: EVDS credentials use an HTTP `key` header, while the
  common fetcher currently filters unknown headers. Authorize `internal/ingestion/`
  only for safe credential forwarding; ensure it is omitted from persisted
  request metadata and stripped before following redirects.

## Out of scope

- Treating retrieval time as a fabricated official publication timestamp.

## Acceptance criteria

- [ ] Unknown source publication time remains null with an explicit basis; retrieval time is never substituted for publication time.
- [ ] Decimal separators and locale/date rules normalize deterministically.
- [ ] Missing/late expected periods create quality evidence, not zero values.
- [ ] Re-fetched changed values create append-only revisions retaining raw provenance; unchanged values are idempotent.
- [ ] Logs, raw metadata, and redirected requests contain no EVDS key.

## Required verification

```text
task test-go TEST=TCMB
task test-go-integration TEST=TCMBIngestion
task test-contract
```
