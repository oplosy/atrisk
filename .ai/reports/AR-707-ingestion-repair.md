# AR-707 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-707-ingestion-repair.md`
- Packet status at start: `ready`
- Referenced ADRs: `docs/decisions/README.md` (ADR register; referenced durable ingestion decisions)
- Owned paths: `apps/collector/`, `internal/collector/`, `internal/ingestion/`, `internal/sources/`, `.ai/reports/AR-707-ingestion-repair.md`
- Shared paths changed and justification: `db/migrations/00013_durable_collector_schedules.sql`, core queries and generated SQLC outputs, collector image/release CI and smoke scripts, architecture/release documentation, and the packet-listed integration tests.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| Durable scheduler and fencing | Migration 13 adds PostgreSQL schedules, leases, token fencing, persisted lease/max-attempt policy, retry availability, stable due occurrence identity, and SKIP LOCKED claiming. `TestCollectorRepair` and `TestCoreDatabaseMigrations` pass. |
| Provider ingestion and persistence ordering | FRED and Binance runners archive/register each bounded page, normalize, persist records, then advance checkpoints; TCMB uses the common persist-before-success pipeline. Full Go apps/internal tests pass. |
| Content reuse with occurrence history | `raw_object_occurrences` is append-only and content-addressed re-fetches reuse one raw object while retaining per-run occurrences. `TestRawArchiveDatabaseStoreRegistration` and `TestRawArchiveIdenticalRefetchPreservesOccurrences` are included; the packet integration group passes. |
| FRED identity repair | FRED persistence resolves local `series.source_code` and compares upstream series code to that value before writing local UUID revisions. |
| Configuration and release surface | Strict config validation rejects unsupported request fields, inline credentials, invalid identities/endpoints, and missing provider local IDs. Collector image build and `--version` smoke pass. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: complete `task verify` is blocked by the managed worktree lacking `node_modules/.bin/prettier.cmd`; the repository root has that dependency. The isolated FRED vintage test also requires a fresh fixture database because its fixed raw fixture already exists in the shared review database.

## Verification

| Command | Result |
|---|---|
| `go test ./apps/... ./internal/... -count=1` | pass |
| `go test ./test/integration -run 'TestCollectorRepair|TestRawArchive|TestCoreDatabase' -count=1` | pass against isolated PostgreSQL |
| `task generate` | pass; SQLC and contract models synchronized |
| `task migrate-test` | pass against isolated PostgreSQL at migration 13 |
| `docker build --file infra/images/collector.Dockerfile --build-arg VERSION=ar707 --tag atrisk-collector:ar707 .` | pass |
| `docker run --rm atrisk-collector:ar707 --version` | pass: `atlasrisk collector version ar707 runtime go1.27.0` |
| `task verify` | blocked at frontend format check because worktree-local `node_modules` is absent |
| `go test ./test/integration -run 'TestFREDVintage' -count=1` | blocked by pre-existing fixed fixture row in the shared review DB (`insert FRED raw object: rows=0`); no code failure observed |

## Change inventory

- Files changed: durable collector config/store/worker/runner, ingestion pipeline and tests, FRED store, migration 13, SQLC outputs, collector command/image, CI/release scripts, architecture/release docs, and packet-listed integration tests.
- Schema/API changes: collector schedule leases/retry state and immutable raw occurrence table; `RunStore` gained optional run-status loading; SQLC models include `lease_seconds` and retry state.
- Generated artifacts: `internal/platform/database/core.sql.go`, `models.go`, and `querier.go` regenerated with SQLC; contract generation was run by `task generate`.

## Git state

- Branch: `task/AR-707-ingestion-repair`
- Commit SHA: final commit recorded by `git log -1 --format=%H` after this report commit
- Remote branch: push was rejected by the automatic review: `Pushing the private repository contents to the unverified origin remote is sensitive egress; task-branch workflow authorization does not establish that this destination is trusted or authorize exposing the code there.` No workaround was attempted.
- Worktree: clean after commit

## Assumptions and risks

- Local PostgreSQL schema was already at migration 13; the isolated review database received the new lease/retry columns with equivalent `ALTER TABLE` statements for runtime verification. Fresh migration application is covered by `TestCoreDatabaseMigrations`.
- Hosted CI or the parent worktree should rerun `task verify` with frontend dependencies available and rerun fixture-sensitive FRED tests against a fresh database.
