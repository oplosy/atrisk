"""Worker adapter for the immutable scenario-revaluation job kind."""

from __future__ import annotations

from typing import Any

from atlasrisk.scenarios import ScenarioValidationError, evaluate_scenario

from .contracts import JobEnvelope, PermanentJobError


def handle_scenario_revaluation(job: JobEnvelope) -> dict[str, Any]:
    try:
        return evaluate_scenario(job.payload)
    except ScenarioValidationError as error:
        raise PermanentJobError("ATLAS_INVALID_SCENARIO", str(error)) from error
