#!/usr/bin/env bash
set -Eeuo pipefail

echo "running repository secret and dependency checks"
if command -v gitleaks >/dev/null 2>&1; then
  gitleaks detect --no-banner --redact --source .
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
  (cd risk-engine && pip-audit --strict)
else
  echo "pip-audit is required for Python vulnerability scanning" >&2
  exit 2
fi
(cd risk-engine && uv run --locked ruff check src tests)
echo "dependency and secret checks passed"
