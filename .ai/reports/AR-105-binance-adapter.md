# AR-NNN Worker Report

## Task authority

- Task packet: `.ai/tasks/AR-105-binance-adapter.md`
- Packet status at start: `ready`
- Referenced ADRs: ADR-002, ADR-005, ADR-006, ADR-011, ADR-024
- Owned paths: `internal/sources/binance/`, `test/fixtures/binance/`
- Shared paths changed and justification: `internal/ingestion/` was explicitly authorized for bounded HTTP 418 handling and additionally required to merge final run coverage so post-completion Binance evidence is retained. `db/migrations/` was authorized by amendment `426b28a` under ADR-024 to preserve provider asset codes. Integration tests cover migration, USDT persistence, evidence, and checkpoint ordering.

## Result

`complete`

## Acceptance evidence

| Criterion | Evidence |
|---|---|
| AC-1 | `client.go` builds only HTTPS `/api/v3/exchangeInfo` with `permissions=SPOT` and `/api/v3/klines` with `interval=1d`; production defaults to `https://data-api.binance.vision`, and request-builder tests assert exact path/query allowlists. |
| AC-2 | `adapter.go` uses an injectable UTC clock to exclude candles whose close time has not passed; `Store.PersistInstrumentRecords` appends missing-symbol and upstream-status evidence to `ingestion_runs.coverage.binance_status_evidence` without creating missing instruments. Evidence is accepted after `CompleteRun` and remains queryable after completion. |
| AC-3 | Kline millisecond timestamps are parsed with `time.UnixMilli().UTC()` and decimal fields remain strings validated by a decimal grammar; no floating-point conversion is used. |
| AC-4 | Price records retain `source_known_at` as nil and `first_observed_by_system`, with `source_publication_time_unknown` quality evidence. |
| AC-5 | `internal/ingestion/fetcher.go` retries 429/418 and 5xx with bounded `Retry-After`; an oversized provider wait fails closed. `AdvanceComplete` excludes incomplete current candles and keeps their open boundary for restart. `PersistPriceRecordsAndCheckpoint` validates checkpoint/request symbol identity and commits accepted prices plus the checkpoint atomically; integration verifies unrelated and rejected cursors leave the prior checkpoint unchanged. |
| AC-6 | `Store.PersistPriceRecords` uses append-only `price_revisions` identity and raw SHA lookup; duplicate pages are idempotent and a changed raw candle can create a new revision. Integration assertions cover exact raw SHA joins and unknown publication time. |
| AC-7 | `db/migrations/00003_asset_unit_codes.sql` widens only instrument/price unit fields to uppercase alphanumeric `TEXT` (non-empty, with no leading-letter or length restriction), preserves existing values, drops/recreates the dependent `latest_price_revisions` view around the type change in both directions, leaves FX quote revisions `CHAR(3)`, and integration asserts exact `USDT` plus digit-leading `1INCH` persistence. |

## Stop-condition check

- Decision or scope conflict: `none`; ADR-024 and packet amendment `426b28a` authorize the asset-code migration. FX quote revisions remain ISO-4217 fiat-only and are not widened.
- Local PostgreSQL DSN and `task` executable were unavailable, so local database tests failed closed. Hosted GitHub Actions Verify run `36057760019` passed `task verify` with isolated PostgreSQL and Garage services, including migration and integration coverage. No local Docker setting or service was changed.

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
| `node scripts/verify/check-generated.mjs` | pass; registered generated outputs are synchronized. |
| `git diff --check` | pass. |
| Hosted GitHub Actions Verify run `36057760019` | pass; isolated migration/integration and full `task verify` gate completed. |

## Change inventory

- Files changed: `db/migrations/00003_asset_unit_codes.sql`, `internal/sources/binance/client.go`, `internal/sources/binance/adapter.go`, `internal/sources/binance/store.go`, `internal/sources/binance/adapter_test.go`, `internal/ingestion/fetcher.go`, `internal/ingestion/fetcher_test.go`, `internal/ingestion/pipeline.go`, `test/fixtures/binance/exchange-info.json`, `test/fixtures/binance/daily-klines.json`, `test/integration/binance_market_data_test.go`, `test/integration/core_database_test.go`, and this report.
- Schema/API changes: forward migration widens `instruments.native_currency` and `price_revisions.quote_currency` to normalized uppercase `TEXT`; existing values are preserved, and `fx_quote_revisions` remains unchanged.
- Generated artifacts: none changed.

## Git state

- Branch: `task/AR-105-binance-adapter`
- Implementation/code tip: `13f7b2d58c739d8b333216118d5b4c9a1fa5bf47`; a report-only handoff commit follows this implementation tip.
- Prior report-only handoff tip: `1fc21ff86996e0e0d8583eb38ee0b61803e50eeb`; the current report-only correction commit follows.
- Remote branch: `origin/task/AR-105-binance-adapter` at `ccc171a2ac5c03173fe582934e6f3fdf636bdcfd`; PR #19 merged.
- Merge commit: `872bd8278b021a5d74bbd4f6835703f6cb68de64`.
- Worktree: clean at merge; finalization is recorded by this follow-up commit.

## Assumptions and risks

- Binance kline responses do not carry a source publication timestamp; the adapter intentionally records first-observed system knowledge rather than retrieval time.
- Missing catalog symbols are emitted as quality/status records and are not inserted as synthetic instruments; explicit non-TRADING statuses are preserved as inactive evidence rather than inferred delistings.
- Binance asset codes are distinct from ISO fiat currencies; no peg or conversion is inferred. Reporting-currency valuation still requires an explicit persisted price/FX path.
