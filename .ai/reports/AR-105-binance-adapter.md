# AR-NNN Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-105-binance-adapter.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-002, ADR-005, ADR-006, ADR-011, ADR-024
- Owned paths: `internal/sources/binance/`, `test/fixtures/binance/`
- Shared paths changed and justification: `internal/ingestion/` was explicitly authorized for bounded HTTP 418 handling and additionally required to merge final run coverage so post-completion Binance evidence is retained. `db/migrations/` was authorized by amendment `426b28a` under ADR-024 to preserve provider asset codes. Integration tests cover migration, USDT persistence, evidence, and checkpoint ordering.

## Result

`needs-review`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `client.go` builds only HTTPS `/api/v3/exchangeInfo` with `permissions=SPOT` and `/api/v3/klines` with `interval=1d`; production defaults to `https://data-api.binance.vision`, and request-builder tests assert exact path/query allowlists. |
| AC-2 | `adapter.go` uses an injectable UTC clock to exclude candles whose close time has not passed; `Store.PersistInstrumentRecords` appends missing-symbol and upstream-status evidence to `ingestion_runs.coverage.binance_status_evidence` without creating missing instruments. Evidence is accepted after `CompleteRun` and remains queryable after completion. |
| AC-3 | Kline millisecond timestamps are parsed with `time.UnixMilli().UTC()` and decimal fields remain strings validated by a decimal grammar; no floating-point conversion is used. |
| AC-4 | Price records retain `source_known_at` as nil and `first_observed_by_system`, with `source_publication_time_unknown` quality evidence. |
| AC-5 | `internal/ingestion/fetcher.go` retries 429/418 and 5xx with bounded `Retry-After`; an oversized provider wait fails closed. `AdvanceComplete` excludes incomplete current candles and keeps their open boundary for restart. `PersistPriceRecordsAndCheckpoint` validates checkpoint/request symbol identity and commits accepted prices plus the checkpoint atomically; integration verifies unrelated and rejected cursors leave the prior checkpoint unchanged. |
| AC-6 | `Store.PersistPriceRecords` uses append-only `price_revisions` identity and raw SHA lookup; duplicate pages are idempotent and a changed raw candle can create a new revision. Integration assertions cover exact raw SHA joins and unknown publication time. |
| AC-7 | `db/migrations/00003_asset_unit_codes.sql` widens only instrument/price unit fields to uppercase normalized `TEXT`, preserves existing values, leaves FX quote revisions `CHAR(3)`, and integration asserts exact `USDT` persistence. |

## Stop-condition check

- Decision or scope conflict: `none`; ADR-024 and packet amendment `426b28a` authorize the asset-code migration. FX quote revisions remain ISO-4217 fiat-only and are not widened.
- Missing dependency, unsafe migration, or unavailable verification: isolated PostgreSQL DSN is not configured; migration and integration commands therefore fail closed. The repository's `task` executable is also unavailable in this environment, so the packet task wrappers were run through equivalent Go commands where possible. No Docker or local service was started or changed.

## Verification

| Command | Result |
|---|---|
| `task test-go TEST=Binance` | unavailable: PowerShell reports `task` is not installed; equivalent `go test ./apps/... ./internal/... -run 'TestBinance' -count=1` passed. |
| `task test-go-integration TEST=BinanceMarketData` | unavailable: `task` is not installed; equivalent isolated run failed closed because `ATLASRISK_TEST_DATABASE_URL` is not configured. |
| `task migrate-test` | unavailable: `task` is not installed; migration assertions are present in `TestCoreDatabaseMigrations` and `TestCoreDatabasePreviousVersionUpgrade`, but no PostgreSQL DSN was available. |
| `task test-contract` | unavailable: `task` is not installed; no contract files were changed. |
| `go test ./apps/... ./internal/... ./test/integration -count=1` | pass; integration tests were skipped without required DSN. |
| `go vet ./apps/... ./internal/...` | pass. |
| `rg -n "TRADE|USER_DATA|apiKey|secret" internal/sources/binance` | pass; no matches. |
| `git diff --check` | pass. |

## Change inventory

- Files changed: `db/migrations/00003_asset_unit_codes.sql`, `internal/sources/binance/client.go`, `internal/sources/binance/adapter.go`, `internal/sources/binance/store.go`, `internal/sources/binance/adapter_test.go`, `internal/ingestion/fetcher.go`, `internal/ingestion/fetcher_test.go`, `internal/ingestion/pipeline.go`, `test/fixtures/binance/exchange-info.json`, `test/fixtures/binance/daily-klines.json`, `test/integration/binance_market_data_test.go`, `test/integration/core_database_test.go`, and this report.
- Schema/API changes: forward migration widens `instruments.native_currency` and `price_revisions.quote_currency` to normalized uppercase `TEXT`; existing values are preserved, and `fx_quote_revisions` remains unchanged.
- Generated artifacts: none changed.

## Git state

- Branch: `task/AR-105-binance-adapter`
- Implementation/code tip: `4f6af16941740ab9b20669ff6a4e9294787e7ed1`; a report-only handoff commit follows this implementation tip.
- Report-only handoff tip before this correction: `2889c7e2ad278c35f668edb3d8e679fe9c71a25d`; the current report-only correction commit follows.
- Remote branch: `origin/task/AR-105-binance-adapter` is synchronized after the report-only handoff commit.
- Worktree: clean after the report-only handoff commit.

## Assumptions and risks

- Binance kline responses do not carry a source publication timestamp; the adapter intentionally records first-observed system knowledge rather than retrieval time.
- Missing catalog symbols are emitted as quality/status records and are not inserted as synthetic instruments; explicit non-TRADING statuses are preserved as inactive evidence rather than inferred delistings.
- Binance asset codes are distinct from ISO fiat currencies; no peg or conversion is inferred. Reporting-currency valuation still requires an explicit persisted price/FX path.
