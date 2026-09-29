# AtlasRisk release checklist

Release candidates are review artifacts only; this checklist does not publish
or deploy a release.

## Candidate

- Version: V1 (proposed tag `v1.0.0`)
- Candidate commit: `0b767d4716b9d3a92651242ec12c8e331c2cd9c4` (`main`)
- Evidence run: GitHub Actions CI run `36600398081` on the candidate commit
  (job `Verify`, 38 steps, 0 failed)
- Scope: all 31 V1 task packets are `merged`; the deferred roadmap in
  `docs/plans/MASTER_PLAN.md` is out of scope.

## Checks

- [x] CI completed successfully on the candidate commit.
      Run `36600398081`, conclusion `success`.
- [x] AR-601 end-to-end journey completed, including sealed decision evidence.
      Step "Run complete end-to-end journey" passed.
- [x] `task test-backup-restore` restored into empty PostgreSQL and Garage.
      Step "Run isolated backup and restore drill" passed: fresh `atrisk_restore`
      database and empty `atrisk-ci-restore` bucket; the restored sealed decision
      reconstructed with the same canonical hash as the source.
- [x] Database/object checksum and `raw_objects` links matched after restore.
      Verified inside the same drill against every restored object.
- [x] Corrupt database and missing-object negative checks failed explicitly.
      The drill requires both a tampered dump and a removed object to fail
      manifest verification.
- [x] `task test-migration` upgraded the previous schema without losing the
      sentinel row.
      Step "Run migration-from-previous verification" passed.
- [x] `task security-scan` found no high-severity dependency or secret result.
      Step "Run secret and dependency scan" passed: Gitleaks over full history,
      `npm audit --audit-level=high`, `govulncheck`, `pip-audit --strict`.
- [x] `task sbom` produced a CycloneDX SBOM with a non-empty component list.
      Artifact `atlasrisk-sbom`: CycloneDX 1.6, 110 components (Go, PyPI, npm
      runtime). npm lists only runtime packages; Syft excludes dev dependencies
      by default.
- [x] RPO/RTO, retention, encryption, and secret handling were reviewed in
      `docs/runbooks/BACKUP_RESTORE.md`.
      The runbook states RPO 24 h, RTO 4 h, seven daily and four weekly
      retention, provider envelope encryption with keys outside the backup, and
      secret-store handling.
- [x] No `.env`, credentials, personal portfolio data, or production payloads
      are present in the candidate or CI artifacts.
      The only tracked env file is `.env.example` with local-only placeholder
      values; the only CI artifact on the candidate run is the SBOM.

## Accepted risks

- A removed local single-node Garage `rpc_secret` remains in `main` history
  (`c3e229b`); the full-history scan allowlists that exact commit and path.
- `pg_dump` receives its DSN on the command line; CI uses test credentials only.
- Backup encryption is provider guidance; no ADR selects a key-management
  provider.

## Sign-off

The release gate is complete only when every item is recorded as passed. A
failed or missing check blocks the candidate; it is not converted to a warning.

- Release owner: ______________________
- Date (UTC): ______________________
- Decision: approve / reject
