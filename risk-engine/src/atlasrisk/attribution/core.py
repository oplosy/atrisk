"""Bounded, deterministic Shapley attribution for scenario revaluation output.

The scenario engine remains the source of truth for valuation.  This module only
replays that sealed payload with subsets of its declared shocks and allocates the
resulting P&L.  Decimal values are used at the reconciliation boundary; no
floating-point conversion is needed for the bounded V1 calculation.
"""

from __future__ import annotations

from collections.abc import Callable, Iterable, Mapping, Sequence
from copy import deepcopy
from decimal import Decimal, InvalidOperation, localcontext
from itertools import combinations
from math import factorial
from typing import Any

from atlasrisk.scenarios import evaluate_scenario, quantize_storage, storage_text

ATTRIBUTION_METHOD = "shapley"
ATTRIBUTION_VERSION = "1.0.0"
MAX_SUPPORTED_FACTORS = 8
DEFAULT_TOLERANCE = Decimal("0.00000001")


class AttributionValidationError(ValueError):
    """Attribution input is malformed or exceeds the deterministic V1 bound."""


def _decimal(value: Any, field: str) -> Decimal:
    if isinstance(value, bool) or not isinstance(value, str | int | Decimal | float):
        raise AttributionValidationError(f"{field} must be numeric")
    try:
        result = Decimal(str(value))
    except (InvalidOperation, ValueError) as error:
        raise AttributionValidationError(f"{field} must be finite") from error
    if not result.is_finite():
        raise AttributionValidationError(f"{field} must be finite")
    return result


def _normalise_factors(factors: Iterable[str] | Mapping[str, Any]) -> tuple[str, ...]:
    names = tuple(sorted(factors.keys() if isinstance(factors, Mapping) else factors))
    if any(not isinstance(name, str) or not name for name in names):
        raise AttributionValidationError("factor names must be non-empty strings")
    if len(set(names)) != len(names):
        raise AttributionValidationError("factor names must be unique")
    if len(names) > MAX_SUPPORTED_FACTORS:
        raise AttributionValidationError(
            f"attribution supports at most {MAX_SUPPORTED_FACTORS} factors"
        )
    return names


def _subsets(names: Sequence[str]) -> Iterable[frozenset[str]]:
    for size in range(len(names) + 1):
        yield from (frozenset(item) for item in combinations(names, size))


def shapley_allocate(
    factors: Iterable[str] | Mapping[str, Any],
    value_function: Callable[[frozenset[str]], Decimal | str | int | float],
    *,
    max_factors: int = MAX_SUPPORTED_FACTORS,
) -> dict[str, Decimal]:
    """Allocate a scalar utility by exact subset enumeration.

    ``value_function`` receives the included factor names.  The empty subset is
    the baseline and is deliberately retained by callers as a visible residual
    if it is non-zero.  The implementation enumerates every subset, so its
    runtime is bounded before any numerical work starts.
    """

    names = _normalise_factors(factors)
    if max_factors < 0 or len(names) > max_factors:
        raise AttributionValidationError(f"attribution supports at most {max_factors} factors")
    count = len(names)
    cache: dict[frozenset[str], Decimal] = {}
    for subset in _subsets(names):
        cache[subset] = _decimal(value_function(subset), f"value[{sorted(subset)}]")

    if not names:
        return {}
    denominator = factorial(count)
    contributions: dict[str, Decimal] = {}
    with localcontext() as context:
        context.prec = 50
        for factor in names:
            contribution = Decimal(0)
            remaining = [name for name in names if name != factor]
            for size in range(len(remaining) + 1):
                weight = Decimal(factorial(size) * factorial(count - size - 1)) / denominator
                for subset_values in combinations(remaining, size):
                    subset = frozenset(subset_values)
                    contribution += weight * (cache[subset | {factor}] - cache[subset])
            contributions[factor] = +contribution
    return contributions


def _factor_specs(version: Mapping[str, Any]) -> dict[str, dict[str, Any]]:
    shocks = version.get("shocks")
    if not isinstance(shocks, Mapping):
        raise AttributionValidationError("scenario shocks must be an object")
    specs: dict[str, dict[str, Any]] = {}

    def add_map(field: str, prefix: str, neutral: Any) -> None:
        values = shocks.get(field, {})
        if values is None:
            return
        if not isinstance(values, Mapping):
            raise AttributionValidationError(f"shocks.{field} must be an object")
        for key in sorted(values):
            if not isinstance(key, str) or not key:
                raise AttributionValidationError(f"shocks.{field} keys must be non-empty strings")
            specs[f"{prefix}:{key}"] = {
                "field": field,
                "key": key,
                "value": values[key],
                "neutral": neutral,
            }

    add_map("asset_class_returns", "asset_return", "0")
    add_map("yield_shifts_bps", "yield_shift", 0)
    add_map("fx_pair_changes", "fx_change", "0")
    add_map("volatility_multipliers", "volatility_multiplier", "1")
    if shocks.get("correlation_target") is not None:
        specs["correlation_target"] = {
            "field": "correlation_target",
            "value": shocks["correlation_target"],
            "neutral": None,
        }
    if shocks.get("correlation_blend") is not None:
        specs["correlation_blend"] = {
            "field": "correlation_blend",
            "value": shocks["correlation_blend"],
            "neutral": "0",
        }
    return specs


def _payload_for_subset(
    payload: Mapping[str, Any], specs: Mapping[str, Mapping[str, Any]], subset: frozenset[str]
) -> dict[str, Any]:
    candidate = deepcopy(dict(payload))
    version = candidate["scenario_version"]
    shocks = version["shocks"]
    for name, spec in specs.items():
        value = spec["value"] if name in subset else spec["neutral"]
        field = spec["field"]
        if "key" in spec:
            shocks.setdefault(field, {})[spec["key"]] = value
        else:
            shocks[field] = value
    return candidate


def _result_value(result: Mapping[str, Any], currency: str) -> Decimal:
    field = "portfolio_pnl_try" if currency == "try" else "portfolio_pnl_usd"
    raw = result.get(field)
    if raw is not None:
        return _decimal(raw, field)
    # A blocked scenario has no portfolio total by design.  Sum only valid
    # position lines so the supported mapped amount remains inspectable.
    total = Decimal(0)
    for position in result.get("positions", []):
        if isinstance(position, Mapping) and position.get("state") == "valid":
            total += _decimal(
                position.get("pnl_try" if currency == "try" else "pnl_usd", 0), "position pnl"
            )
    return total


def _serialise_decimal(value: Decimal | None, field: str = "attribution value") -> str | None:
    return None if value is None else storage_text(value, field)


def attribute_scenario(
    payload: Mapping[str, Any],
    *,
    currency: str = "try",
    tolerance: str | int | Decimal = DEFAULT_TOLERANCE,
    max_factors: int = MAX_SUPPORTED_FACTORS,
    scenario_result: Mapping[str, Any] | None = None,
) -> dict[str, Any]:
    """Return deterministic factor and position attribution for an AR-303 payload."""

    if currency not in {"try", "usd"}:
        raise AttributionValidationError("currency must be try or usd")
    if not isinstance(payload, Mapping) or not isinstance(payload.get("scenario_version"), Mapping):
        raise AttributionValidationError("scenario payload must contain scenario_version")
    if not isinstance(payload.get("positions"), list):
        raise AttributionValidationError("scenario payload must contain positions")
    tolerance_value = quantize_storage(_decimal(tolerance, "tolerance"), "tolerance")
    if tolerance_value < 0:
        raise AttributionValidationError("tolerance must be non-negative")
    specs = _factor_specs(payload["scenario_version"])
    names = _normalise_factors(specs)
    if len(names) > max_factors:
        raise AttributionValidationError(f"attribution supports at most {max_factors} factors")

    results: dict[frozenset[str], dict[str, Any]] = {}

    def evaluate_subset(subset: frozenset[str]) -> dict[str, Any]:
        if subset not in results:
            results[subset] = evaluate_scenario(_payload_for_subset(payload, specs, subset))
        return results[subset]

    reference = (
        dict(scenario_result) if scenario_result is not None else evaluate_scenario(dict(payload))
    )
    factor_values = shapley_allocate(
        names,
        lambda subset: _result_value(evaluate_subset(subset), currency),
        max_factors=max_factors,
    )
    baseline = _result_value(evaluate_subset(frozenset()), currency)
    baseline = quantize_storage(baseline, "baseline_pnl")
    factor_values = {
        name: quantize_storage(value, f"factor_contributions[{name}]")
        for name, value in factor_values.items()
    }
    mapped_total = quantize_storage(
        baseline + sum(factor_values.values(), Decimal(0)), "mapped_total"
    )

    def position_value(index: int, subset: frozenset[str]) -> Decimal:
        result = evaluate_subset(subset)
        positions = result.get("positions", [])
        if index >= len(positions) or not isinstance(positions[index], Mapping):
            return Decimal(0)
        position = positions[index]
        if position.get("state") != "valid":
            return Decimal(0)
        return _decimal(
            position.get("pnl_try" if currency == "try" else "pnl_usd", 0), "position pnl"
        )

    position_rows: list[dict[str, Any]] = []
    for index, original in enumerate(payload["positions"]):
        if not isinstance(original, Mapping):
            raise AttributionValidationError("positions must contain objects")
        contributions = shapley_allocate(
            names,
            lambda subset, index=index: position_value(index, subset),
            max_factors=max_factors,
        )
        original_position = (
            reference.get("positions", [])[index]
            if index < len(reference.get("positions", []))
            else {}
        )
        total = (
            _decimal(
                original_position.get("pnl_try" if currency == "try" else "pnl_usd"), "position pnl"
            )
            if isinstance(original_position, Mapping) and original_position.get("state") == "valid"
            else None
        )
        position_baseline = quantize_storage(
            position_value(index, frozenset()), f"position[{index}].baseline_pnl"
        )
        contributions = {
            name: quantize_storage(value, f"position[{index}].factor[{name}]")
            for name, value in contributions.items()
        }
        total = (
            quantize_storage(total, f"position[{index}].total_pnl")
            if total is not None
            else None
        )
        allocated = quantize_storage(
            position_baseline + sum(contributions.values(), Decimal(0)),
            f"position[{index}].allocated_pnl",
        )
        position_residual = (
            quantize_storage(total - allocated, f"position[{index}].residual")
            if total is not None
            else None
        )
        position_rows.append(
            {
                "snapshot_line_id": original.get("snapshot_line_id"),
                "instrument_id": original.get("instrument_id"),
                "state": original_position.get("state", "blocked")
                if isinstance(original_position, Mapping)
                else "blocked",
                "total_pnl": _serialise_decimal(total, f"position[{index}].total_pnl"),
                "factor_contributions": [
                    {
                        "factor": name,
                        "contribution": _serialise_decimal(
                            contributions[name], f"position[{index}].factor[{name}]"
                        ),
                    }
                    for name in names
                ],
                "residual": _serialise_decimal(
                    position_residual, f"position[{index}].residual"
                ),
            }
        )

    raw_total = reference.get("portfolio_pnl_try" if currency == "try" else "portfolio_pnl_usd")
    total = (
        quantize_storage(_decimal(raw_total, "portfolio pnl"), "total_pnl")
        if raw_total is not None
        else None
    )
    residual = (
        quantize_storage(total - mapped_total, "interaction_residual")
        if total is not None
        else None
    )
    unmapped = list(reference.get("unmapped_instruments", []))
    for index, position in enumerate(reference.get("positions", [])):
        if isinstance(position, Mapping) and position.get("state") != "valid":
            if not any(
                item.get("instrument_id") == position.get("instrument_id")
                for item in unmapped
                if isinstance(item, Mapping)
            ):
                unmapped.append(
                    {
                        "instrument_id": position.get("instrument_id"),
                        "reason_code": "UNSUPPORTED_POSITION",
                        "snapshot_line_id": position.get("snapshot_line_id"),
                    }
                )
    return {
        "method": ATTRIBUTION_METHOD,
        "method_version": ATTRIBUTION_VERSION,
        "currency": currency,
        "tolerance": _serialise_decimal(tolerance_value, "tolerance"),
        "factor_count": len(names),
        "subset_count": 2 ** len(names),
        "permutation_count": factorial(len(names)),
        "maximum_supported_factors": max_factors,
        "state": reference.get("state", "blocked"),
        "total_pnl": _serialise_decimal(total, "total_pnl"),
        "baseline_pnl": _serialise_decimal(baseline, "baseline_pnl"),
        "factor_contributions": [
            {
                "factor": name,
                "contribution": _serialise_decimal(
                    factor_values[name], f"factor_contributions[{name}]"
                ),
            }
            for name in names
        ],
        "interaction_residual": _serialise_decimal(residual, "interaction_residual"),
        "position_contributions": position_rows,
        "unmapped_positions": sorted(unmapped, key=lambda item: str(item.get("instrument_id", ""))),
        "reconciles": residual is not None and abs(residual) <= tolerance_value,
    }


calculate_attribution = attribute_scenario
