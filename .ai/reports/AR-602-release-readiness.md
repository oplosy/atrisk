# AR-602 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-602-release-readiness.md`
- Packet status at start: `active` (orchestrator readiness metadata)
- Referenced ADRs: ADR-001, ADR-005, ADR-017, ADR-018
- Owned paths: `scripts/backup/`, `docs/runbooks/`, `docs/releases/`, `infra/release/`
- Shared paths changed and justification: `.github/workflows/` and `Taskfile.yml`; required to run the isolated restore, migration, scan, SBOM, and fail-closed release gate in hosted CI.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| Empty restore and checksums | `scripts/backup/backup.sh`, `restore.sh`, `manifest.py`, and `restore-drill.sh` dump PostgreSQL, sync Garage objects, verify SHA-256, restore into a distinct empty database/bucket, and verify `raw_objects` links. |
| Sealed evidence continuity | `restore-drill.sh` captures the source `decision_evidence.manifest_sha256`, then `reconstruct-evidence.go` calls the production evidence service against the restored PostgreSQL/Garage pair; the drill fails unless the reconstructed canonical hash is identical. |
| Negative integrity cases | `restore-drill.sh` expects both a tampered database dump and a removed object to fail manifest verification. |
| Release gate | `Taskfile.yml`, `.github/workflows/ci.yml`, `scripts/backup/release-gate.sh`, and `docs/releases/RELEASE_CHECKLIST.md` require CI, E2E, scan, migration, restore, and SBOM markers. |
| Recovery operations | `docs/runbooks/BACKUP_RESTORE.md` defines RPO 24 hours, RTO 4 hours, retention, encryption, secret handling, isolated targets, and failure handling. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: local Windows host lacks Bash, PostgreSQL client, AWS CLI, Task, and Python; live restore/migration/scan/SBOM verification is intentionally delegated to hosted CI ephemeral services. No Docker Desktop, WSL, or shared workstation setting was changed.

## Verification

| Command | Result |
|---|---|
| `git diff --check` | pass |
| `bash -n scripts/backup/*.sh` | unavailable: Bash is not installed/launchable on this Windows host |
| `python3 scripts/backup/manifest.py --help` | unavailable: Python runtime is not installed on this Windows host |
| `task test-backup-restore` | pending hosted CI; local Task/PostgreSQL/Garage/AWS CLI unavailable |
| `task test-migration` | pending hosted CI; local Task/PostgreSQL unavailable |
| `task security-scan` | pending hosted CI; pinned scan tools are installed in workflow |
| `task sbom` | pending hosted CI; pinned Syft container is used in workflow |
| `task verify` | pending orchestrator/hosted CI |

## Change inventory

- Files changed: backup/restore scripts and manifest, release scan/SBOM/gate scripts, runbook, release checklist, release infrastructure notes, Taskfile targets, CI workflow, this report.
- Schema/API changes: none.
- Generated artifacts: none committed; CI SBOM is written to ignored `.task/release/`.

## Git state

- Branch: `task/AR-602-release-readiness`
- Commit SHA: `9683058` (implementation commit; report metadata is finalized in the follow-up handoff commit)
- Remote branch: pushed by this worker after verification; no PR opened
- Worktree: the orchestrator's pre-existing `.ai/tasks/AR-602-release-readiness.md` metadata edit remains outside this worker commit; implementation files are clean after commit.

## Assumptions and risks

- Hosted CI provides the existing PostgreSQL and Garage services, AWS CLI, and Docker runtime; the workflow installs PostgreSQL client, `pip-audit`, and `govulncheck` and uses a pinned Syft image.
- The restore drill is intentionally limited to the loopback `atrisk_test` source and a fresh `atrisk_restore` target.
- Backup encryption is provider/secret-store guidance rather than a new encryption implementation, because no accepted ADR defines a key-management provider.
