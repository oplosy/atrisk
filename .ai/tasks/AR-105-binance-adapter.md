---
id: AR-105
title: Add Binance Spot public market data
status: draft
phase: 1
depends_on: [AR-102]
branch: task/AR-105-binance-adapter
owned_paths: [internal/sources/binance/, test/fixtures/binance/]
shared_paths: [apps/collector/, db/queries/, contracts/]
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

## Out of scope

- API keys, account/user data, orders, websockets, futures, or intraday storage.

## Acceptance criteria

- [ ] Only documented public market-data hosts/endpoints are callable.
- [ ] The current incomplete daily candle is excluded or explicitly provisional.
- [ ] Millisecond timestamps and decimal prices round-trip correctly.
- [ ] Rate-limit responses back off without losing checkpoint integrity.
- [ ] Duplicate pages are idempotent and changed historical candles create revisions.

## Required verification

```text
task test-go TEST=Binance
task test-go-integration TEST=BinanceMarketData
rg -n "TRADE|USER_DATA|apiKey|secret" internal/sources/binance
```
