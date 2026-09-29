# AR-602 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-602-release-readiness.md`
- Packet status at start: `active` (orchestrator readiness metadata)
- Referenced ADRs: ADR-001, ADR-005, ADR-017, ADR-018
- Owned paths: `scripts/backup/`, `docs/runbooks/`, `docs/releases/`, `infra/release/`
- Shared paths changed and justification: `.github/workflows/` and `Taskfile.yml`
  run the isolated restore, migration, scan, SBOM, and release gate in hosted CI;
  `risk-engine/pyproject.toml` and `risk-engine/uv.lock` pin the test-only pytest
  9.0.3 to clear PYSEC-2026-1845, as scoped by the packet. No runtime dependency changed.

## Result

`needs-review`

## Acceptance evidence

All evidence is from hosted CI run `36545431960` on `8e750be` (36 steps, 0 failed).

| Criterion | Evidence |
|---|---|
| Restore into empty infrastructure verifies database/object checksums and links | `restore-drill.sh` backs up PostgreSQL and Garage, restores into a freshly created `atrisk_restore` database and an empty `atrisk-ci-restore` bucket, re-verifies every object checksum, and checks every `raw_objects.object_key`/`content_sha256` against the restored archive. `restore.sh` runs every target guard (isolation, empty database, empty bucket, manifest) before its first write. |
| A sealed decision reconstructs with the same canonical hashes after restore | The drill reads the source `decision_evidence` id and `manifest_sha256`, then `reconstruct-evidence.go` reconstructs that same decision through the production evidence service against the restored PostgreSQL/Garage pair; the hashes must be identical. |
| Missing object/database mismatch fails the integrity gate explicitly | The drill requires a tampered dump and a removed object to fail `manifest.py verify`. Verify also rejects objects not listed in the manifest and a `sha256sums.txt` that differs from the manifest (checked locally with create/verify negative cases). |
| Release gate requires all CI, E2E, scan, migration, and restore checks to finish | `release-gate.sh` requires `passed` for CI, E2E, scan, migration, restore, and SBOM. CI derives each marker from the real `steps.<id>.outcome`, so a skipped or failed step blocks the gate. |
| Runbook states recovery assumptions, RPO/RTO targets, and secret handling | `docs/runbooks/BACKUP_RESTORE.md`: RPO 24 h, RTO 4 h, retention, encryption, secret-store handling, the full restore environment, required tools, and failure handling. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: none in hosted CI.
  PostgreSQL/Garage restore paths are verified only in CI; they were not run on the
  Windows workstation.

## Verification

| Command | Result |
|---|---|
| `task test-backup-restore` | pass (CI run `36545431960`) |
| `task test-migration` | pass (CI) |
| `task security-scan` | pass (CI): Gitleaks over full history, allowlist probes, `go vet`, `npm audit`, `govulncheck` v1.8.0, `pip-audit --strict`, `ruff` |
| `task sbom` | pass (CI): Syft v1.33.0 (digest-pinned) CycloneDX with a non-empty component list, uploaded as `atlasrisk-sbom` |
| `task verify` | pass (CI) |
| `task release-gate` | pass (CI), markers from step outcomes |
| `uv run --locked pytest` (risk-engine, pytest 9.0.3) | pass locally, 47 tests |
| `python scripts/backup/test-gitleaks-config.py` (Gitleaks v8.28.0) | pass locally |

## Change inventory

- Files changed: backup/restore scripts and manifest, evidence reconstruction helper,
  release scan/SBOM/gate scripts, Gitleaks config and probe test, runbook, release
  checklist, release infrastructure notes, Taskfile targets, CI workflow, risk-engine
  pytest pin and lockfile, this report.
- Schema/API changes: none.
- Generated artifacts: none committed; the CI SBOM is written to ignored `.task/release/`.

## Git state

- Branch: `task/AR-602-release-readiness`
- Commit SHA: verified implementation `8e750be`; this report is committed on top of it.
- Remote branch: pushed; PR #63 open, mergeable, required check `Verify` green.
- Worktree: clean after commit.

## Assumptions and risks

- Full-history Gitleaks surfaced a local single-node Garage `rpc_secret` committed in
  `c3e229b` (AR-004) and removed in `04b323f`. It is allowlisted only for that commit
  and path, as approved by the owner; the value remains in `main` history. Rotation
  is unnecessary for the disposable local stack but should be tracked if that value
  was ever reused.
- Two probe fixtures (`golden-risk-002`) from this task's own history (`0f22a07`,
  `2c4b054`) are allowlisted by exact commit, path, and value.
- `pg_dump` still receives `PG_DSN` on its command line; the CI DSN is a test credential.
- Backup encryption is provider/secret-store guidance; no accepted ADR defines a
  key-management provider.
