"""Worker adapter for the immutable scenario-revaluation job kind."""

from __future__ import annotations

from typing import Any

from atlasrisk.attribution import AttributionValidationError, attribute_scenario
from atlasrisk.scenarios import ScenarioValidationError, evaluate_scenario

from .contracts import JobEnvelope, PermanentJobError


def handle_scenario_revaluation(job: JobEnvelope) -> dict[str, Any]:
    try:
        result = evaluate_scenario(job.payload)
        try:
            attribution = attribute_scenario(job.payload, scenario_result=result)
        except AttributionValidationError as error:
            attribution = {
                "method": "shapley",
                "method_version": "1.0.0",
                "state": result.get("state", "blocked"),
                "reconciles": False,
                "interaction_residual": None,
                "reason_code": "ATTRIBUTION_UNAVAILABLE",
                "reason": str(error),
            }
        if result.get("state") == "valid" and not attribution.get("reconciles", False):
            result["state"] = "degraded"
            result.setdefault("reason_codes", []).append("ATTRIBUTION_RESIDUAL_EXCEEDS_TOLERANCE")
        result["attribution"] = attribution
        result["input_provenance"] = job.payload.get("sealed_input", {})
        return result
    except ScenarioValidationError as error:
        raise PermanentJobError("ATLAS_INVALID_SCENARIO", str(error)) from error
