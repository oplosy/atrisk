---
id: AR-202
title: Add safe manual CSV imports
status: draft
phase: 2
depends_on: [AR-201]
branch: task/AR-202-manual-csv-imports
owned_paths: [internal/imports/, apps/api/handlers/imports/, contracts/imports/, test/fixtures/imports/]
shared_paths: [contracts/openapi/, db/queries/portfolio/]
adrs: [ADR-009, ADR-010, ADR-016]
---

# AR-202: Add safe manual CSV imports

## Outcome

The user can preview and atomically commit versioned position and manual-price CSV
imports with row-level diagnostics and no partial or executable content.

## In scope

- Published UTF-8 CSV schemas/examples, strict headers, decimal/date/currency rules.
- Upload bounds, content sniffing, spreadsheet-formula protection, preview token,
  validation report, idempotency, and atomic commit.

## Out of scope

- XLSX, automatic column guessing, broker-specific formats, or background execution.

## Acceptance criteria

- [ ] Invalid files change no domain rows and return bounded row-level errors.
- [ ] Reusing a committed idempotency key returns the original result.
- [ ] Preview and commit verify the same content hash and schema version.
- [ ] Formula-prefixed text, duplicate rows, locale ambiguity, and oversized files fail safely.
- [ ] Successful import retains raw artifact and creates immutable revisions/snapshots.

## Required verification

```text
task test-go TEST=CSVImport
task test-go-integration TEST=ImportAPI
task test-contract
```
