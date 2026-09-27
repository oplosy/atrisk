import json
from pathlib import Path

import pytest

from atlasrisk.jobs import (
    JobWorker,
    PermanentJobError,
    canonical_json,
    sha256_json,
    validate_job,
)

FIXTURES = Path(__file__).parents[2] / "test" / "fixtures" / "risk"


def fixture(name: str) -> dict:
    return json.loads((FIXTURES / name).read_text())


def test_golden_job_and_result_hashes_are_stable() -> None:
    job = fixture("golden-job.json")
    result = fixture("golden-result.json")
    assert canonical_json(job) == (
        b'{"idempotency_key":"golden-risk-001","input_snapshot_ids":["snapshot-001","snapshot-002"],'
        b'"kind":"risk.run","payload":{"scenario_version":"v1","values":{"alpha":1.25,"beta":"2.50"}},'
        b'"schema_version":"1.0"}'
    )
    assert sha256_json(job) == "200a097de7bb446df1b0ccb5a7039aed73164f8272c04611ae24ad98d5bec1bb"
    assert sha256_json(result) == "bb078d46aef3aa371c2fbaa47f4253b6bf3e1532c55c2c10f1ac7355db06aee3"


def test_unknown_schema_and_kind_are_permanent() -> None:
    job = fixture("golden-job.json")
    job["schema_version"] = "9.0"
    with pytest.raises(PermanentJobError, match="unsupported") as error:
        validate_job(job)
    assert error.value.code == "ATLAS_UNKNOWN_SCHEMA_VERSION"

    job = fixture("golden-job.json")
    with pytest.raises(PermanentJobError) as error:
        validate_job(job, allowed_kinds={"risk.other"})
    assert error.value.code == "ATLAS_UNKNOWN_JOB_KIND"


def test_worker_returns_versioned_result_and_hash() -> None:
    job = fixture("golden-job.json")
    worker = JobWorker(
        {"risk.run": lambda value: {"echo": value.payload["scenario_version"]}}, "engine-test"
    )
    result, digest = worker.run_once("job-1", job)
    assert result.status == "succeeded"
    assert result.data_quality == "healthy"
    assert result.engine_version == "engine-test"
    assert result.output == {"echo": "v1"}
    assert digest == sha256_json(result.as_dict())
