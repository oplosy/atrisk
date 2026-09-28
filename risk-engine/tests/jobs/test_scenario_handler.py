from __future__ import annotations

import json
from datetime import date, timedelta
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


def test_scenario_job_derives_pre_metrics_from_server_sealed_ar302_inputs() -> None:
    payload = _job_payload()
    days = []
    current = date(2025, 1, 1)
    while len(days) < 65:
        if current.weekday() < 5:
            days.append(current.isoformat())
        current += timedelta(days=1)
    payload["positions"] = [
        {**payload["positions"][0], "instrument_id": "a", "asset_class": "crypto"},
        {
            **payload["positions"][0],
            "snapshot_line_id": "golden-line-2",
            "instrument_id": "b",
            "asset_class": "equity",
        },
    ]
    payload["pre_metrics"] = {
        "data_quality": "blocked",
        "reason": "PRE_SHOCK_METRICS_INPUT_HISTORY_UNAVAILABLE",
    }
    payload["metric_inputs"] = {
        "price_history": {
            "a": {day: str(100 + index) for index, day in enumerate(days)},
            "b": {day: str(200 + index * 2) for index, day in enumerate(days)},
        },
        "nav_history": {day: str(300 + index * 3) for index, day in enumerate(days)},
        "signed_exposures": {"a": "100", "b": "200"},
        "calendar": "business_daily",
    }
    result, _ = JobWorker({}, "scenario-engine-test").run_once(
        "job-derived-metrics", _envelope(payload)
    )
    assert result.status == "succeeded"
    assert result.data_quality == "healthy"
    assert result.output["pre_metrics"]["data_quality"] == "valid"
    assert result.output["pre_metrics"]["volatility"]["a"]["state"] == "valid"


def test_scenario_job_keeps_explicit_history_gap_blocked() -> None:
    payload = _job_payload()
    payload["pre_metrics"] = {
        "data_quality": "blocked",
        "reason": "PRE_SHOCK_METRICS_INPUT_HISTORY_UNAVAILABLE",
    }
    payload["metric_inputs"] = {}
    result, _ = JobWorker({}, "scenario-engine-test").run_once(
        "job-missing-metric-history", _envelope(payload)
    )
    assert result.status == "blocked"
    assert result.data_quality == "blocked"
    assert result.output["state"] == "blocked"


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
