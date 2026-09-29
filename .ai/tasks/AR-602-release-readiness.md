---
id: AR-602
title: Establish backup restore and release gate
status: merged
phase: 6
depends_on: [AR-601]
branch: task/AR-602-release-readiness
base_sha: 608d0c632f245bce9d4fb49feb1da705d259e154
owned_paths: [scripts/backup/, docs/runbooks/, docs/releases/, infra/release/]
shared_paths: [.github/workflows/, Taskfile.yml, SECURITY.md, risk-engine/pyproject.toml, risk-engine/uv.lock]
adrs: [ADR-001, ADR-005, ADR-017, ADR-018]
---

# AR-602: Establish backup restore and release gate

## Outcome

A release candidate can be backed up, restored into a clean environment, scanned,
and verified without losing point-in-time evidence or silently changing results.

## In scope

- PostgreSQL and object-archive backup/restore scripts, manifest/checksums,
  encryption guidance, retention/runbook, restore drill, migration-from-previous,
  SBOM, secret/dependency scanning, and release checklist.
- Resolve the release scan's PYSEC-2026-1845 finding by pinning the test-only
  pytest dependency to 9.0.3 and regenerating its lockfile; no runtime dependency changes.

## Out of scope

- Cloud deployment, paid backup provider, automatic production retention, or release publishing.

## Execution guardrails

- Restore drills use a fresh, isolated test database and bucket; never restore over the backup source or a user installation.
- CI may start ephemeral PostgreSQL and Garage services. Do not change Docker Desktop, WSL, or shared workstation settings.
- Backups and CI artifacts must not include `.env` files, credentials, or personal portfolio payloads.

## Acceptance criteria

- [x] Restore into empty infrastructure verifies database/object checksums and links.
- [x] A sealed decision reconstructs with the same canonical hashes after restore.
- [x] Missing object/database mismatch fails the integrity gate explicitly.
- [x] Release gate requires all CI, E2E, scan, migration, and restore checks to finish.
- [x] Runbook states recovery assumptions, RPO/RTO targets, and secret handling.

## Required verification

```text
task test-backup-restore
task test-migration
task security-scan
task verify
```
