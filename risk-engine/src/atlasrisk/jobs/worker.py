"""Pure worker dispatch; persistence/lease ownership remains in PostgreSQL."""

from __future__ import annotations

from collections.abc import Callable, Mapping
from typing import Any, Protocol

from .canonical import sha256_json
from .contracts import JobEnvelope, PermanentJobError, ResultEnvelope, validate_job
from .scenario import handle_scenario_revaluation

Handler = Callable[[JobEnvelope], Mapping[str, Any]]


class QueueClient(Protocol):
    """Adapter for the Go-owned PostgreSQL claim/complete boundary."""

    def claim(self, worker_id: str, lease_seconds: float) -> Mapping[str, Any] | None: ...

    def complete(self, job_id: str, worker_id: str, result: Mapping[str, Any]) -> bool: ...

    def fail(
        self, job_id: str, worker_id: str, code: str, message: str, retryable: bool
    ) -> bool: ...


def execute_job(
    job_id: str,
    value: dict[str, Any],
    handlers: Mapping[str, Handler],
    *,
    engine_version: str,
) -> tuple[ResultEnvelope, str]:
    available_handlers: dict[str, Handler] = {"scenario.revalue": handle_scenario_revaluation}
    available_handlers.update(handlers)
    job = validate_job(value, allowed_kinds=set(available_handlers))
    handler = available_handlers.get(job.kind)
    if handler is None:
        raise PermanentJobError(
            "ATLAS_UNKNOWN_JOB_KIND", "unsupported job kind", {"kind": job.kind}
        )
    output = dict(handler(job))
    scenario_state = output.get("state")
    quality_by_state = {"valid": "healthy", "degraded": "degraded", "blocked": "blocked"}
    data_quality = quality_by_state.get(scenario_state, "healthy")
    result_status = scenario_state if scenario_state in {"degraded", "blocked"} else "succeeded"
    result = ResultEnvelope(
        job_id=job_id,
        schema_version=job.schema_version,
        status=result_status,
        input_snapshot_ids=job.input_snapshot_ids,
        data_quality=data_quality,
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

    def run_claimed_once(
        self, queue: QueueClient, worker_id: str, lease_seconds: float = 60
    ) -> bool:
        """Claim one durable job, execute it, and commit or fail it through the queue adapter."""
        claimed = queue.claim(worker_id, lease_seconds)
        if claimed is None:
            return False
        job_id = str(claimed["id"])
        try:
            result, _ = self.run_once(job_id, dict(claimed["envelope"]))
        except PermanentJobError as error:
            queue.fail(job_id, worker_id, error.code, str(error), retryable=False)
            return True
        except Exception as error:  # adapter decides whether retry policy permits another attempt
            queue.fail(job_id, worker_id, "ATLAS_WORKER_ERROR", str(error), retryable=True)
            return True
        queue.complete(job_id, worker_id, result.as_dict())
        return True
