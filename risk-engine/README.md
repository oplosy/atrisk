# AtlasRisk risk engine

The risk engine is a Python 3.14 package for deterministic numerical analytics.
Its implementation is intentionally empty in the toolchain task; domain behavior
is added by the numbered risk-engine tasks.

```powershell
uv run --project risk-engine atlasrisk-risk-engine
uv run --project risk-engine pytest
```
