# Release verification

`infra/release` contains release-readiness policy, not deployment manifests.
The release candidate must use the ephemeral PostgreSQL and Garage services in
CI. Production credentials, buckets, and user data are never used by the
restore drill.

`scripts/backup/release-gate.sh` is the final fail-closed gate. It requires
explicit `passed` markers for CI, E2E, secret/dependency scanning, migration,
restore, and SBOM generation. Missing markers or findings stop the job.

SBOM output is written to the ignored `.task/` directory in CI. The preferred
generator is Syft in CycloneDX JSON format; the checked-in fallback records the
locked Go, npm, and Python dependency inventories without copying source,
credentials, or runtime payloads.
