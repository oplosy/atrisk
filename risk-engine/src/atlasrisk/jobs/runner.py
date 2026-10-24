"""Long-running risk-worker process for release installations.

Configuration comes only from the environment:

- ``ATLASRISK_DATABASE_URL`` (required): PostgreSQL holding the job queue.
- ``ATLASRISK_WORKER_POLL_SECONDS`` (default 2): idle wait between empty claims.
- ``ATLASRISK_WORKER_LEASE_SECONDS`` (default 120): lease taken on each job.

Logs are JSON lines on stderr. They name events and exception types only, never
job payloads, results, or connection strings.
"""

from __future__ import annotations

import json
import math
import os
import signal
import socket
import sys
import threading
from collections.abc import Callable, Mapping, Sequence
from contextlib import suppress
from typing import Any

from atlasrisk import __version__, diagnostics

from .postgres import PostgresQueueClient
from .worker import JobWorker

ENGINE_VERSION = f"atlasrisk-risk-engine-{__version__}"


def _log(level: str, event: str, **fields: Any) -> None:
    print(json.dumps({"level": level, "event": event, **fields}), file=sys.stderr, flush=True)


def serve(
    connect: Callable[[], Any],
    worker: Any,
    worker_id: str,
    stop: threading.Event,
    *,
    poll_seconds: float = 2.0,
    lease_seconds: float = 120.0,
    retry_seconds: float = 5.0,
    queue_factory: Callable[[Any], Any] = PostgresQueueClient,
) -> None:
    """Claim and run jobs until ``stop`` is set, reconnecting after any failure."""
    while not stop.is_set():
        try:
            connection = connect()
        except Exception as error:
            _log(
                "warning",
                "database unavailable",
                error=type(error).__name__,
                retry_seconds=retry_seconds,
            )
            stop.wait(retry_seconds)
            continue
        try:
            queue = queue_factory(connection)
            while not stop.is_set():
                recover_expired = getattr(queue, "recover_expired", None)
                if callable(recover_expired):
                    recover_expired()
                if not worker.run_claimed_once(queue, worker_id, lease_seconds):
                    stop.wait(poll_seconds)
        except Exception as error:
            _log(
                "error", "job loop failed", error=type(error).__name__, retry_seconds=retry_seconds
            )
            stop.wait(retry_seconds)
        finally:
            with suppress(Exception):
                connection.close()


def stop_on_signals(stop: threading.Event) -> None:
    """Finish the current job, then stop, on SIGTERM (container stop) or SIGINT."""

    def handler(signum: int, frame: object) -> None:
        stop.set()

    signal.signal(signal.SIGTERM, handler)
    signal.signal(signal.SIGINT, handler)


def _seconds(environ: Mapping[str, str], name: str, default: float) -> float:
    raw = environ.get(name, "")
    if raw == "":
        return default
    try:
        value = float(raw)
    except ValueError:
        value = math.nan
    if not math.isfinite(value) or value <= 0:
        raise ValueError(f"{name} must be a positive number of seconds")
    return value


def main(argv: Sequence[str] | None = None, environ: Mapping[str, str] | None = None) -> int:
    """Run the worker; returns the process exit code."""
    args = list(sys.argv[1:] if argv is None else argv)
    env = os.environ if environ is None else environ
    if args == ["--version"]:
        print(diagnostics())
        return 0
    if args:
        print(f"unexpected arguments: {' '.join(args)}", file=sys.stderr)
        return 2
    database_url = env.get("ATLASRISK_DATABASE_URL", "")
    if not database_url:
        print("ATLASRISK_DATABASE_URL is required", file=sys.stderr)
        return 2
    try:
        poll_seconds = _seconds(env, "ATLASRISK_WORKER_POLL_SECONDS", 2.0)
        lease_seconds = _seconds(env, "ATLASRISK_WORKER_LEASE_SECONDS", 120.0)
    except ValueError as error:
        print(error, file=sys.stderr)
        return 2

    import psycopg  # the worker extra; the numerical package does not need it

    stop = threading.Event()
    stop_on_signals(stop)
    worker_id = f"{socket.gethostname()}-{os.getpid()}"
    _log("info", "worker started", worker_id=worker_id, engine_version=ENGINE_VERSION)
    serve(
        lambda: psycopg.connect(database_url, connect_timeout=10),
        JobWorker({}, engine_version=ENGINE_VERSION),
        worker_id,
        stop,
        poll_seconds=poll_seconds,
        lease_seconds=lease_seconds,
    )
    _log("info", "worker stopped", worker_id=worker_id)
    return 0
