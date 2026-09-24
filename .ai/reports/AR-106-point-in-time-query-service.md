# AR-106 Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-106-point-in-time-query-service.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-006, ADR-007, ADR-009, ADR-011
- Owned paths: `internal/application/timeline/`, `apps/api/handlers/timeline/`, `db/queries/timeline/`
- Shared paths changed and justification: `contracts/openapi/` documents the public routes, 400 client errors, and point-in-time semantics; generated SQLC output is required to compile the timeline query source; `test/integration/point_in_time_api_test.go` supplies the required real PostgreSQL proof; `.github/workflows/ci.yml` runs that integration test in hosted CI. No web client generator exists in this checkout.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `TestPointInTimeAPI` seeds three immutable revisions and asserts latest `110`, source-as-of `100`, and system-as-of `90`; the late source-known/system-known ordering proves the clocks answer different questions. |
| AC-2 | `TestPointInTimeAPI` requests source-as-of for a series with no `source_known_at` values and asserts HTTP 409, `SOURCE_AS_OF_UNSUPPORTED`, `request_id`, and explicit capability details. Series metadata derives this capability from persisted evidence rather than provider-name heuristics. |
| AC-3 | Observation responses include unit, frequency, nested source/system clocks and knowledge basis, quality JSON, raw object UUID, and raw SHA-256. The integration fixture asserts the raw provenance UUID is not combined with the SHA. |
| AC-4 | Latest, source-as-of, system-as-of, revisions, and combined cross-source routes are explicit in `apps/api/handlers/timeline/handler.go` and `contracts/openapi/openapi.json`; combined mode is restricted to the cross-source endpoint and uses the selected clock. |
| AC-5 | `db/queries/timeline/timeline.sql` applies keyset predicates after `DISTINCT ON` winner selection, and series listing uses a composite keyset over data-source code, source code, and UUID rather than offset pagination. The integration fixture requests two combined pages, two series-list pages, and two revisions pages with stable cursors. |
| AC-6 | SQLC output is regenerated from the timeline query source; OpenAPI is updated for routes, schemas, capabilities, errors, and mode semantics. Contract tests and generated-file verification were run. |

## Stop-condition check

- Decision or scope conflict: `none`; behavior follows the accepted three-clock, append-only, contract-first, and explicit-quality ADRs.
- Missing dependency, unsafe migration, or unavailable verification: the required PostgreSQL integration test cannot run locally because no isolated `ATLASRISK_TEST_DATABASE_URL` is configured; it failed closed without starting or changing Docker/services. The `task` executable is unavailable, so equivalent Go/Node commands were run. Hosted PR CI is required for the database-backed proof.

## Verification

| Command | Result |
|---|---|
| `task test-go TEST=Timeline` | unavailable: `task` is not installed; equivalent timeline/handler Go tests passed. |
| `task test-go-integration TEST=PointInTimeAPI` | unavailable: `task` is not installed; fail-closed equivalent reports isolated database URL required. |
| `task test-contract` | unavailable: `task` is not installed; `node --test test/contract/contract.test.mjs` passed, 9 tests. |
| `task check-generated` | unavailable: `task` is not installed; `node scripts/verify/check-generated.mjs` passed after staging generated-state normalization. |
| `go test ./... -count=1` | pass. |
| `go vet ./apps/... ./internal/...` | pass. |
| `sqlc generate -f db/queries/core/sqlc.yaml` | pass; local SQLC v1.31.1 regenerated the timeline query output. |
| `ATLASRISK_REQUIRE_TEST_DATABASE=1 go test ./test/integration -run '^TestPointInTimeAPI$' -count=1` | fail-closed as expected: test database URL is required. |
| `git diff --check` | pass. |

## Change inventory

- Files changed: `.github/workflows/ci.yml`, `apps/api/cmd/api/main.go`, `apps/api/handlers/timeline/handler.go`, `apps/api/handlers/timeline/handler_test.go`, `contracts/openapi/openapi.json`, `db/queries/core/sqlc.yaml`, `db/queries/timeline/timeline.sql`, `internal/application/timeline/service.go`, `internal/application/timeline/service_test.go`, `internal/platform/database/querier.go`, generated `internal/platform/database/timeline.sql.go`, `test/integration/point_in_time_api_test.go`, and this report.
- Schema/API changes: no database migration; read-only timeline queries implement latest, source-as-of, system-as-of, revision, and combined projections with stable keyset cursors and explicit source-vintage capability errors. API is mounted at `/api/v1` (the handler also accepts `/v1` for compatibility).
- Generated artifacts: SQLC timeline query output regenerated; existing contract model outputs remain synchronized; no web client generator exists in the repository.

## Git state

- Branch: `task/AR-106-point-in-time-query-service`
- Implementation/code tip: `da8e44b024a4bc26dfd4bad566cc179395f28edd`; a report-only handoff commit follows.
- Remote branch: implementation is pushed to `origin/task/AR-106-point-in-time-query-service`; final handoff push will synchronize the report-only commit as well.
- Worktree: clean before the report-only handoff commit; expected clean after its push.

## Assumptions and risks

- Source-as-of capability is data-derived: a series is supported when at least one immutable revision has a non-null `source_known_at`; sources without defensible publication/vintage clocks return an explicit capability error.
- Latest semantics are the latest persisted system revision; system-as-of selects the latest revision known by the system cutoff; source-as-of selects the latest source-known revision at the source cutoff, with system-known tie-breaking.
- Hosted PostgreSQL CI must execute `TestPointInTimeAPI` to validate SQL behavior and pgx UUID-array encoding; local verification was intentionally fail-closed.
