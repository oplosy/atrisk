# Architecture Source Evidence

These links support decisions that depend on current external behavior. Product
decisions remain in the ADR register.

- FRED/ALFRED observations expose real-time periods, vintage dates, and output
  modes for revised/initial observations:
  https://fred.stlouisfed.org/docs/api/fred/series_observations.html
- ALFRED archives the real-time period when values were released and revised:
  https://fred.stlouisfed.org/docs/api/fred/alfred.html
- TCMB documents EVDS web-service series, frequency, aggregation, and API-key
  parameters:
  https://evds2.tcmb.gov.tr/help/videos/EVDS_Web_Service_Usage_Guide.pdf
- Binance documents a public market-data-only base endpoint and request limits:
  https://developers.binance.com/en/docs/products/spot/rest-api
- PostgreSQL documents native partitioning and `SKIP LOCKED`; `SKIP LOCKED` is
  appropriate for queue-like consumers, not general consistent reads:
  https://www.postgresql.org/docs/18/ddl-partitioning.html
  https://www.postgresql.org/about/featurematrix/detail/skip-locked-clause/
- PostgreSQL 18 is a supported major line and current minor upgrades are advised:
  https://www.postgresql.org/support/versioning/
- Timescale hypertable unique indexes must contain all partitioning columns,
  supporting the decision not to introduce the extension before it is needed:
  https://docs.timescale.com/use-timescale/latest/hypertables/hypertables-and-unique-indexes/
- Go release/support policy and the 1.27 release line:
  https://go.dev/doc/devel/release
- Python 3.14 documentation:
  https://docs.python.org/3/
- React current major documentation:
  https://react.dev/versions
- `uv` lockfiles capture exact cross-platform dependency resolutions and are
  intended for version control:
  https://docs.astral.sh/uv/concepts/projects/layout/
- Task runs the same Taskfile on Windows, Linux, and macOS and provides official
  installation paths for local machines and GitHub Actions:
  https://taskfile.dev/
  https://taskfile.dev/docs/installation
- Garage is a lightweight S3-compatible store intended for small self-hosted
  deployments and publishes container images/releases:
  https://github.com/deuxfleurs-org/garage
- MinIO Community is now source-only, so AtlasRisk does not depend on its legacy
  unmaintained binary/container releases:
  https://charts.min.io/
- Codex subagent roles, model/effort overrides, project agent files, and parallel
  write cautions:
  https://learn.chatgpt.com/docs/agent-configuration/subagents
