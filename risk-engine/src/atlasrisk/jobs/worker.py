"""Pure worker dispatch; persistence/lease ownership remains in PostgreSQL."""

from __future__ import annotations

import threading
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

    def renew(self, job_id: str, worker_id: str, lease_seconds: float) -> bool: ...

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
        renew = getattr(queue, "renew", None)
        stop_renewal = threading.Event()
        lease_lost = threading.Event()
        renewal_thread: threading.Thread | None = None
        if callable(renew):
            interval = max(0.1, lease_seconds / 3)

            def renew_lease() -> None:
                while not stop_renewal.wait(interval):
                    try:
                        if not renew(job_id, worker_id, lease_seconds):
                            lease_lost.set()
                            return
                    except Exception:
                        lease_lost.set()
                        return

            renewal_thread = threading.Thread(target=renew_lease, daemon=True)
            renewal_thread.start()
        failure: tuple[str, str, bool] | None = None
        result: ResultEnvelope | None = None
        try:
            result, _ = self.run_once(job_id, dict(claimed["envelope"]))
        except PermanentJobError as error:
            failure = (error.code, str(error), False)
        except Exception as error:  # adapter decides whether retry policy permits another attempt
            failure = ("ATLAS_WORKER_ERROR", str(error), True)
        finally:
            stop_renewal.set()
            if renewal_thread is not None:
                renewal_thread.join(timeout=max(1.0, lease_seconds / 3))
        if lease_lost.is_set():
            # A stale owner must never attempt a result write or a second failure transition.
            return True
        if failure is not None:
            code, message, retryable = failure
            queue.fail(job_id, worker_id, code, message, retryable=retryable)
            return True
        assert result is not None
        committed = queue.complete(job_id, worker_id, result.as_dict())
        if not committed:
            # The lease expired or changed owners while the handler was running.
            return True
        return True
