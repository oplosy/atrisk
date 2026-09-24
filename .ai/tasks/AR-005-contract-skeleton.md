---
id: AR-005
title: Establish contract-first interfaces
status: ready
phase: 0
depends_on: [AR-002, AR-003]
branch: task/AR-005-contract-skeleton
base_sha: b59acf82fda547e84b99a00cf9d9a5601dac6f59
owned_paths: [contracts/, scripts/generate/, test/contract/]
shared_paths: [apps/api/, apps/web/, risk-engine/, Taskfile.yml]
adrs: [ADR-009]
---

# AR-005: Establish contract-first interfaces

## Outcome

HTTP, asynchronous job, and CSV contracts have versioned sources of truth,
generated language types, validation tests, and a no-drift gate.

## In scope

- OpenAPI 3.1 base document, error envelope, pagination, request IDs, and
  idempotency conventions.
- JSON Schema base envelopes for jobs/results and import-manifest conventions.
- Repeatable generation of Go and TypeScript types plus Python validation models.
- Contract fixtures for valid, invalid, unknown-version, and additive-change cases.

## Out of scope

- Domain endpoints, real job kinds, or actual CSV import behavior.

## Acceptance criteria

- [ ] Generation from a clean checkout is deterministic.
- [ ] Generated files are labeled and never hand-authored.
- [ ] Unknown schema versions fail with a stable machine-readable error.
- [ ] `task check-generated` reports no diff after regeneration.

## Required verification

```text
task generate
task test-contract
task check-generated
git status --short
```
