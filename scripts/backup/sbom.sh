#!/usr/bin/env bash
set -Eeuo pipefail

: "${SBOM_OUTPUT:?SBOM_OUTPUT is required}"
mkdir -p "$(dirname "$SBOM_OUTPUT")"
if [[ "${ATLASRISK_SBOM_PREBUILT:-0}" == "1" ]]; then
  test -s "$SBOM_OUTPUT" || { echo "prebuilt SBOM is missing" >&2; exit 1; }
  echo "pinned CI SBOM present at $SBOM_OUTPUT"
  exit 0
fi
if command -v syft >/dev/null 2>&1; then
  syft dir:. --output cyclonedx-json="$SBOM_OUTPUT"
else
  python3 - "$SBOM_OUTPUT" <<'PY'
import json
import pathlib
import sys

out = pathlib.Path(sys.argv[1])
components = []
for lock, ecosystem in (("go.mod", "golang"), ("package-lock.json", "npm"), ("risk-engine/uv.lock", "pypi")):
    path = pathlib.Path(lock)
    if path.exists():
        components.append({"bom-ref": lock, "name": path.name, "type": "file", "version": "locked", "properties": [{"name": "ecosystem", "value": ecosystem}]})
payload = {"bomFormat": "CycloneDX", "specVersion": "1.5", "version": 1, "metadata": {"component": {"type": "application", "name": "atlasrisk"}}, "components": components}
out.write_text(json.dumps(payload, indent=2, sort_keys=True) + "\n", encoding="utf-8")
PY
  echo "syft unavailable; generated lockfile inventory SBOM at $SBOM_OUTPUT"
fi
test -s "$SBOM_OUTPUT"
