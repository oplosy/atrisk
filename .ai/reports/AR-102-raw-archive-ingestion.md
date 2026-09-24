# AR-102 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-102-raw-archive-ingestion.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-005, ADR-006, ADR-007, ADR-017
- Owned paths: `apps/collector/`, `internal/archive/`, `internal/ingestion/`, `test/fixtures/http/`
- Shared paths changed and justification: `Taskfile.yml` adds the required Go test targets; `.github/workflows/ci.yml` provisions an ephemeral, pinned Garage service on the hosted GitHub runner for the raw-archive integration test; `test/integration/` adds the required PostgreSQL `DatabaseStore.RegisterRawObject` proof; `go.mod` and `go.sum` add the AWS SDK for Go v2 S3 client.

## Result

`needs-review`

## Review follow-up

- Independent review found five blocking issues. Redirect/error redaction, conditional S3 create/content validation, database conflict validation, and the required PostgreSQL integration proof are now implemented. No local Docker or service action was used.
- Second review found one remaining Garage proof gap, and hosted CI exposed an idempotent PostgreSQL-registration test failure. The Garage test now pre-seeds a same-size conflicting object and verifies `ErrContentMismatch` plus byte preservation; PostgreSQL conflict comparison now uses native `jsonb` equality. The same-size fixture correction is in `0c52153ab3c43bc8e026b3016563127789529372`.

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `internal/archive/store_test.go` exercises conditional S3-level concurrent creates and tampered-content rejection; `internal/archive/raw_archive_integration_test.go` exercises concurrent Put/Get against hosted Garage. |
| AC-2 | `internal/ingestion/pipeline_test.go` runs the fetch/archive/normalize pipeline and asserts every normalized record carries the exact archived key and SHA-256. |
| AC-3 | `internal/ingestion/fetcher_test.go`, `internal/archive/store_test.go`, `internal/ingestion/pipeline_test.go`, and `test/integration/raw_archive_test.go` assert authorization/API-key values are excluded from request/response metadata, transport errors, and stored archive metadata. |
| AC-4 | Unit tests cover timeout, oversized response, invalid media type, and storage failure; `internal/archive/raw_archive_integration_test.go` proves Put/Get bytes against Garage in hosted CI. |
| AC-5 | `test/fixtures/http/sample-response.json` is replayed by `TestReplayFixtureUsesSavedBytesWithoutNetwork`, with exact raw SHA-256 provenance. |

## Stop-condition check

- Decision or scope conflict: none.
- Missing dependency, unsafe migration, or unavailable verification: local required integration targets fail closed because `ATLASRISK_TEST_DATABASE_URL` and `ATLASRISK_S3_ENDPOINT` are not configured. No local Docker command was run; hosted CI Garage and PostgreSQL are configured in `.github/workflows/ci.yml`.

## Verification

| Command | Result |
|---|---|
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 test-go TEST=Ingestion` | pass; Go ingestion tests passed. |
| `go test ./...` | pass; all Go packages passed. |
| `go vet ./apps/... ./internal/...` | pass. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 test-go TEST=Ingestion` | pass after remediation. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 test-go-integration TEST=RawArchive` | blocked locally; the new PostgreSQL integration test correctly failed closed because `ATLASRISK_TEST_DATABASE_URL` was not configured. No Docker command was run. |
| `go run github.com/go-task/task/v3/cmd/task@v3.44.1 verify` | partial after remediation; format, lint, typecheck, unit, and contract checks passed, then PostgreSQL integration stopped because `ATLASRISK_TEST_DATABASE_URL` was not configured. Generated artifacts were restored after the check. |
| `go test ./internal/archive ./internal/ingestion -run 'Test(Ingestion|RawArchive)' -count=1` | pass; archive and ingestion tests passed. |
| `git diff --check` | pass. |

## Change inventory

- Files changed: `.github/workflows/ci.yml`, `Taskfile.yml`, `go.mod`, `go.sum`, `internal/archive/`, `internal/ingestion/`, `test/fixtures/http/`, `test/integration/raw_archive_test.go`, and this report.
- Schema/API changes: no database migration; added archive, bounded fetcher, adapter, replay, pipeline, and PostgreSQL run/object persistence interfaces.
- Generated artifacts: none intentionally changed.

## Git state

- Branch: `task/AR-102-raw-archive-ingestion`
- Implementation commit SHA: `0c52153ab3c43bc8e026b3016563127789529372` (corrects the same-size Garage byte-mismatch proof on top of `1eb95e7aebd97bec9a66a4f20a87808a61895e1a`)
- Remote branch: push pending for this test correction; worktree is clean before push.
- Orchestrator review-status update follows on the same task branch.

## Assumptions and risks

- Hosted CI must pass the Garage integration and full verification before merge; local Docker was deliberately not used per operator instruction.
- Credentials in CI are synthetic test-only values and are not production secrets.
