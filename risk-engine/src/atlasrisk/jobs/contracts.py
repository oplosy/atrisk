"""Validation for the versioned JSON job boundary."""

from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Any

SUPPORTED_SCHEMA_VERSION = "1.0"
UNKNOWN_SCHEMA_VERSION_CODE = "ATLAS_UNKNOWN_SCHEMA_VERSION"
UNKNOWN_JOB_KIND_CODE = "ATLAS_UNKNOWN_JOB_KIND"
_KIND = re.compile(r"^[a-z][a-z0-9_.-]*$")


class PermanentJobError(ValueError):
    """A job cannot be retried without changing its input or contract."""

    def __init__(self, code: str, message: str, details: dict[str, Any] | None = None):
        super().__init__(message)
        self.code = code
        self.details = details or {}


class RetryableJobError(RuntimeError):
    """A transient execution failure that may be retried under the lease policy."""


@dataclass(frozen=True)
class JobEnvelope:
    kind: str
    schema_version: str
    idempotency_key: str
    input_snapshot_ids: tuple[str, ...]
    payload: dict[str, Any]

    @classmethod
    def from_dict(cls, value: dict[str, Any]) -> JobEnvelope:
        return cls(
            kind=value["kind"],
            schema_version=value["schema_version"],
            idempotency_key=value["idempotency_key"],
            input_snapshot_ids=tuple(value["input_snapshot_ids"]),
            payload=dict(value["payload"]),
        )


@dataclass(frozen=True)
class ResultEnvelope:
    job_id: str
    schema_version: str
    status: str
    input_snapshot_ids: tuple[str, ...]
    data_quality: str
    engine_version: str
    output: dict[str, Any]

    def as_dict(self) -> dict[str, Any]:
        return {
            "job_id": self.job_id,
            "schema_version": self.schema_version,
            "status": self.status,
            "input_snapshot_ids": list(self.input_snapshot_ids),
            "data_quality": self.data_quality,
            "engine_version": self.engine_version,
            "output": self.output,
        }


def validate_job(value: Any, *, allowed_kinds: set[str] | None = None) -> JobEnvelope:
    if not isinstance(value, dict):
        raise PermanentJobError("ATLAS_INVALID_JOB", "job must be an object")
    version = value.get("schema_version")
    if version != SUPPORTED_SCHEMA_VERSION:
        raise PermanentJobError(
            UNKNOWN_SCHEMA_VERSION_CODE,
            "unsupported job schema version",
            {
                "kind": value.get("kind", "unknown"),
                "schema_version": version,
                "supported_versions": [SUPPORTED_SCHEMA_VERSION],
            },
        )
    required = {"kind", "schema_version", "idempotency_key", "input_snapshot_ids", "payload"}
    if set(value) != required:
        raise PermanentJobError("ATLAS_INVALID_JOB", "job fields do not match the envelope")
    if not isinstance(value["kind"], str) or not _KIND.fullmatch(value["kind"]):
        raise PermanentJobError("ATLAS_INVALID_JOB", "job kind is invalid")
    if allowed_kinds is not None and value["kind"] not in allowed_kinds:
        raise PermanentJobError(
            UNKNOWN_JOB_KIND_CODE, "unsupported job kind", {"kind": value["kind"]}
        )
    key = value["idempotency_key"]
    ids = value["input_snapshot_ids"]
    if not isinstance(key, str) or not 1 <= len(key) <= 255:
        raise PermanentJobError("ATLAS_INVALID_JOB", "idempotency key is invalid")
    if (
        not isinstance(ids, list)
        or any(not isinstance(item, str) or not item for item in ids)
        or len(ids) != len(set(ids))
    ):
        raise PermanentJobError(
            "ATLAS_INVALID_JOB", "snapshot IDs must be unique non-empty strings"
        )
    if not isinstance(value["payload"], dict):
        raise PermanentJobError("ATLAS_INVALID_JOB", "payload must be an object")
    return JobEnvelope.from_dict(value)
