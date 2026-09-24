---
id: AR-102
title: Build raw archive and ingestion framework
status: active
phase: 1
depends_on: [AR-101]
branch: task/AR-102-raw-archive-ingestion
base_sha: 76b0877574de6c609433eb9a893fcb9817ca07ef
owned_paths: [apps/collector/, internal/archive/, internal/ingestion/, test/fixtures/http/]
shared_paths: [db/migrations/, db/queries/, Taskfile.yml, .github/workflows/ci.yml]
adrs: [ADR-005, ADR-006, ADR-007, ADR-017]
---

# AR-102: Build raw archive and ingestion framework

## Outcome

Adapters can fetch bounded source responses, archive redacted immutable payloads,
and normalize them through idempotent ingestion runs that can be replayed offline.

## In scope

- Adapter/fetcher interface, allowlisted HTTPS client, timeouts, size bounds,
  retry/backoff, rate-limit hooks, and request correlation.
- SHA-256 content-addressed object writes and database registration.
- AWS SDK for Go v2 S3 client constrained to the Garage-tested operation subset.
- Secret-safe request metadata, run states, metrics, and fixture replay mode.

## Out of scope

- Provider-specific parsing or scheduling UI.

## Acceptance criteria

- [ ] Same bytes produce one raw object under concurrent writes.
- [ ] Normalized records reference the exact archived response.
- [ ] API keys/authorization values never enter objects, metadata, or logs.
- [ ] Timeout, oversized payload, invalid media type, and storage failure are tested.
- [ ] A saved fixture reproduces normalization without network access.

## Required verification

```text
task test-go TEST=Ingestion
task test-go-integration TEST=RawArchive
task verify
```
