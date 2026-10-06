# AtlasRisk risk engine

The risk engine is a Python 3.14 package for deterministic numerical analytics.
Its implementation is intentionally empty in the toolchain task; domain behavior
is added by the numbered risk-engine tasks.

```powershell
uv run --project risk-engine atlasrisk-risk-engine
uv run --project risk-engine pytest
```

Release installations run the queue worker from the `worker` extra; it reads
`ATLASRISK_DATABASE_URL` and stops cleanly on SIGTERM:

```powershell
uv run --project risk-engine --extra worker atlasrisk-risk-worker
```
