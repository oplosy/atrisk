"""Pure worker dispatch; persistence/lease ownership remains in PostgreSQL."""

from __future__ import annotations

from collections.abc import Callable, Mapping
from typing import Any

from .canonical import sha256_json
from .contracts import JobEnvelope, PermanentJobError, ResultEnvelope, validate_job

Handler = Callable[[JobEnvelope], Mapping[str, Any]]


def execute_job(
    job_id: str,
    value: dict[str, Any],
    handlers: Mapping[str, Handler],
    *,
    engine_version: str,
) -> tuple[ResultEnvelope, str]:
    job = validate_job(value, allowed_kinds=set(handlers))
    handler = handlers.get(job.kind)
    if handler is None:
        raise PermanentJobError(
            "ATLAS_UNKNOWN_JOB_KIND", "unsupported job kind", {"kind": job.kind}
        )
    output = dict(handler(job))
    result = ResultEnvelope(
        job_id=job_id,
        schema_version=job.schema_version,
        status="succeeded",
        input_snapshot_ids=job.input_snapshot_ids,
        data_quality="healthy",
        engine_version=engine_version,
        output=output,
    )
    return result, sha256_json(result.as_dict())


class JobWorker:
    """Dispatches claimed envelopes and leaves commit/retry decisions to the queue."""

    def __init__(self, handlers: Mapping[str, Handler], engine_version: str):
        self._handlers = dict(handlers)
        self.engine_version = engine_version

    def run_once(self, job_id: str, value: dict[str, Any]) -> tuple[ResultEnvelope, str]:
        return execute_job(job_id, value, self._handlers, engine_version=self.engine_version)
