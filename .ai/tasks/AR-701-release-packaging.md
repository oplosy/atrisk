---
id: AR-701
title: Package release images and web bundle
status: active
phase: 7
depends_on: [AR-602]
branch: task/AR-701-release-packaging
base_sha: de523e09194e857af58b29783bc66488b776bc62
owned_paths: [infra/images/, scripts/release/, risk-engine/src/atlasrisk/jobs/runner.py, risk-engine/tests/test_runner.py, docs/releases/]
shared_paths: [apps/api/cmd/api/, internal/platform/database/migrate.go, internal/platform/database/migrate_test.go, risk-engine/pyproject.toml, risk-engine/uv.lock, risk-engine/README.md, .github/workflows/]
adrs: [ADR-001, ADR-003, ADR-008, ADR-017, ADR-018]
---

# AR-701: Package release images and web bundle

## Outcome

A pushed release tag produces everything a self-hosted installation needs to run
AtlasRisk without the source tree: an API image that can also apply migrations,
a risk-worker image that processes the PostgreSQL job queue, and the built web
bundle, each traceable to the tag by digest or checksum.

## Context

The SecureEdge deployment (separate repository) runs the API and risk worker as
containers next to PostgreSQL and Garage on an app server, and serves the web
bundle from its edge proxy. It needs pinned images and a migration step it can
run before starting a new API version. Today the API has no production
migration path (`database.Migrate` accepts only the isolated test database), and
the risk worker exists only as a library (`JobWorker.run_claimed_once`).

## In scope

- `atlasrisk-api migrate` applies `db/migrations` to `ATLASRISK_DATABASE_URL`
  and exits; the isolated-database guard stays on the test helpers.
- `atlasrisk-risk-worker` polls the queue through `PostgresQueueClient`,
  reconnects after database errors, stops cleanly on SIGTERM/SIGINT, and logs
  without job payloads or connection strings.
- Non-root, digest-pinned Dockerfiles for both processes; configuration only
  through environment variables.
- CI builds both images on every pull request and smoke-tests them against the
  CI PostgreSQL service.
- A tag-triggered release workflow that pushes both images to GHCR and creates
  a GitHub release with the web bundle, its checksum, and the image digests.
- Release documentation.

## Out of scope

- The collector process (still a toolchain skeleton) and durable collector
  schedules (ADR-023).
- Deployment manifests, servers, TLS, and authentication (SecureEdge owns them).
- Changes to job claim, lease, or retry semantics.

## Inputs and contracts

- Environment: `ATLASRISK_DATABASE_URL` (both images), the existing
  `ATLASRISK_S3_*` and `AWS_*` variables (API), `ATLASRISK_WORKER_POLL_SECONDS`
  and `ATLASRISK_WORKER_LEASE_SECONDS` (worker, optional).
- Engine version recorded in results stays `atlasrisk-risk-engine-<version>`.

## Acceptance criteria

- [ ] `migrate` without `ATLASRISK_DATABASE_URL` exits 2; with it, it applies all
      migrations and a second run is a no-op.
- [ ] `database.Migrate` still refuses non-isolated targets.
- [ ] The worker processes queued jobs, waits when the queue is empty,
      reconnects after a connection failure, and exits 0 on SIGTERM.
- [ ] Both images run as a non-root user and pin every base image by digest.
- [ ] CI builds both images and proves migrate and worker start against PostgreSQL.
- [ ] A release tag publishes both images and a release with the web bundle,
      a SHA-256 checksum, and the image digests; tags not on `main` fail.

## Required verification

```text
go test ./apps/api/... ./internal/platform/database/...
uv run --project risk-engine --locked pytest tests/test_runner.py
bash scripts/release/smoke-images.sh   (CI, against the PostgreSQL service)
task verify
```

## Handoff evidence

- Commit SHA and pushed branch.
- Changed files.
- Commands and results.
- Acceptance-criterion mapping.
- Remaining risks or `none`.
