"""Prove the release allowlist permits only the exact non-secret fixtures."""

import json
import os
import subprocess
import tempfile
from pathlib import Path


config = Path(__file__).with_name("gitleaks.toml").resolve()
scanner = os.environ.get("GITLEAKS_BIN", "gitleaks")
fixture_value = "golden-risk-001"
fixture_paths = (
    "internal/jobs/queue_test.go",
    "risk-engine/tests/test_jobs.py",
    "test/fixtures/risk/golden-job.json",
)

with tempfile.TemporaryDirectory(prefix="atlasrisk-gitleaks-") as temporary:
    root = Path(temporary)
    subprocess.run(["git", "init", "--quiet", str(root)], check=True)

    def scan(files, expected_files):
        for name, content in files.items():
            path = root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(json.dumps(content) + "\n", encoding="utf-8")
        subprocess.run(["git", "-C", str(root), "add", "--", *files], check=True)
        subprocess.run(
            ["git", "-C", str(root), "-c", "user.name=AR602 Test",
             "-c", "user.email=ar602@example.invalid", "commit", "--quiet", "-m", "probe"],
            check=True,
        )
        report = root / "report.json"
        result = subprocess.run(
            [scanner, "detect", "--no-banner", "--redact", "--source", str(root),
             "--config", str(config), "--log-opts=-1", "--report-format", "json",
             "--report-path", str(report)],
            capture_output=True, text=True,
        )
        expected_code = 1 if expected_files else 0
        if result.returncode != expected_code:
            raise SystemExit(f"Gitleaks probe exit {result.returncode}; expected {expected_code}")
        findings = json.loads(report.read_text(encoding="utf-8"))
        actual = [(item["File"], item["RuleID"]) for item in findings]
        expected = [(name, "generic-api-key") for name in expected_files]
        if sorted(actual) != sorted(expected):
            raise SystemExit(f"Gitleaks allowlist regression: {actual!r}; expected {expected!r}")

    scan({name: {"idempotency_key": fixture_value} for name in fixture_paths}, [])
    # Construct a synthetic key at runtime so this test source is not a credential fixture.
    other_value = "0123456789" + "abcdef" * 3
    scan({fixture_paths[0]: {"idempotency_key": fixture_value, "api_key": other_value}}, [fixture_paths[0]])
    scan({"outside.json": {"idempotency_key": fixture_value}}, ["outside.json"])

print("Gitleaks allowlist probes passed: exact fixtures, same-line secret, outside path")
