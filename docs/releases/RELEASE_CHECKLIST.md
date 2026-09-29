# AtlasRisk release checklist

Release candidates are review artifacts only; this checklist does not publish
or deploy a release.

- [ ] CI completed successfully on the candidate commit.
- [ ] AR-601 end-to-end journey completed, including sealed decision evidence.
- [ ] `task test-backup-restore` restored into empty PostgreSQL and Garage.
- [ ] Database/object checksum and `raw_objects` links matched after restore.
- [ ] Corrupt database and missing-object negative checks failed explicitly.
- [ ] `task test-migration` upgraded the previous schema without losing the
      sentinel row.
- [ ] `task security-scan` found no high-severity dependency or secret result.
- [ ] `task sbom` produced a CycloneDX SBOM from locked dependencies.
- [ ] RPO/RTO, retention, encryption, and secret handling were reviewed in
      `docs/runbooks/BACKUP_RESTORE.md`.
- [ ] No `.env`, credentials, personal portfolio data, or production payloads
      are present in the candidate or CI artifacts.

The release gate is complete only when every item is recorded as passed. A
failed or missing check blocks the candidate; it is not converted to a warning.
