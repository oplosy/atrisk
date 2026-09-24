# AtlasRisk

AtlasRisk is a personal risk operating system that places macroeconomic data,
portfolio exposure, reproducible stress tests, and investment decisions on the
same point-in-time timeline.

AtlasRisk does not generate trade signals and does not execute orders. Its core
question is: **What am I exposed to, which assumptions am I relying on, and what
changes if conditions move against me?**

## Current status

The repository is in the architecture and planning stage. Implementation must
not begin until the applicable task packet in `.ai/tasks/` is marked `ready`.

## V1 scope

- TCMB EVDS, FRED/ALFRED, and Binance Spot public market data
- Append-only raw-response archive and point-in-time observations
- Manual portfolio snapshots and manual price-history import
- TRY and USD valuation with traceable FX conversion
- Volatility, correlation, drawdown, leverage, and concentration analysis
- Three versioned stress templates: TRY depreciation, rate shock, and risk-off
- Decision journal with immutable data, portfolio, scenario, and thesis snapshots
- Explicit missing, stale, partial, and suspect-data states

## Documentation map

- [Product definition](docs/product/PRODUCT_DEFINITION.md)
- [System architecture](docs/architecture/SYSTEM_ARCHITECTURE.md)
- [Data and risk model](docs/architecture/DATA_AND_RISK_MODEL.md)
- [Accepted decisions](docs/decisions/README.md)
- [Master implementation plan](docs/plans/MASTER_PLAN.md)
- [Agent execution protocol](docs/plans/EXECUTION_PROTOCOL.md)
- [Task packet template](.ai/TASK_TEMPLATE.md)
- [Worker report template](.ai/REPORT_TEMPLATE.md)

## Development workflow

`main` is protected conceptually and must never receive direct feature or
documentation pushes. Every change uses a short-lived task branch and a separate
worktree when another task is active. See `AGENTS.md` and the execution protocol
before making changes.

## Toolchain

The repository pins the supported runtime families in the component manifests:

- Go 1.27 is declared by `go.mod`.
- Python 3.14.7 is pinned by `risk-engine/.python-version`; `risk-engine/uv.lock`
  locks the Python project and development dependencies.
- Node.js 24.16.0 is pinned by `apps/web/.node-version`, and npm 12.0.1 is pinned
  by `package.json`; `package-lock.json` locks the workspace dependencies.

From a clean clone, install and verify each component with:

```powershell
go test ./...
uv run --project risk-engine pytest
npm ci
npm test -- --run
npm run build
```

Diagnostic commands report the runtime and component versions:

```powershell
go run ./apps/api/cmd/api --version
go run ./apps/collector/cmd/collector --version
uv run --project risk-engine atlasrisk-risk-engine
npm run diagnostics
```

The Go commands are toolchain-only entry points. Domain behavior, database
access, network ingestion, risk calculations, and UI flows belong to later task
packets.
