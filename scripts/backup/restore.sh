#!/usr/bin/env bash
set -Eeuo pipefail

: "${BACKUP_DIR:?BACKUP_DIR is required}"
: "${SOURCE_PG_DSN:?SOURCE_PG_DSN is required}"
: "${RESTORE_PG_DSN:?RESTORE_PG_DSN is required}"
: "${RESTORE_S3_ENDPOINT:?RESTORE_S3_ENDPOINT is required}"
: "${RESTORE_S3_REGION:?RESTORE_S3_REGION is required}"
: "${RESTORE_S3_BUCKET:?RESTORE_S3_BUCKET is required}"
: "${ATLASRISK_S3_BUCKET:?ATLASRISK_S3_BUCKET is required (source bucket)}"
: "${ATLASRISK_RESTORE_TARGET:?ATLASRISK_RESTORE_TARGET=isolated is required}"
: "${SOURCE_DATABASE_NAME:?SOURCE_DATABASE_NAME is required}"
: "${RESTORE_DATABASE_NAME:?RESTORE_DATABASE_NAME is required}"

if [[ "$ATLASRISK_RESTORE_TARGET" != "isolated" ]]; then
  echo "refusing restore: ATLASRISK_RESTORE_TARGET must be isolated" >&2
  exit 2
fi
if [[ "$RESTORE_S3_BUCKET" == "$ATLASRISK_S3_BUCKET" ]]; then
  echo "refusing restore: target bucket must differ from source bucket" >&2
  exit 2
fi
for database_name in "$SOURCE_DATABASE_NAME" "$RESTORE_DATABASE_NAME"; do
  [[ "$database_name" =~ ^[a-z_][a-z0-9_]{0,62}$ ]] || { echo "refusing unsafe database identifier" >&2; exit 2; }
done
if [[ "$RESTORE_DATABASE_NAME" == "$SOURCE_DATABASE_NAME" ]]; then
  echo "refusing restore: target database must differ from source database" >&2
  exit 2
fi
[[ "$ATLASRISK_S3_BUCKET" =~ ^[a-z0-9][a-z0-9.-]{2,62}$ ]] || { echo "unsafe source bucket" >&2; exit 2; }
[[ "$RESTORE_S3_BUCKET" =~ ^[a-z0-9][a-z0-9.-]{2,62}$ ]] || { echo "unsafe target bucket" >&2; exit 2; }
command -v pg_restore >/dev/null || { echo "pg_restore is required" >&2; exit 2; }
command -v aws >/dev/null || { echo "aws CLI is required" >&2; exit 2; }
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 2; }
expected_major="${ATLASRISK_POSTGRES_MAJOR:-18}"
restore_major="$(pg_restore --version | awk '{print $3}' | cut -d. -f1)"
[[ "$restore_major" == "$expected_major" ]] || { echo "pg_restore major $restore_major does not match PostgreSQL $expected_major" >&2; exit 2; }

source_identity="$(psql "$SOURCE_PG_DSN" -At -F '|' -c "SELECT current_database(), COALESCE(inet_server_addr()::text, ''), inet_server_port()")"
target_identity="$(psql "$RESTORE_PG_DSN" -At -F '|' -c "SELECT current_database(), COALESCE(inet_server_addr()::text, ''), inet_server_port()")"
[[ "$source_identity" != "$target_identity" ]] || { echo "refusing restore: source and target PostgreSQL endpoints are identical" >&2; exit 2; }

target_database="$(psql "$RESTORE_PG_DSN" -At -c "SELECT current_database()")"
[[ "$target_database" == "$RESTORE_DATABASE_NAME" ]] || {
  echo "refusing restore: RESTORE_PG_DSN does not target RESTORE_DATABASE_NAME" >&2
  exit 2
}
existing_tables="$(psql "$RESTORE_PG_DSN" -At -c "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.relkind IN ('r','p','v','m','f') AND n.nspname NOT IN ('pg_catalog','information_schema')")"
[[ "$existing_tables" == "0" ]] || {
  echo "refusing restore: target database is not empty ($existing_tables user relation(s))" >&2
  exit 2
}

python3 "$(dirname "$0")/manifest.py" verify "$BACKUP_DIR"

# pg_restore targets an explicitly provisioned empty database. It never drops
# the source database and never uses --clean, which protects user data.
pg_restore --exit-on-error --no-owner --no-acl --dbname "$RESTORE_PG_DSN" "$BACKUP_DIR/database.dump"

if aws s3api head-bucket --bucket "$RESTORE_S3_BUCKET" --endpoint-url "$RESTORE_S3_ENDPOINT" >/dev/null 2>&1; then
  target_object_count="$(aws s3api list-objects-v2 --bucket "$RESTORE_S3_BUCKET" --endpoint-url "$RESTORE_S3_ENDPOINT" --query 'KeyCount' --output text)"
  [[ "$target_object_count" == "0" ]] || { echo "refusing restore into non-empty target bucket" >&2; exit 2; }
else
  aws s3api create-bucket --bucket "$RESTORE_S3_BUCKET" --endpoint-url "$RESTORE_S3_ENDPOINT" >/dev/null
fi
aws s3 sync "$BACKUP_DIR/objects" "s3://$RESTORE_S3_BUCKET" \
  --endpoint-url "$RESTORE_S3_ENDPOINT" --no-progress

tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
aws s3 sync "s3://$RESTORE_S3_BUCKET" "$tmp_dir" \
  --endpoint-url "$RESTORE_S3_ENDPOINT" --no-progress

while IFS= read -r line; do
  hash="${line%%  *}"
  file="${line#*  }"
  [[ "$file" == "database.dump" ]] && continue
  restored="$tmp_dir/${file#objects/}"
  [[ -f "$restored" ]] || { echo "integrity failure: missing restored object $file" >&2; exit 1; }
  actual="$(sha256sum "$restored" | awk '{print $1}')"
  [[ "$actual" == "$hash" ]] || { echo "integrity failure: restored checksum mismatch $file" >&2; exit 1; }
done < "$BACKUP_DIR/sha256sums.txt"
echo "restore integrity passed: database and object archive"
