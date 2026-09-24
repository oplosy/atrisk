# AR-NNN Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-104-tcmb-adapter.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-005, ADR-006, ADR-007, ADR-011
- Owned paths: `internal/sources/tcmb/`, `test/fixtures/tcmb/`
- Shared paths changed and justification: `internal/ingestion/` was explicitly authorized by the amended packet commit `b7ef9d8`; the common fetcher now forwards the EVDS `key` header upstream, excludes it from response metadata, and strips it before redirects. `test/integration/tcmb_ingestion_test.go` adds the required PostgreSQL integration coverage.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `internal/sources/tcmb/adapter.go` leaves `SourceKnownAt` nil and records `source_publication_time=unavailable_in_evds2_response`, `retrieval_time_not_publication=true`, and `knowledge_time_basis=first_observed_by_system`; unit coverage is in `TestTCMBNormalizesLocaleMissingAndProvenance`. |
| AC-2 | `normalizeDecimal` enforces the configured `.`/`,` separator and rejects mixed locale forms; `parseEVDSDate` covers daily, monthly, quarterly, and yearly forms; `TestTCMBCheckpointAdvancesSourceFrequencyBoundaries` covers period-boundary resume. |
| AC-3 | `null`, empty, and `-` values become `value_text` with `missing`/`missing_expected_period`; delayed windows receive `late` quality evidence. Covered by `TestTCMBNormalizesLocaleMissingAndProvenance`, `TestTCMBMarksLateCoverageWithoutInventingValues`, and the integration assertions. |
| AC-4 | `Store.PersistRecords` resolves the database series UUID to its source code and rejects mismatched records. `PersistFXRecords` requires that series UUID, validates every numeric/missing record before insertion, retains missing FX periods in `observation_revisions` with explicit quality flags, and keeps numeric quotes in `fx_quote_revisions`; integration coverage checks mixed-series rollback, duplicate idempotency, changed-value revisions, missing-date quality, and raw SHA provenance. Checkpoints persist a request fingerprint and source-frequency cursor. |
| AC-5 | EVDS2 request path/auth semantics are covered by `TestTCMBBuildRequestUsesHeaderKeyAndOfficialMetadata` and `TestTCMBFetchesJSONResponseAndCheckpoint`; `internal/ingestion/fetcher_test.go` proves key forwarding, response metadata exclusion, and redirect stripping. |

## Stop-condition check

- Decision or scope conflict: none after amended packet commit `b7ef9d8` authorized `internal/ingestion/` for EVDS credential forwarding.
- Missing dependency, unsafe migration, or unavailable verification: isolated PostgreSQL DSN is not configured; the required integration command fails closed. `task verify` is also blocked at existing web format setup because `node_modules/.bin/prettier.cmd` is unavailable. No Docker or local service was changed.

## Verification

| Command | Result |
|---|---|
| `task test-go TEST=TCMB` | pass; TCMB unit tests and repository Go packages passed. |
| `task test-go-integration TEST=TCMBIngestion` | fail-closed as required; `ATLASRISK_TEST_DATABASE_URL` is not configured. The test includes mixed-series rollback, mandatory series-ID validation, missing FX quality evidence, raw SHA provenance, and numeric-only quote assertions when PostgreSQL is available. |
| `task test-contract` | pass; generation completed and all 9 contract tests passed; generated drift was restored. |
| `task verify` | blocked at `npm run format:check`; existing `node_modules/.bin/prettier.cmd` is unavailable. Go formatting passed and Python formatting completed before that gate. |
| `go test ./apps/... ./internal/... -count=1` | pass. |
| `go vet ./apps/... ./internal/...` | pass. |
| `git diff --check` | pass. |

## Change inventory

- Files changed: `internal/sources/tcmb/client.go`, `internal/sources/tcmb/adapter.go`, `internal/sources/tcmb/store.go`, `internal/sources/tcmb/adapter_test.go`, `internal/ingestion/fetcher.go`, `internal/ingestion/fetcher_test.go`, `test/fixtures/tcmb/evds-observations.json`, `test/fixtures/tcmb/evds-revision.json`, `test/integration/tcmb_ingestion_test.go`, and this report.
- Schema/API changes: no database migration or public contract change; EVDS2 adapter/client, append-only observation/FX persistence, checkpointing, quality evidence, and safe credential forwarding were added.
- Generated artifacts: none intentionally changed; contract/sqlc generation was run and generated drift was restored.

## Git state

- Branch: `task/AR-104-tcmb-adapter`
- Commit SHA: `c6af80d0314a36f021328f6d6848fed2286d9d84` (includes `619bb80`, `78c574f`, `ee60abc`, `8b22cc4`, `8f32506`, and `70c6d75`).
- Remote branch: `origin/task/AR-104-tcmb-adapter` synchronized at `c6af80d0314a36f021328f6d6848fed2286d9d84`.
- Worktree: clean.

## Assumptions and risks

- EVDS3 documentation portal currently redirects without exposing a stable service contract; implementation is limited to the official EVDS2 service contract behind configurable `BaseURL`, as authorized by the packet.
- Local PostgreSQL and the complete `task verify` gate require environment/dependency setup outside this worker; hosted CI should run the integration test with an isolated DSN and install web dependencies.
