"""Deterministic explainable stress-factor attribution."""

from .core import (
    ATTRIBUTION_METHOD,
    ATTRIBUTION_VERSION,
    MAX_SUPPORTED_FACTORS,
    AttributionValidationError,
    attribute_scenario,
    calculate_attribution,
    shapley_allocate,
)

__all__ = [
    "ATTRIBUTION_METHOD",
    "ATTRIBUTION_VERSION",
    "MAX_SUPPORTED_FACTORS",
    "AttributionValidationError",
    "attribute_scenario",
    "calculate_attribution",
    "shapley_allocate",
]
