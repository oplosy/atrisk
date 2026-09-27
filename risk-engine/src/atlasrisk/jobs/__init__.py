"""Versioned, deterministic risk-job boundary and worker primitives."""

from .canonical import canonical_json, sha256_json
from .contracts import (
    SUPPORTED_SCHEMA_VERSION,
    JobEnvelope,
    PermanentJobError,
    ResultEnvelope,
    RetryableJobError,
    validate_job,
)
from .postgres import PostgresQueueClient
from .worker import JobWorker, execute_job

__all__ = [
    "JobEnvelope",
    "JobWorker",
    "PostgresQueueClient",
    "PermanentJobError",
    "ResultEnvelope",
    "RetryableJobError",
    "SUPPORTED_SCHEMA_VERSION",
    "canonical_json",
    "execute_job",
    "sha256_json",
    "validate_job",
]
