from __future__ import annotations

import json
from pathlib import Path

import pytest

from atlasrisk.jobs import JobWorker, PermanentJobError

ROOT = Path(__file__).resolve().parents[3]
SCENARIO_FIXTURE = ROOT / "test" / "fixtures" / "risk" / "scenario-revaluation-golden.json"


def _job_payload() -> dict:
    return json.loads(SCENARIO_FIXTURE.read_text(encoding="utf-8"))["payload"]


def _envelope(payload: dict) -> dict:
    return {
        "kind": "scenario.revalue",
        "schema_version": "1.0",
        "idempotency_key": "scenario-job-1",
        "input_snapshot_ids": ["snapshot-1"],
        "payload": payload,
    }


def test_scenario_job_is_registered_and_preserves_blocked_quality() -> None:
    valid, _ = JobWorker({}, "scenario-engine-test").run_once("job-1", _envelope(_job_payload()))
    assert valid.status == "succeeded"
    assert valid.data_quality == "healthy"

    payload = _job_payload()
    payload.pop("pre_metrics")
    blocked, _ = JobWorker({}, "scenario-engine-test").run_once("job-2", _envelope(payload))
    assert blocked.status == "blocked"
    assert blocked.data_quality == "blocked"
    assert blocked.output["state"] == "blocked"


def test_degraded_scenario_quality_is_not_wrapped_as_healthy() -> None:
    payload = _job_payload()
    payload["scenario_version"]["assumptions"]["coverage_policy"] = "degrade"
    payload["positions"][0]["asset_class"] = "unmapped"
    result, _ = JobWorker({}, "scenario-engine-test").run_once("job-3", _envelope(payload))
    assert result.status == "degraded"
    assert result.data_quality == "degraded"


def test_malformed_scenario_position_is_a_permanent_contract_error() -> None:
    payload = _job_payload()
    payload["positions"] = [None]
    with pytest.raises(PermanentJobError) as error:
        JobWorker({}, "scenario-engine-test").run_once("job-4", _envelope(payload))
    assert error.value.code == "ATLAS_INVALID_SCENARIO"


def test_invalid_pre_metric_state_is_a_permanent_contract_error() -> None:
    payload = _job_payload()
    payload["pre_metrics"]["volatility"]["crypto"]["state"] = "bogus"
    with pytest.raises(PermanentJobError) as error:
        JobWorker({}, "scenario-engine-test").run_once("job-5", _envelope(payload))
    assert error.value.code == "ATLAS_INVALID_SCENARIO"
