#!/usr/bin/env bash
set -Eeuo pipefail

: "${PG_DSN:?PG_DSN is required}"
: "${ATLASRISK_S3_ENDPOINT:?ATLASRISK_S3_ENDPOINT is required}"
: "${ATLASRISK_S3_REGION:?ATLASRISK_S3_REGION is required}"
: "${ATLASRISK_S3_BUCKET:?ATLASRISK_S3_BUCKET is required}"
: "${RESTORE_PG_DSN:?RESTORE_PG_DSN is required}"
: "${RESTORE_S3_BUCKET:?RESTORE_S3_BUCKET is required}"
: "${ATLASRISK_ADMIN_DSN:?ATLASRISK_ADMIN_DSN is required}"
: "${ATLASRISK_RESTORE_TARGET:=isolated}"
: "${BACKUP_DIR:?BACKUP_DIR is required}"

command -v psql >/dev/null || { echo "psql is required" >&2; exit 2; }
command -v realpath >/dev/null || { echo "realpath is required" >&2; exit 2; }
temp_parent="$(realpath -m "${RUNNER_TEMP:-${TMPDIR:-/tmp}}")"
drill_dir="$(realpath -m "$BACKUP_DIR")"
case "$drill_dir" in
  "$temp_parent"/atlasrisk-ar602-*) ;;
  *) echo "refusing backup path outside an isolated AR-602 temp child: $drill_dir" >&2; exit 2 ;;
esac
if [[ -e "$drill_dir" && -n "$(find "$drill_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
  echo "refusing non-empty AR-602 drill directory" >&2
  exit 2
fi
trap 'rm -rf -- "$drill_dir"' EXIT

source_identity="$(psql "$PG_DSN" -At -F '|' -c "SELECT current_database(), COALESCE(inet_server_addr()::text, '')")"
[[ "$source_identity" == "atrisk_test|127.0.0.1" ]] || {
  echo "refusing restore drill: source must be the isolated loopback atrisk_test database" >&2
  exit 2
}
admin_identity="$(psql "$ATLASRISK_ADMIN_DSN" -At -F '|' -c "SELECT current_database(), COALESCE(inet_server_addr()::text, '')")"
[[ "$admin_identity" == "postgres|127.0.0.1" ]] || {
  echo "refusing restore drill: admin must target loopback postgres" >&2
  exit 2
}

psql "$PG_DSN" -v ON_ERROR_STOP=1 -c \
  "CREATE TABLE IF NOT EXISTS release_backup_probe (id integer PRIMARY KEY, marker text NOT NULL); INSERT INTO release_backup_probe VALUES (1, 'ar602-fixture') ON CONFLICT (id) DO UPDATE SET marker = EXCLUDED.marker;" >/dev/null
printf 'atlasrisk AR-602 immutable archive fixture\n' | aws s3 cp - "s3://$ATLASRISK_S3_BUCKET/ar602/probe.txt" \
  --endpoint-url "$ATLASRISK_S3_ENDPOINT" --no-progress

bash "$(dirname "$0")/backup.sh"
if bash "$(dirname "$0")/backup.sh"; then
  echo "backup rerun safety test unexpectedly allowed a non-empty target" >&2
  exit 1
fi

psql "$ATLASRISK_ADMIN_DSN" -v ON_ERROR_STOP=1 -c \
  "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid();" >/dev/null || true

: "${RESTORE_DATABASE_NAME:?RESTORE_DATABASE_NAME is required}"
[[ "$RESTORE_DATABASE_NAME" =~ ^[a-zA-Z_][a-zA-Z0-9_]{0,62}$ ]] || {
  echo "refusing unsafe restore database identifier" >&2
  exit 2
}
if [[ "$RESTORE_DATABASE_NAME" != "atrisk_restore" ]]; then
  echo "refusing restore drill: target must be the fixed test database atrisk_restore" >&2
  exit 2
fi
[[ "$RESTORE_S3_BUCKET" == "atrisk-ci-restore" ]] || {
  echo "refusing restore drill: target must be the fixed test bucket atrisk-ci-restore" >&2
  exit 2
}
if aws s3api head-bucket --bucket "$RESTORE_S3_BUCKET" --endpoint-url "$RESTORE_S3_ENDPOINT" >/dev/null 2>&1; then
  target_object_count="$(aws s3api list-objects-v2 --bucket "$RESTORE_S3_BUCKET" --endpoint-url "$RESTORE_S3_ENDPOINT" --query 'KeyCount' --output text)"
  [[ "$target_object_count" == "0" ]] || { echo "refusing restore drill into non-empty target bucket" >&2; exit 2; }
else
  aws s3api create-bucket --bucket "$RESTORE_S3_BUCKET" --endpoint-url "$RESTORE_S3_ENDPOINT" >/dev/null
fi
psql "$ATLASRISK_ADMIN_DSN" -v ON_ERROR_STOP=1 -v target_db="$RESTORE_DATABASE_NAME" -c \
  "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = :'target_db';" >/dev/null || true
drop_statement="$(psql "$ATLASRISK_ADMIN_DSN" -At -v target_db="$RESTORE_DATABASE_NAME" -c "SELECT format('DROP DATABASE IF EXISTS %I', :'target_db')")"
if [[ -n "$drop_statement" ]]; then psql "$ATLASRISK_ADMIN_DSN" -v ON_ERROR_STOP=1 -c "$drop_statement" >/dev/null; fi
psql "$ATLASRISK_ADMIN_DSN" -v ON_ERROR_STOP=1 -c "CREATE DATABASE \"$RESTORE_DATABASE_NAME\"" >/dev/null

bash "$(dirname "$0")/restore.sh"
marker="$(psql "$RESTORE_PG_DSN" -At -c "SELECT marker FROM release_backup_probe WHERE id = 1")"
[[ "$marker" == "ar602-fixture" ]] || { echo "restore probe row mismatch" >&2; exit 1; }

source_evidence_hash="$(psql "$PG_DSN" -At -c "SELECT btrim(manifest_sha256) FROM decision_evidence ORDER BY decision_id LIMIT 1")"
[[ "$source_evidence_hash" =~ ^[0-9a-f]{64}$ ]] || {
  echo "restore drill requires one sealed decision from the preceding E2E journey" >&2
  exit 1
}
restored_evidence_hash="$(go run ./scripts/backup/reconstruct-evidence.go)"
[[ "$restored_evidence_hash" == "$source_evidence_hash" ]] || {
  echo "sealed evidence hash changed across restore" >&2
  exit 1
}

restored_objects="$(mktemp -d)"
trap 'rm -rf "$drill_dir" "$restored_objects" "$drill_dir.corrupt" "$drill_dir.missing"' EXIT
aws s3 sync "s3://$RESTORE_S3_BUCKET" "$restored_objects" \
  --endpoint-url "$RESTORE_S3_ENDPOINT" --no-progress
while IFS=$'\t' read -r key hash; do
  [[ -n "$key" ]] || continue
  object="$restored_objects/$key"
  [[ -f "$object" ]] || { echo "database/archive link missing: $key" >&2; exit 1; }
  actual="$(sha256sum "$object" | awk '{print $1}')"
  [[ "$actual" == "$hash" ]] || { echo "database/archive link checksum mismatch: $key" >&2; exit 1; }
done < <(psql "$RESTORE_PG_DSN" -At -F $'\t' -c "SELECT object_key, btrim(content_sha256) FROM raw_objects ORDER BY object_key")

cp -R "$drill_dir" "$drill_dir.corrupt"
printf 'tamper' >> "$drill_dir.corrupt/database.dump"
if python3 "$(dirname "$0")/manifest.py" verify "$drill_dir.corrupt"; then
  echo "integrity negative test unexpectedly passed" >&2
  exit 1
fi
echo "restore drill passed: isolated database/object restore and mismatch rejection"
