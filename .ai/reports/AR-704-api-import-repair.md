# AR-704 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-704-api-import-repair.md`
- Packet status at start: `ready`
- Referenced ADRs: `ADR-001`, `ADR-009`, `ADR-010`, `ADR-016` (accepted decision register)
- Owned paths: `apps/api/cmd/api/`, `internal/imports/`, `.ai/reports/AR-704-api-import-repair.md`
- Shared paths changed and justification: `docs/releases/PUBLISHING.md` documents the explicit container listen override required for Docker-published traffic; `test/integration/api_import_repair_test.go` adds PostgreSQL regressions for ownership, rollback, exact decimals, and bulk manual prices.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `apps/api/cmd/api/main.go` defaults `-listen` to `127.0.0.1:8080`; explicit overrides remain supported. `newAPIServer` centralizes 5s header, 30s read/write, and 60s idle deadlines. `main_test.go` covers loopback/default finite values, stalled raw TCP body termination, and a complete body control. `docs/releases/PUBLISHING.md` documents explicit `-listen 0.0.0.0:8080` for Docker-published traffic and the required external TLS/OIDC boundary. |
| AC-2 | `internal/imports/service.go` validates all position account/instrument ownership and all manual-price instruments with set-based `UNNEST` queries. Position lines use one bounded `pgx.CopyFrom`; manual prices use one set-based `UNNEST` insert. Values are parsed as `pgtype.Numeric` and sent to PostgreSQL `NUMERIC(38,18)` without floating-point conversion. |
| AC-3 | `test/integration/api_import_repair_test.go` (`TestAPIImportRepair` and `TestImportAPIRepair`) proves exact 38-digit position/price decimals, cross-portfolio rejection with no new snapshot, duplicate position rejection, and duplicate manual-price revision rollback. Static statement evidence: one set-based validation query per import kind, one `CopyFrom` for position lines, and one `UNNEST` insert for prices; no per-row database writes remain. |

## Stop-condition check

- Decision or scope conflict: `none`.
- Missing dependency, unsafe migration, or unavailable verification: local `task` executable and `.task/bin/task.exe` are unavailable in this worktree; the parent-provided isolated PostgreSQL/Garage services were available. Existing fixed-name `TestImportAPI` was not rerun after an earlier concurrent invocation populated that disposable database; the new unique AR-704 regression ran successfully against the same migrated services.

## Verification

| Command | Result |
|---|---|
| `go test ./apps/api/cmd/api ./internal/imports -count=1` | pass: API deadline/default and import unit tests |
| `go test ./test/integration -run 'TestAPIImportRepair\|TestManualCSV' -count=1` | pass: AR-704 PostgreSQL import regressions |
| `go test ./test/integration -run 'TestImportAPIRepair\|TestManualCSV' -count=1` | pass: ImportAPI-focused AR-704 regression selection |
| `go test ./test/integration -run '^$' -count=1` | pass: integration package compiles |
| `go vet ./apps/api/cmd/api ./internal/imports` | pass |
| `task verify` | unavailable locally: Task executable is not installed in this worktree; hosted gate remains parent-owned |
| `git diff --check` | pass |

## Change inventory

- Files changed: `apps/api/cmd/api/main.go`, `apps/api/cmd/api/main_test.go`, `internal/imports/service.go`, `docs/releases/PUBLISHING.md`, `test/integration/api_import_repair_test.go`, this report.
- Schema/API changes: loopback API default and finite HTTP server deadlines; set-based import validation and bulk writes; no schema or dependency changes.
- Generated artifacts: none.

## Git state

- Branch: `task/AR-704-api-import-repair`
- Implementation commit SHA: `768d2a5c6ef5d2ae71efa7c18c76aaaa00f64fd9`
- Report metadata commit: follows this implementation commit
- Remote branch: pending
- Worktree: dirty until report and implementation commit

## Assumptions and risks

- Manual-price duplicate revisions retain the original service behavior: the PostgreSQL unique constraint rejects the bulk insert and the surrounding transaction rolls back. No `ON CONFLICT DO NOTHING` behavior was introduced.
- The container image keeps an executable-only entrypoint so `migrate` and explicit user arguments retain their existing semantics; Docker-published API traffic must pass `-listen 0.0.0.0:8080` explicitly as documented.
