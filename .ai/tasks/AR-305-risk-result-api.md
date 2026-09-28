---
id: AR-305
title: Expose risk and stress results
status: ready
phase: 3
depends_on: [AR-302, AR-304]
branch: task/AR-305-risk-result-api
base_sha: 016ab9239071b2d77c863f865a2ceb7273ba016b
owned_paths: [internal/application/risk/, apps/api/handlers/risk/, db/queries/risk/]
shared_paths: [apps/api/cmd/api/main.go, contracts/openapi/, contracts/jobs/, contracts/generated/, scripts/generate/contract-models.mjs, apps/web/src/generated/, test/integration/, Taskfile.yml]
adrs: [ADR-009, ADR-011, ADR-013, ADR-014, ADR-015]
---

# AR-305: Expose risk and stress results

## Outcome

The API submits idempotent calculations and exposes lifecycle, metrics, coverage,
attribution, quality, provenance, and engine/schema versions without leaking tables.

## In scope

- Submit/status/cancel/read endpoints, result pagination, canonical identifiers,
  reason codes, snapshot links, and completed-result immutability.

## Out of scope

- UI, synchronous long-running requests, or calculation logic in Go.

## Acceptance criteria

- [ ] Repeated submission with one idempotency key returns one semantic run.
- [ ] Clients can distinguish queued/running/retryable/permanent/completed states.
- [ ] Completed payload exposes all input and version provenance.
- [ ] Invalid/degraded/blocked states cannot be mistaken for valid.
- [ ] OpenAPI-generated clients round-trip representative golden results.

## Required verification

```text
task test-go TEST=RiskAPI
task test-integration TEST=RiskEndToEnd
task test-contract
task check-generated
```
