# AR-701 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-701-release-packaging.md`
- Packet status at start: `active` (created with the task at the repository owner's request)
- Referenced ADRs: ADR-001, ADR-003, ADR-008, ADR-017, ADR-018
- Owned paths: `infra/images/`, `scripts/release/`, `risk-engine/src/atlasrisk/jobs/runner.py`,
  `risk-engine/tests/test_runner.py`, `docs/releases/`
- Shared paths changed and justification: `apps/api/cmd/api/` (the `migrate`
  subcommand); `internal/platform/database/migrate.go` and its new test
  (`ApplyMigrations` for installation databases, with the isolated-test guard
  moved to the front of `Migrate` and `MigrateInSchema`); `risk-engine/pyproject.toml`,
  `risk-engine/uv.lock`, `risk-engine/README.md` (the `worker` extra with the
  existing `psycopg[binary]==3.3.6` pin and the `atlasrisk-risk-worker` script);
  `.github/workflows/ci.yml` (the `Images` job) and `.github/workflows/release.yml`.

## Result

`complete`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| `migrate` exits 2 without a URL; applies all migrations; a second run is a no-op | `TestMigrateRequiresDatabaseURL`, `TestMigrateRejectsUnexpectedArguments`, `TestMigrateReportsDatabaseFailure`; CI `Images` runs `migrate` twice against PostgreSQL and requires `no migrations to run` the second time. |
| `database.Migrate` still refuses non-isolated targets | `TestMigrateStillRefusesNonIsolatedTargets` (both `Migrate` and `MigrateInSchema`); `TestApplyMigrationsAcceptsReleaseTargets` shows only the release path skips the guard. |
| Worker processes jobs, waits when empty, reconnects, exits 0 on SIGTERM | `tests/test_runner.py` (18 tests: drain-then-wait, reconnect after connect and loop failures, no payload or password in logs, signal handling, invalid settings); CI `Images` starts the worker, then `docker stop` must exit 0 and log `worker stopped`. |
| Both images run non-root and pin base images by digest | `infra/images/*.Dockerfile` (users 65532 and 10001; every `FROM` has `@sha256`); CI `Images` fails on a root user. |
| CI builds both images and proves migrate and worker start | `Images` job: builds, then `scripts/release/smoke-images.sh` with read-only root FS, `--cap-drop ALL`, `no-new-privileges`, and an API `GET /api/v1/instruments` returning 200. |
| A release tag publishes images, bundle, checksum, digests; non-main tags fail | `.github/workflows/release.yml` and `scripts/release/publish-images.sh` (tag format check, `merge-base --is-ancestor` check, digest from `docker push`). Positive path proven by tag `v1.0.1` (Release run `37668160469`: both images, web bundle, checksum, `images.txt`). The non-main rejection was not exercised. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: the release
  workflow runs only on a tag, so it is unverified until the first release.

## Verification

| Command | Result |
|---|---|
| `go test ./apps/api/cmd/api/ ./internal/platform/database/` | pass |
| `uv run --project risk-engine --locked pytest` | pass, 65 tests (18 new) |
| `ruff format --check`, `ruff check` | pass |
| CI `Images` | pass |
| CI `Verify` (`task security-scan`, `task verify`, release gate) | required PR checks on the head commit |

## Change inventory

- Files changed: see the PR diff; no generated files.
- Schema/API changes: none (new CLI entry points only).
- Generated artifacts: none.

## Git state

- Branch: `task/AR-701-release-packaging`
- Remote branch: pushed; PR #69.
- Worktree: clean.

## Assumptions and risks

- GHCR packages are private on first publish; `docs/releases/PUBLISHING.md` covers the visibility decision.
- The collector remains a skeleton and has no image.
- A job whose result cannot be persisted stays `running` after its lease expires,
  because claims select only `queued` and `retryable_failed`. Existing queue
  behavior, unchanged here.
