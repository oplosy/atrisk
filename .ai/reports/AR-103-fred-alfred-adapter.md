# AR-NNN Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-103-fred-alfred-adapter.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-005, ADR-006, ADR-007, ADR-011
- Owned paths: `internal/sources/fred/`, `test/fixtures/fred/`
- Shared paths changed and justification: `test/integration/fred_vintage_test.go` adds the required real PostgreSQL vintage and system-as-of proof. No schema migration or generated query source was needed.

## Result

`needs-review`

## Review follow-up

- The checkpoint now persists a SHA-256 fingerprint of the complete normalized
  FRED request (series, vintage/realtime bounds, observation filters, units,
  frequency, aggregation, output type, and effective page limit). Resume rejects
  any request whose fingerprint differs, and separately rejects a tampered
  checkpoint series ID; the unit and PostgreSQL integration tests cover
  original-request resume and changed-vintage rejection.

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `internal/sources/fred/adapter_test.go` preserves initial and revised values for the same observation date with source vintage timestamps; `test/integration/fred_vintage_test.go` persists both append-only revisions and checks source-as-of values for 2025-01-15 and 2025-02-15. |
| AC-2 | `test/integration/fred_vintage_test.go` checks source-as-of selects the release known on the requested vintage date and system-as-of returns no rows before the captured retrieval time but returns the revised value after ingestion. |
| AC-3 | `internal/sources/fred/adapter_test.go` and the integration test preserve FRED `.` as `value_text` with a `missing` quality flag and never write zero. |
| AC-4 | `internal/sources/fred/adapter_test.go` covers duplicate-safe normalization/provenance, bounded offset pagination, checkpoint advancement, original-request resume, and filter/limit mismatch rejection; `fred.Store.SaveCheckpoint`/`LoadCheckpoint` persist the request-bound offset in `ingestion_runs.coverage`; duplicate `PersistRecords` inserts affect zero rows. |
| AC-5 | `internal/sources/fred/adapter_test.go` verifies the request carries the API key only upstream and `archive.RedactedURL` removes the key from persisted/loggable URI metadata. |

## Stop-condition check

- Decision or scope conflict: none. ADR-005/006/007/011 are represented in the accepted register at `docs/decisions/README.md`; no separate ADR files exist in this checkout.
- Missing dependency, unsafe migration, or unavailable verification: local PostgreSQL integration is unavailable because `ATLASRISK_TEST_DATABASE_URL` is not configured. The required integration test fails closed; no Docker command or lifecycle action was used.

## Verification

| Command | Result |
|---|---|
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 test-go TEST=FRED` | pass; FRED unit tests passed. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 test-go-integration TEST=FREDVintage` | blocked locally; test failed closed with `isolated database validation failed: test database URL is required`. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 test-contract` | pass; all contract tests passed. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 verify` | partial; format, lint, typecheck, unit, and contract stages passed, then existing integration gate failed closed because `ATLASRISK_TEST_DATABASE_URL` was not configured. |
| `git diff --check` | pass. |

Review-fix rerun: scoped `test-go TEST=FRED`, `go test ./test/integration -run TestFREDVintage`, `go vet ./apps/... ./internal/...`, and `git diff --check` passed/compiled; the required integration test remains fail-closed without an explicit isolated PostgreSQL DSN. `test-contract` passed and generated drift was restored.

## Change inventory

- Files changed: `internal/sources/fred/client.go`, `internal/sources/fred/adapter.go`, `internal/sources/fred/store.go`, `internal/sources/fred/adapter_test.go`, `test/fixtures/fred/vintage-observations.json`, `test/integration/fred_vintage_test.go`, and this report.
- Schema/API changes: no database migration; FRED adapter/client, bounded vintage pagination, append-only observation persistence, and persisted ingestion checkpoint helpers added using existing tables.
- Generated artifacts: none intentionally changed; verification-generated drift was restored because no query source changed.

## Git state

- Branch: `task/AR-103-fred-alfred-adapter`
- Commit SHA: `b405963316bc966763f0b39fbc0eec5c2c941250` (checkpoint fingerprint review fix; tampered-series fix pending)
- Remote branch: `task/AR-103-fred-alfred-adapter` pending tampered-series fix push
- Worktree: dirty only with the tampered-checkpoint fix until commit/push

## Assumptions and risks

- Local integration and full verify require the explicitly isolated PostgreSQL DSN; hosted CI should exercise the same test against its ephemeral PostgreSQL service.
- Independent review checkpoint fingerprint finding is addressed in this follow-up. Hosted integration verification, PR merge, and task lifecycle finalization remain outstanding; this report does not claim acceptance completion.
- The FRED `realtime_start` date is recorded as `source_known_at` at UTC midnight with `source_published_at`; the raw realtime interval remains in quality metadata and the normalized record.
