# AR-601 end-to-end evidence

## Journey proof

`TestAtlasRiskJourney` (`test/e2e/journey_test.go`) drives portfolio creation,
manual-price preview/commit, valuation, stress submission/read, and decision
create/finalize/evidence/review/timeline through the API HTTP handlers backed by an
isolated PostgreSQL database. Raw payloads and sealed evidence use the configured
Garage/S3-compatible store. The Python worker claims and completes the queued run
through the production `PostgresQueueClient` adapter. The test checks historical
source-as-of and system-as-of answers after a price revision, exact TRY/USD valuation
totals and price/FX lineage, persisted position/attribution/metric rows, exact stress
loss and reconciliation, and unchanged sealed evidence after review. A second
unpriced snapshot must produce a blocked valuation.

The fixed system fixture is `test/fixtures/system/journey.json`. Its 64 business-day
price observations are generated deterministically, supplying the full 63-return
window required for valid pre-stress volatility metrics.

## Cross-cutting regression proofs

`task test-e2e` also runs point-in-time revision, quality gating, valuation lineage,
risk result, decision sealing/reconstruction, and route-level UI tests. The UI route
tests use Testing Library with controlled API responses; they verify interaction and
failure-state rendering, but are not a live-browser test against the database-backed
journey. The HTTP journey invokes API handlers through `httptest`; it does not launch
the production API executable.

When the end-to-end step fails in GitHub Actions, its console output is captured at
`.task/ci-failure/e2e.log` and uploaded with the CI failure diagnostics artifact.
