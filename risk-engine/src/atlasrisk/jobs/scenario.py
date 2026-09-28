"""Worker adapter for the immutable scenario-revaluation job kind."""

from __future__ import annotations

from datetime import date
from typing import Any

from atlasrisk.attribution import AttributionValidationError, attribute_scenario
from atlasrisk.metrics import calculate_metrics
from atlasrisk.scenarios import ScenarioValidationError, evaluate_scenario

from .contracts import JobEnvelope, PermanentJobError


def _derive_pre_metrics(metric_inputs: Any) -> dict[str, Any]:
    if not isinstance(metric_inputs, dict):
        raise ScenarioValidationError("server metric inputs are unavailable")
    price_history = metric_inputs.get("price_history")
    nav_history = metric_inputs.get("nav_history")
    signed_exposures = metric_inputs.get("signed_exposures")
    calendar = metric_inputs.get("calendar", "business_daily")
    if (
        not isinstance(price_history, dict)
        or not isinstance(nav_history, dict)
        or not isinstance(signed_exposures, dict)
    ):
        raise ScenarioValidationError("server metric inputs are incomplete")
    try:
        prices = {
            str(instrument): {
                date.fromisoformat(str(observed_at)): float(value)
                for observed_at, value in values.items()
            }
            for instrument, values in price_history.items()
        }
        nav = {
            date.fromisoformat(str(observed_at)): float(value)
            for observed_at, value in nav_history.items()
        }
        exposures = {
            str(instrument): float(value) for instrument, value in signed_exposures.items()
        }
        return calculate_metrics(prices, nav, exposures, calendar=calendar)
    except (TypeError, ValueError, AttributeError) as error:
        raise ScenarioValidationError("server metric inputs are invalid") from error


def handle_scenario_revaluation(job: JobEnvelope) -> dict[str, Any]:
    try:
        payload = dict(job.payload)
        pre_metrics = payload.get("pre_metrics")
        if (
            isinstance(pre_metrics, dict)
            and pre_metrics.get("reason") == "PRE_SHOCK_METRICS_INPUT_HISTORY_UNAVAILABLE"
        ):
            metric_inputs = payload.get("metric_inputs")
            if metric_inputs:
                payload["pre_metrics"] = _derive_pre_metrics(metric_inputs)
        result = evaluate_scenario(payload)
        try:
            attribution = attribute_scenario(payload, scenario_result=result)
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
        result["input_provenance"] = payload.get("sealed_input", {})
        return result
    except ScenarioValidationError as error:
        raise PermanentJobError("ATLAS_INVALID_SCENARIO", str(error)) from error
