# AR-102 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-102-raw-archive-ingestion.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-005, ADR-006, ADR-007, ADR-017
- Owned paths: `apps/collector/`, `internal/archive/`, `internal/ingestion/`, `test/fixtures/http/`
- Shared paths changed and justification: `Taskfile.yml` adds the required Go test targets; `.github/workflows/ci.yml` provisions an ephemeral, pinned Garage service on the hosted GitHub runner for the raw-archive integration test; `test/integration/` adds the required PostgreSQL `DatabaseStore.RegisterRawObject` proof; `go.mod` and `go.sum` add the AWS SDK for Go v2 S3 client.

## Result

`complete`

## Review follow-up

- Independent review blockers were fixed on the task branch: redirects revalidate every hop and reject HTTPS downgrade; transport errors redact query secrets; S3 writes use conditional create and compare existing bytes; PostgreSQL conflicts validate immutable fields using JSONB equality; and a PostgreSQL registration test covers idempotency, secret redaction, and conflicts.
- The Garage integration pre-seeds a different same-length body and confirms `ErrContentMismatch` without overwriting original bytes. No local Docker, database, or service action was used.

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
| Hosted GitHub Actions Verify run `36036834134` on head `0e1b1a353f60a0d03f5b87e82cea3ddfeb0913cc` | pass; Garage/PostgreSQL raw archive integration and full verification gate both succeeded; hosted Garage cleanup succeeded. |

## Merge record

- PR: [#12](https://github.com/oplosy/atrisk/pull/12)
- Merge commit: `133841e030c77c6c0ae063eb64c5882d7db68cdb`
- Task status: merged after independent review and hosted Verify success.

## Change inventory

- Files changed: `.github/workflows/ci.yml`, `Taskfile.yml`, `go.mod`, `go.sum`, `internal/archive/`, `internal/ingestion/`, `test/fixtures/http/`, `test/integration/raw_archive_test.go`, and this report.
- Schema/API changes: no database migration; added archive, bounded fetcher, adapter, replay, pipeline, and PostgreSQL run/object persistence interfaces.
- Generated artifacts: none intentionally changed.

## Git state

- Branch: `task/AR-102-raw-archive-ingestion`
- Implementation commit SHA: `0c52153ab3c43bc8e026b3016563127789529372` (corrects the same-size Garage byte-mismatch proof on top of `1eb95e7aebd97bec9a66a4f20a87808a61895e1a`)
- Worker handoff snapshot: `dc504339b7ceca891d312cb987796b8fdd1e50a5`; local and remote matched and the worktree was clean.
- Merge commit on `main`: `133841e030c77c6c0ae063eb64c5882d7db68cdb`.
- Hosted Verify: run `36036834134` passed on the final implementation branch head; this status-finalization PR changes task/report metadata only.

## Assumptions and risks

- Local integration commands intentionally failed closed without `ATLASRISK_TEST_DATABASE_URL` / `ATLASRISK_S3_ENDPOINT`; the same PostgreSQL and Garage acceptance paths passed on isolated GitHub-hosted services.
- Credentials in CI are synthetic test-only values and are not production secrets.
