# Architecture Decision Register

All entries below are **accepted** for V1. Changing one requires a new superseding
ADR that explains migration and compatibility impact. Implementation tasks may
not reopen these decisions implicitly.

| ID | Decision | Rationale and consequence |
|---|---|---|
| ADR-001 | Single-user, self-hosted V1 | Avoid tenancy/auth complexity; localhost by default, external OIDC proxy for remote access. |
| ADR-002 | Decision support only | No execution, broker credentials, custody, signals, or automated trading boundary. |
| ADR-003 | Monorepo modular monolith | One product/release boundary; Go API/collector and Python risk worker are separate processes only where runtime needs differ. |
| ADR-004 | PostgreSQL 18 without TimescaleDB | Expected volume fits PostgreSQL; preserve unconstrained point-in-time keys. Reconsider only with measured evidence. |
| ADR-005 | Garage 2.x immutable raw archive | Default local/self-hosted storage is Garage through the AWS SDK for Go v2 S3 interface; every revision traces to content-addressed raw evidence and AWS S3 remains compatible. |
| ADR-006 | Three-clock point-in-time model | Observation, source knowledge, and system knowledge times answer distinct questions without fabricated publication data. |
| ADR-007 | Append-only revisions and snapshots | Latest state is a projection; corrections never overwrite historical evidence. |
| ADR-008 | PostgreSQL durable job queue | `SKIP LOCKED`, leases, attempts, and idempotency are sufficient for V1; no external broker. |
| ADR-009 | Contract-first boundaries | OpenAPI 3.1 for HTTP and JSON Schema for jobs/imports; generated types prevent Go/Python/TypeScript drift. |
| ADR-010 | Exact persistence, bounded float analytics | `NUMERIC(38,18)` for financial storage; NumPy float64 only within versioned analytics with tolerances and golden tests. |
| ADR-011 | Explicit data-quality gating | Every result is valid, degraded, or blocked with machine-readable causes; stale/missing inputs cannot look healthy. |
| ADR-012 | Deterministic valuation and FX paths | Every selected price and ordered FX quote path is persisted; no hidden quote or route selection. |
| ADR-013 | Deterministic risk engine | Pure Python numerical functions, immutable inputs, versioned parameters, no network calls, no LLM involvement. |
| ADR-014 | Shapley stress-factor attribution | Position P&L reconciles to total; interacting factor effects are allocated reproducibly, with unsupported residual visible. |
| ADR-015 | Versioned scenarios | Templates are immutable versions; prior runs always retain original shocks, mappings, and engine version. |
| ADR-016 | Manual snapshots before transaction ledger | V1 supports reliable manual positions and reconciliation while preserving identities required for later TradeLedger integration. |
| ADR-017 | OpenTelemetry and structured logs | Correlation IDs connect ingest, jobs, calculations, and requests without logging secrets or portfolio payloads. |
| ADR-018 | Protected trunk workflow | Short-lived task branches target `main`; no long-lived `dev`, no direct main push, one worktree per active writer. |
| ADR-019 | Vendor-neutral task packets | Markdown plus YAML frontmatter defines scope, dependencies, ownership, acceptance, verification, and handoff for any AI/human executor. |
| ADR-020 | Sol-medium orchestrator, Luna-high workers | The Codex profile uses `gpt-5.6-sol` medium for planning/coordination and `gpt-5.6-luna` high for bounded worker roles; core artifacts remain model-independent. |
| ADR-021 | Maximum three concurrent workers | Parallelism is limited to independent tasks with disjoint owned paths; review/merge remains serial. |
| ADR-022 | LLM enrichment deferred | A future LLM may classify sourced news/theses or draft explanations, but cannot supply prices, predictions, shocks, or risk numbers. |
| ADR-023 | Durable database schedules | Collector schedules/checkpoints are claimed from PostgreSQL with leases and idempotency; V1 does not depend on OS cron or an external scheduler. |
| ADR-024 | Financial unit codes distinguish fiat currencies from asset tickers | Instrument denomination and price quote-unit fields use normalized uppercase text codes so digital assets such as `USDT` are preserved exactly; FX quote revisions remain ISO 4217 fiat currencies. No asset peg or conversion is inferred: valuation into a reporting currency requires an explicit, persisted price/FX path, otherwise the result is degraded or blocked. |
| ADR-025 | Account-scoped immutable reconciliation checkpoints | Checkpoints bind a valid valuation to one account, compare exact persisted TRY/USD line sums, snapshot a versioned tolerance, and preserve optional line-check evidence without changing valuation history. |

## Decision quality gate

Before an implementation task becomes `ready`, the orchestrator must confirm:

- it does not require an unrecorded architecture choice;
- its API/schema behavior follows the accepted decisions;
- its owned paths do not overlap another active writer;
- its acceptance criteria prove behavior, failure states, and provenance;
- its verification commands are executable and deterministic.
