---
id: AR-707
title: Wire durable collector and persist ingestion before success
status: ready
phase: 7
depends_on: [AR-702]
branch: task/AR-707-ingestion-repair
base_sha: a4a060559e40c0773fe7eb7475b8f08b313ec1bc
owned_paths: [apps/collector/, internal/collector/, internal/ingestion/, internal/sources/, .ai/reports/AR-707-ingestion-repair.md]
shared_paths: [db/migrations/, db/queries/core/, internal/platform/database/models.go, docs/architecture/SYSTEM_ARCHITECTURE.md, docs/architecture/DATA_AND_RISK_MODEL.md, docs/releases/PUBLISHING.md, infra/images/collector.Dockerfile, scripts/release/, .github/workflows/ci.yml, test/integration/core_database_test.go, test/integration/raw_archive_integration_test.go, test/integration/collector_repair_test.go, test/fixtures/http/]
adrs: [ADR-003, ADR-005, ADR-006, ADR-007, ADR-008, ADR-017, ADR-023]
---

# AR-707: Wire durable collector and persist ingestion before success

## Outcome and readiness

Repair user-authorized review findings 6, 7 and 8: collector performs actual bounded provider ingestion; unchanged source content can be re-fetched; success means normalized persistence completed.

The orchestrator locks the following concrete boundary under accepted ADRs before dispatch. This implements the already accepted PostgreSQL scheduler, not OS cron or a new broker. No other task writes migrations. Preserve immutable raw/revision history and secrets outside configuration records.

## In scope and inputs

- Replace collector skeleton with version, one-shot (--once) and continuous poll-loop modes, signal cancellation and fail-closed validated configuration. Use required database/archive configuration consistent with API; malformed or missing required archive configuration prevents ingestion.
- Configuration is an explicit local JSON schedule file containing non-secret schedule name, provider (fred/tcmb/binance), existing source/dataset/series identities, upstream request/window, interval and bounded retry/lease policy. Provider API credentials are environment-variable references resolved at runtime, never raw values in PostgreSQL/config/logs.
- Use a PostgreSQL schedules table with next-run UTC, active lease owner/expiry and durable configuration/checkpoint fields. Claim due or expired work with SKIP LOCKED; complete/fail with lease-owner fencing. Idempotency binds schedule occurrence/request/page; crashes/retries cannot skip or falsely finish records. A changed request must not reuse an old checkpoint.
- Wire existing provider adapters, bounded HTTPS fetcher, immutable archive and provider Store persistence. Exercise FRED/TCMB observation and Binance spot catalog/closed daily price paths using local TLS fixtures; do not create broker/auth/trading calls.
- Pagination is bounded; each response page is archived and persisted before checkpoint advancement. Complete a scheduled request only after its configured range/pages are covered; truncated/max-page coverage is explicit, not succeeded-as-complete.
- Make normalized persistence a required pipeline completion callback/boundary. Provider persistence errors leave ingestion failed/non-succeeded. Existing direct pipeline callers/tests must explicitly provide persistence; replay-only functions remain offline and do not claim durable success.
- Content-addressed raw objects retain immutable first registration. A distinct run with identical bytes reuses content identity; record retrieval/request/run metadata in a new append-only occurrence association so first system-known clocks/revisions are preserved. Validate content address/key/length and do not silently discard a metadata conflict.
- Keep migration forward-only. Add append-only protection to occurrence history and update migration version assertions. Regenerate sqlc models using existing generator if changed, never edit generated files by hand.
- Add collector image and release build/smoke/docs using existing image conventions. No secrets or live portfolio payloads in artifacts.
- Retry/recovery semantics must be tested at the durable database boundary, not only with fake adapters.

## Out of scope

New providers, upstream API migrations, schema cleanup, portfolio/risk calculation changes, public network deployment, OS cron, production ingestion or real provider credential use.

## Acceptance criteria

- Executable-level TLS fixture -> archive -> normalized PostgreSQL rows succeeds and records exact provenance for supported providers.
- Re-fetch identical body in distinct runs preserves one raw object and original revision system-known time while preserving both retrieval occurrences.
- Provider persistence failure, archive failure and incomplete pagination cannot create a succeeded complete run.
- Competing claims, expired lease/restart and checkpoint replay preserve durable work; stale owners cannot finish a newer lease.
- Configuration rejects unknown fields/invalid identities/unsupported endpoints and credentials remain redacted.
- Clean/previous-version migrations, updated models, collector build/image smoke, focused regressions and complete gate pass.

## Required verification

```text
go test ./apps/collector/... ./internal/collector/... ./internal/ingestion/... ./internal/sources/... -count=1
go test ./test/integration -run 'TestCollectorRepair|TestRawArchive|TestCoreDatabase' -count=1
task generate
task migrate-test
task verify
```

Local isolated test DB: postgres://atrisk:atlasrisk-local-only@127.0.0.1:50358/atrisk_test?sslmode=disable. Credentials are existing disposable local-test defaults. Hosted CI owns the final complete Task/migration gate if local Task is unavailable.

## Handoff evidence

Use .ai/REPORT_TEMPLATE.md. Report acceptance mapping, changed paths, commands/results, SHA, pushed branch and clean worktree. Writer must not delegate. Orchestrator independently reviews and integrates; do not claim provider production/live acceptance.


