#!/usr/bin/env bash
set -Eeuo pipefail

echo "running repository secret and dependency checks"
if command -v gitleaks >/dev/null 2>&1; then
  gitleaks_report="$(mktemp)"
  if gitleaks detect --no-banner --redact --source . --report-format json --report-path "$gitleaks_report"; then
    rm -f "$gitleaks_report"
  else
    python3 - "$gitleaks_report" <<'PY'
import json
import sys
from pathlib import Path

try:
    findings = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
except (OSError, json.JSONDecodeError):
    findings = []
for finding in findings:
    print(
        "secret finding: "
        f"{finding.get('File', '<unknown>')}:"
        f"{finding.get('StartLine', '?')} "
        f"rule={finding.get('RuleID', '<unknown>')} "
        f"commit={finding.get('Commit', '<unknown>')}"
    )
PY
    rm -f "$gitleaks_report"
    exit 1
  fi
else
  if git grep -nI -E '-----BEGIN (RSA|OPENSSH|EC|DSA) PRIVATE KEY-----|AKIA[0-9A-Z]{16}' -- . ':!*.lock' ':!docs/**'; then
    echo "secret scan found a high-confidence credential pattern" >&2
    exit 1
  fi
  echo "gitleaks unavailable; high-confidence repository secret fallback passed"
fi

go vet ./apps/... ./internal/...
npm audit --audit-level=high
if command -v govulncheck >/dev/null 2>&1; then
  govulncheck ./...
else
  echo "govulncheck is required for Go vulnerability scanning" >&2
  exit 2
fi
if command -v pip-audit >/dev/null 2>&1; then
  audit_requirements="$(mktemp)"
  trap 'rm -f "$audit_requirements"' EXIT
  uv export --locked --project risk-engine --no-emit-project --all-extras --all-groups \
    --format requirements.txt --output-file "$audit_requirements"
  pip-audit --strict --requirement "$audit_requirements"
else
  echo "pip-audit is required for Python vulnerability scanning" >&2
  exit 2
fi
(cd risk-engine && uv run --locked ruff check src tests)
echo "dependency and secret checks passed"
