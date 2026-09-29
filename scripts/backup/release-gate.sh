#!/usr/bin/env bash
set -Eeuo pipefail

required=("CI" "E2E" "SCAN" "MIGRATION" "RESTORE" "SBOM")
for name in "${required[@]}"; do
  var="ATLASRISK_GATE_${name}"
  value="${!var:-}"
  if [[ "$value" != "passed" ]]; then
    echo "release gate blocked: $var must be passed (got ${value:-missing})" >&2
    exit 1
  fi
done
echo "release gate passed: CI, E2E, scan, migration, restore, and SBOM checks completed"
