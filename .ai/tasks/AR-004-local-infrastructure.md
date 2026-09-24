---
id: AR-004
title: Provide isolated local infrastructure
status: review
phase: 0
depends_on: [AR-002]
branch: task/AR-004-local-infrastructure
base_sha: fd2d7b5a306d2ab29c30ea41ec8292c546e6eee4
owned_paths: [infra/compose/, scripts/infra/]
shared_paths: [.env.example, Taskfile.yml, README.md]
adrs: [ADR-001, ADR-004, ADR-005, ADR-017]
---

# AR-004: Provide isolated local infrastructure

## Outcome

A developer can start healthy PostgreSQL 18 and S3-compatible development storage
locally without exposing services broadly or committing credentials.

## In scope

- Docker Compose services for PostgreSQL 18 and Garage 2.x, health checks, named
  volumes, localhost-only ports, and deterministic bucket/key bootstrap.
- Non-secret example configuration and separate test project/volumes.
- Start, stop, status, and log commands; documented data-reset command with an
  explicit destructive warning.
- Pinned image versions/digests and non-root configuration where supported.

## Out of scope

- Production hosting, backup implementation, database schema, or app containers.

## Acceptance criteria

- [ ] Services become healthy from a clean environment.
- [ ] PostgreSQL and object storage are reachable only through declared local ports.
- [x] Test infrastructure cannot reuse development databases/buckets.
- [ ] Garage supports the exact Put/Get/Head/List operations used by the archive adapter.
- [x] No real credential or personal data appears in tracked configuration.

## Required verification

```text
docker compose -f infra/compose/compose.yaml config
docker compose -f infra/compose/compose.yaml up -d --wait
docker compose -f infra/compose/compose.yaml ps
task test-infra
```
