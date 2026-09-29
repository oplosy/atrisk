# Backup and restore runbook

## Recovery contract

AtlasRisk backs up two immutable stores together: a PostgreSQL custom-format
dump and every object in the Garage bucket. `scripts/backup/backup.sh` writes
both to a newly created directory, then writes `manifest.json` and
`sha256sums.txt`. The manifest is the integrity boundary; a missing or changed
database dump or object fails closed.

The V1 operating targets are RPO 24 hours and RTO 4 hours. These are planning
targets, not a production SLA. A restore requires a fresh PostgreSQL database
and a bucket different from the source bucket. `restore.sh` refuses a
non-isolated target, same database identifier, or same bucket and never uses
`pg_restore --clean`.

## Scheduled backup

Provide the following values from the deployment secret store, never from a
tracked `.env` file:

```text
PG_DSN
ATLASRISK_S3_ENDPOINT
ATLASRISK_S3_REGION
ATLASRISK_S3_BUCKET
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY
BACKUP_DIR=<new, private directory>
```

Run `bash scripts/backup/backup.sh`. Retain at least seven daily manifests and
four weekly manifests according to the operator's storage policy. Retention is
an operator action in V1; AtlasRisk does not delete production backups
automatically.

## Restore drill

Provision an empty database and bucket with distinct names, export
`RESTORE_PG_DSN`, `RESTORE_S3_ENDPOINT`, `RESTORE_S3_BUCKET`,
`RESTORE_S3_REGION`, `SOURCE_PG_DSN`, `ATLASRISK_ADMIN_DSN`,
`SOURCE_DATABASE_NAME`, `RESTORE_DATABASE_NAME`, and
`ATLASRISK_RESTORE_TARGET=isolated`, then run
`bash scripts/backup/restore.sh`.

For CI, `task test-backup-restore` runs `restore-drill.sh` against ephemeral
PostgreSQL and Garage. It refuses to drop an existing restore database, uses a
fresh task-specific temporary backup directory, and preserves that backup on
failure for diagnosis. The drill checks a restored database marker, every
`raw_objects.object_key`/`content_sha256` link against the restored object,
and both database-tampering and missing-object negative cases. It reconstructs
a sealed decision's canonical evidence against the restored PostgreSQL/Garage
pair and requires that hash to equal the source hash.

Never point a restore at a user installation, the source database, the source
bucket, or a path containing credentials. Backup directories and CI artifacts
must not contain `.env` files, access keys, personal portfolio exports, or raw
production payloads. Encrypt backup storage and transport with the deployment
provider's envelope encryption/TLS; keep the encryption key outside the backup
directory and rotate it through the secret store. The scripts deliberately do
not accept a key on the command line.

## Failure handling

Stop on any manifest, database, or object mismatch. Preserve the original
backup and logs, provision a new isolated target, and repeat the verification.
Do not repair a backup in place and do not delete the source to make a restore
pass.
