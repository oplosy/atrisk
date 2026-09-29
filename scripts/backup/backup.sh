#!/usr/bin/env bash
set -Eeuo pipefail

: "${PG_DSN:?PG_DSN is required}"
: "${ATLASRISK_S3_ENDPOINT:?ATLASRISK_S3_ENDPOINT is required}"
: "${ATLASRISK_S3_REGION:?ATLASRISK_S3_REGION is required}"
: "${ATLASRISK_S3_BUCKET:?ATLASRISK_S3_BUCKET is required}"
: "${BACKUP_DIR:?BACKUP_DIR is required}"

command -v pg_dump >/dev/null || { echo "pg_dump is required" >&2; exit 2; }
command -v aws >/dev/null || { echo "aws CLI is required" >&2; exit 2; }
command -v python3 >/dev/null || { echo "python3 is required" >&2; exit 2; }
expected_major="${ATLASRISK_POSTGRES_MAJOR:-18}"
dump_major="$(pg_dump --version | awk '{print $3}' | cut -d. -f1)"
[[ "$dump_major" == "$expected_major" ]] || { echo "pg_dump major $dump_major does not match PostgreSQL $expected_major" >&2; exit 2; }

case "$BACKUP_DIR" in
  ""|.|..|/|/tmp|"$HOME") echo "refusing unsafe backup directory" >&2; exit 2 ;;
esac
if [[ -e "$BACKUP_DIR" && -n "$(find "$BACKUP_DIR" -mindepth 1 -maxdepth 1 -print -quit)" ]]; then
  echo "refusing non-empty backup directory; choose a fresh isolated path" >&2
  exit 2
fi
mkdir -p "$BACKUP_DIR/objects"

pg_dump --format=custom --no-owner --no-acl --file "$BACKUP_DIR/database.dump" "$PG_DSN"
aws s3 sync "s3://$ATLASRISK_S3_BUCKET" "$BACKUP_DIR/objects" \
  --endpoint-url "$ATLASRISK_S3_ENDPOINT" --region "$ATLASRISK_S3_REGION" --no-progress
python3 "$(dirname "$0")/manifest.py" create "$BACKUP_DIR"
python3 "$(dirname "$0")/manifest.py" verify "$BACKUP_DIR"
echo "backup created: $BACKUP_DIR"
