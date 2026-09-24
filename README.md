# AtlasRisk

AtlasRisk is a personal risk operating system that places macroeconomic data,
portfolio exposure, reproducible stress tests, and investment decisions on the
same point-in-time timeline.

AtlasRisk does not generate trade signals and does not execute orders. Its core
question is: **What am I exposed to, which assumptions am I relying on, and what
changes if conditions move against me?**

## Current status

The repository is implementing the ready task packets in `.ai/tasks/`. The local
infrastructure packet provides PostgreSQL 18 and Garage 2.x for later components.

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
  by `package.json`; `package-lock.json` locks the web dependencies.

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

The diagnostics include the actual Go runtime, Python interpreter, Node/npm
runtime, React dependencies, and web tooling versions installed locally; they do
not access the network. `go test ./...` also discovers a Go example package
shipped inside `node_modules/flatted/golang`. It is outside the AtlasRisk Go
module and currently passes without tests. AR-003 should scope its aggregate Go
gate to AtlasRisk-owned packages if dependency contents make that external
package unstable.

The Go commands are toolchain-only entry points. Domain behavior, database
access, network ingestion, risk calculations, and UI flows belong to later task
packets.

## Local infrastructure

Copy `.env.example` to `.env` and keep the services on their loopback-only
ports. Then use the Taskfile targets:

```powershell
task infra-start
task infra-status
task infra-logs
task infra-stop
task test-infra
```

`task test-infra` starts a separate Compose project (`atrisk-test`) with a
separate database, bucket, ports, and named volumes. It proves the exact S3
`PutObject`, `GetObject`, `HeadObject`, and `ListObjectsV2` operations used by
the raw archive boundary, then removes the test volumes. Each Compose project
also gets its own persistent `garage-rpc-secret` volume; the secret is generated
on first start and is never stored in tracked configuration.

`task infra-reset` is destructive: it removes the development containers and
named volumes, including all local PostgreSQL and Garage data and the Garage RPC
secret. Use it only when that data can be discarded. Starting the project again
after reset generates a new RPC secret. The test project always uses its own
secret volume and is removed by `task test-infra`.
