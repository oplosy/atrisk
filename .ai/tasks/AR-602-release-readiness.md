---
id: AR-602
title: Establish backup restore and release gate
status: draft
phase: 6
depends_on: [AR-601]
branch: task/AR-602-release-readiness
owned_paths: [scripts/backup/, docs/runbooks/, docs/releases/, infra/release/]
shared_paths: [.github/workflows/, Taskfile.yml, SECURITY.md]
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

## Out of scope

- Cloud deployment, paid backup provider, automatic production retention, or release publishing.

## Acceptance criteria

- [ ] Restore into empty infrastructure verifies database/object checksums and links.
- [ ] A sealed decision reconstructs with the same canonical hashes after restore.
- [ ] Missing object/database mismatch fails the integrity gate explicitly.
- [ ] Release gate requires all CI, E2E, scan, migration, and restore checks to finish.
- [ ] Runbook states recovery assumptions, RPO/RTO targets, and secret handling.

## Required verification

```text
task test-backup-restore
task test-migration
task security-scan
task verify
```
