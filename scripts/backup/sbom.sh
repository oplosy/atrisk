#!/usr/bin/env bash
set -Eeuo pipefail

: "${SBOM_OUTPUT:?SBOM_OUTPUT is required}"
mkdir -p "$(dirname "$SBOM_OUTPUT")"
if [[ "${ATLASRISK_SBOM_PREBUILT:-0}" != "1" ]]; then
  command -v syft >/dev/null 2>&1 || { echo "syft is required to generate the SBOM" >&2; exit 2; }
  syft dir:. --output cyclonedx-json="$SBOM_OUTPUT"
fi
test -s "$SBOM_OUTPUT" || { echo "SBOM is missing or empty: $SBOM_OUTPUT" >&2; exit 1; }
python3 - "$SBOM_OUTPUT" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as source:
    bom = json.load(source)
if bom.get("bomFormat") != "CycloneDX":
    raise SystemExit("SBOM is not CycloneDX")
components = bom.get("components")
if not isinstance(components, list) or not components:
    raise SystemExit("SBOM has no components")
print(f"SBOM validated: {len(components)} component(s)")
PY
