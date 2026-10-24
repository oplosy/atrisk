import json
import signal
import threading

import pytest

from atlasrisk import __version__
from atlasrisk.jobs import runner


class StopAfter(threading.Event):
    """Records every wait and stops the loop after the given number of waits."""

    def __init__(self, waits: int) -> None:
        super().__init__()
        self.waits: list[float] = []
        self._remaining = waits

    def wait(self, timeout: float | None = None) -> bool:
        self.waits.append(timeout)
        self._remaining -= 1
        if self._remaining <= 0:
            self.set()
        return self.is_set()


class FakeConnection:
    def __init__(self) -> None:
        self.closed = False

    def close(self) -> None:
        self.closed = True


class ScriptedWorker:
    """Returns (or raises) the scripted outcomes of run_claimed_once in order."""

    def __init__(self, *outcomes: object) -> None:
        self.outcomes = list(outcomes)
        self.calls: list[tuple[object, str, float]] = []

    def run_claimed_once(self, queue: object, worker_id: str, lease_seconds: float) -> bool:
        self.calls.append((queue, worker_id, lease_seconds))
        outcome = self.outcomes.pop(0) if self.outcomes else False
        if isinstance(outcome, Exception):
            raise outcome
        return bool(outcome)


class RecoveringQueue:
    def __init__(self) -> None:
        self.recoveries = 0

    def recover_expired(self) -> None:
        self.recoveries += 1


def log_events(capsys: pytest.CaptureFixture[str]) -> list[dict]:
    return [json.loads(line) for line in capsys.readouterr().err.splitlines()]


def test_serve_drains_the_queue_before_waiting() -> None:
    connection = FakeConnection()
    worker = ScriptedWorker(True, True, False)
    stop = StopAfter(waits=1)

    runner.serve(
        lambda: connection,
        worker,
        "worker-1",
        stop,
        poll_seconds=3,
        lease_seconds=90,
        queue_factory=lambda conn: ("queue", conn),
    )

    assert len(worker.calls) == 3
    assert worker.calls[0] == (("queue", connection), "worker-1", 90)
    assert stop.waits == [3]
    assert connection.closed


def test_serve_recovers_expired_jobs_before_claiming() -> None:
    connection = FakeConnection()
    queue = RecoveringQueue()
    worker = ScriptedWorker(False)
    stop = StopAfter(waits=1)

    runner.serve(
        lambda: connection,
        worker,
        "worker-1",
        stop,
        queue_factory=lambda _: queue,
    )

    assert queue.recoveries == 1
    assert worker.calls[0][0] is queue


def test_serve_retries_when_the_database_is_unavailable(
    capsys: pytest.CaptureFixture[str],
) -> None:
    connection = FakeConnection()
    attempts: list[int] = []

    def connect() -> FakeConnection:
        attempts.append(1)
        if len(attempts) == 1:
            raise OSError("could not connect: password=secret-value")
        return connection

    stop = StopAfter(waits=2)
    runner.serve(
        connect,
        ScriptedWorker(False),
        "worker-1",
        stop,
        poll_seconds=2,
        retry_seconds=7,
        queue_factory=lambda _: object(),
    )

    assert len(attempts) == 2
    assert stop.waits == [7, 2]
    events = log_events(capsys)
    assert events[0] == {
        "level": "warning",
        "event": "database unavailable",
        "error": "OSError",
        "retry_seconds": 7,
    }
    assert "secret-value" not in json.dumps(events)


def test_serve_reconnects_after_a_job_loop_failure(capsys: pytest.CaptureFixture[str]) -> None:
    connections: list[FakeConnection] = []

    def connect() -> FakeConnection:
        connections.append(FakeConnection())
        return connections[-1]

    worker = ScriptedWorker(RuntimeError("payload: portfolio line 7"), False)
    stop = StopAfter(waits=2)
    runner.serve(
        connect,
        worker,
        "worker-1",
        stop,
        poll_seconds=2,
        retry_seconds=5,
        queue_factory=lambda _: object(),
    )

    assert len(connections) == 2
    assert all(connection.closed for connection in connections)
    assert stop.waits == [5, 2]
    events = log_events(capsys)
    assert events[0]["event"] == "job loop failed"
    assert events[0]["error"] == "RuntimeError"
    assert "portfolio" not in json.dumps(events)


def test_serve_does_nothing_once_stopped() -> None:
    stop = threading.Event()
    stop.set()
    runner.serve(lambda: pytest.fail("must not connect"), ScriptedWorker(), "worker-1", stop)


def test_stop_signals_end_the_loop() -> None:
    stop = threading.Event()
    previous = {sig: signal.getsignal(sig) for sig in (signal.SIGTERM, signal.SIGINT)}
    try:
        runner.stop_on_signals(stop)
        handler = signal.getsignal(signal.SIGTERM)
        assert callable(handler)
        handler(signal.SIGTERM, None)
        assert stop.is_set()
        assert signal.getsignal(signal.SIGINT) is handler
    finally:
        for sig, old in previous.items():
            signal.signal(sig, old)


def test_main_requires_a_database_url(capsys: pytest.CaptureFixture[str]) -> None:
    assert runner.main([], environ={}) == 2
    assert "ATLASRISK_DATABASE_URL is required" in capsys.readouterr().err


@pytest.mark.parametrize(
    "name", ["ATLASRISK_WORKER_POLL_SECONDS", "ATLASRISK_WORKER_LEASE_SECONDS"]
)
@pytest.mark.parametrize("value", ["0", "-1", "soon", "nan", "inf"])
def test_main_rejects_invalid_intervals(
    name: str, value: str, capsys: pytest.CaptureFixture[str]
) -> None:
    environ = {"ATLASRISK_DATABASE_URL": "postgres://unused", name: value}
    assert runner.main([], environ=environ) == 2
    assert f"{name} must be a positive number of seconds" in capsys.readouterr().err


def test_main_prints_the_version(capsys: pytest.CaptureFixture[str]) -> None:
    assert runner.main(["--version"], environ={}) == 0
    assert capsys.readouterr().out.startswith(f"atlasrisk risk-engine version {__version__}")


def test_engine_version_matches_recorded_results() -> None:
    assert runner.ENGINE_VERSION == f"atlasrisk-risk-engine-{__version__}"
