---
id: AR-105
title: Add Binance Spot public market data
status: ready
phase: 1
depends_on: [AR-102]
branch: task/AR-105-binance-adapter
base_sha: aca562e2777f078277535db74e90934f51091963
owned_paths: [internal/sources/binance/, test/fixtures/binance/]
shared_paths: [apps/collector/, db/queries/, contracts/, test/integration/]
adrs: [ADR-002, ADR-005, ADR-006, ADR-011]
---

# AR-105: Add Binance Spot public market data

## Outcome

AtlasRisk ingests Binance Spot instrument metadata and daily public klines through
the market-data-only endpoint with deterministic time and rate-limit handling.

## In scope

- Exchange info and daily kline parsing, symbol lifecycle, UTC normalization,
  request-weight throttling, pagination, checkpoints, and public fixtures.
- Delisted/missing symbol and incomplete-current-candle behavior.
- Use only `https://data-api.binance.vision` with public `GET /api/v3/exchangeInfo`
  (request only `permissions=SPOT`) and `GET /api/v3/klines` (request only
  interval `1d`). Use the Binance data-only API contract documented at
  [Market Data Only URLs](https://developers.binance.com/en/docs/products/spot/faqs/market_data_only)
  and [Spot Market REST endpoints](https://developers.binance.com/en/docs/catalog/core-trading-spot-trading/api/rest-api/market).
- Preserve source publication semantics honestly: where Binance supplies no
  publication timestamp, store `source_known_at` as null and use the existing
  first-observed-by-system basis; never substitute retrieval time.
- Do not infer that a symbol is delisted solely because it is absent from one
  exchange-info response. Missing requested symbols and explicit upstream
  status changes must produce observable status/quality evidence, not synthetic
  instruments or prices.

## Out of scope

- Any host or endpoint other than the exact data-only host and two public GET
  endpoints above; API keys/secrets, account/user data, orders, websockets,
  futures, margin, or intraday storage.

## Acceptance criteria

- [ ] Requests are restricted to `data-api.binance.vision`, `GET /api/v3/exchangeInfo?permissions=SPOT`, and `GET /api/v3/klines?interval=1d`; no authenticated/private or trading API is callable.
- [ ] The current incomplete UTC daily candle is excluded using an injectable clock, and missing/delisted symbols are visible without inferring delisting from a single omission.
- [ ] Millisecond open/close timestamps normalize to UTC; source decimal strings round-trip through exact decimal storage without floating-point conversion.
- [ ] Unknown source publication time remains null with an explicit first-observed system basis.
- [ ] HTTP 429/418 responses honor `Retry-After`, back off within configured bounds, and do not advance the checkpoint before data is durably accepted.
- [ ] Duplicate pages are idempotent and changed historical candles create append-only price revisions retaining raw provenance.

## Required verification

```text
task test-go TEST=Binance
task test-go-integration TEST=BinanceMarketData
rg -n "TRADE|USER_DATA|apiKey|secret" internal/sources/binance
```

The official documentation states that the data-only host requires no
authentication and serves only public market data; Spot rate-limit responses
include `Retry-After` for HTTP 429/418. The public kline response represents
prices as decimal strings and timestamps in milliseconds by default.
