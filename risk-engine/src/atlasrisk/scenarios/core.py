"""Pure scenario template versioning and portfolio revaluation."""

from __future__ import annotations

from copy import deepcopy
from decimal import Decimal, InvalidOperation, localcontext
from typing import Any

SCENARIO_ENGINE_VERSION = "1.0.0"

_TEMPLATES: dict[str, dict[str, Any]] = {
    "try_depreciation": {
        "units": {"fx_pair_changes": "relative", "yield_shifts_bps": "basis_points"},
        "shocks": {
            "fx_pair_changes": {"USD/TRY": "0.25"},
            "asset_class_returns": {},
            "yield_shifts_bps": {},
            "volatility_multipliers": {},
            "correlation_target": None,
            "correlation_blend": None,
        },
        "assumptions": {"coverage_policy": "block", "pnl_tolerance": "0.00000001"},
    },
    "rates_up": {
        "units": {"fx_pair_changes": "relative", "yield_shifts_bps": "basis_points"},
        "shocks": {
            "fx_pair_changes": {},
            "asset_class_returns": {},
            "yield_shifts_bps": {"TRY": 500, "USD": 200},
            "volatility_multipliers": {},
            "correlation_target": None,
            "correlation_blend": None,
        },
        "assumptions": {
            "coverage_policy": "block",
            "pnl_tolerance": "0.00000001",
            "convexity_when_absent": "0",
        },
    },
    "risk_off": {
        "units": {"fx_pair_changes": "relative", "yield_shifts_bps": "basis_points"},
        "shocks": {
            "fx_pair_changes": {},
            "asset_class_returns": {
                "crypto": "-0.40",
                "equity": "-0.20",
                "fixed_rate_bond": "0",
            },
            "yield_shifts_bps": {},
            "volatility_multipliers": {"*": "2.0"},
            "correlation_target": "0.75",
            "correlation_blend": "0.50",
        },
        "assumptions": {"coverage_policy": "block", "pnl_tolerance": "0.00000001"},
    },
}


class ScenarioValidationError(ValueError):
    """Scenario input is malformed and cannot be revalued deterministically."""


def _decimal(value: Any, field: str) -> Decimal:
    if not isinstance(value, str | int | Decimal) or isinstance(value, bool):
        raise ScenarioValidationError(f"{field} must be a decimal string or integer")
    try:
        result = Decimal(str(value))
    except InvalidOperation as error:
        raise ScenarioValidationError(f"{field} is not a decimal") from error
    if not result.is_finite():
        raise ScenarioValidationError(f"{field} must be finite")
    return result


def create_template_version(
    template_key: str, *, scenario_id: str, version: int = 1
) -> dict[str, Any]:
    """Return a fresh immutable-by-convention version payload for a V1 template."""
    if template_key not in _TEMPLATES:
        raise ScenarioValidationError("unknown scenario template")
    if not scenario_id or version < 1:
        raise ScenarioValidationError("scenario ID and positive version are required")
    return {
        "scenario_id": scenario_id,
        "version": version,
        "template_key": template_key,
        **deepcopy(_TEMPLATES[template_key]),
    }


def _default_fx_paths(currency: str) -> tuple[list[dict[str, str]], list[dict[str, str]]]:
    if currency == "TRY":
        return [], [{"pair": "USD/TRY", "direction": "inverse"}]
    if currency == "USD":
        return [{"pair": "USD/TRY", "direction": "direct"}], []
    return [], []


def _fx_multiplier(path: list[dict[str, str]], pair_changes: dict[str, Any]) -> Decimal:
    multiplier = Decimal(1)
    for edge in path:
        if not isinstance(edge, dict):
            raise ScenarioValidationError("FX path edges must be objects")
        pair = edge.get("pair", "")
        change = _decimal(pair_changes.get(pair, "0"), f"fx_pair_changes.{pair}")
        rate_multiplier = Decimal(1) + change
        direction = edge.get("direction")
        if rate_multiplier <= 0:
            raise ScenarioValidationError("FX shock produces a non-positive rate")
        if direction == "direct":
            multiplier *= rate_multiplier
        elif direction == "inverse":
            multiplier /= rate_multiplier
        else:
            raise ScenarioValidationError("FX path direction must be direct or inverse")
    return multiplier


def _position_result(
    position: dict[str, Any], version: dict[str, Any]
) -> tuple[dict[str, Any], str | None]:
    shocks = version["shocks"]
    assumptions = version["assumptions"]
    mappings = version.get("mappings", {})
    instrument_id = str(position.get("instrument_id", ""))
    if not instrument_id:
        return {}, "INVALID_INSTRUMENT_ID"
    instrument_type = position.get("instrument_type")
    instrument_asset_classes = mappings.get("instrument_asset_classes", {})
    if not isinstance(instrument_asset_classes, dict) or not isinstance(instrument_type, str):
        return {}, "INVALID_SCENARIO_INPUT"
    asset_class = instrument_asset_classes.get(instrument_id, position.get("asset_class"))
    if asset_class is not None and not isinstance(asset_class, str):
        return {}, "INVALID_SCENARIO_INPUT"
    price_returns = shocks.get("asset_class_returns", {})
    price_shock = price_returns.get(asset_class)
    if price_returns and price_shock is None and asset_class not in {"cash", "currency"}:
        return {}, "UNMAPPED_FACTOR"

    try:
        pre_try = _decimal(position.get("value_try"), "value_try")
        pre_usd = _decimal(position.get("value_usd"), "value_usd")
        price_return = _decimal(price_shock or "0", "asset_class_return")
        price_multiplier = Decimal(1) + price_return
        if price_multiplier < 0:
            raise ScenarioValidationError("asset class shock cannot produce negative value")

        yield_return = Decimal(0)
        duration = position.get("modified_duration_years")
        if instrument_type == "fixed_rate_bond":
            currency = position.get("native_currency")
            yield_bps = shocks.get("yield_shifts_bps", {}).get(currency)
            if yield_bps is None:
                if shocks.get("yield_shifts_bps"):
                    return {}, "UNMAPPED_YIELD_CURVE"
            else:
                if duration is None:
                    return {}, "BOND_DURATION_MISSING"
                duration_value = _decimal(duration, "modified_duration_years")
                convexity = _decimal(
                    position.get(
                        "convexity_years_squared", assumptions.get("convexity_when_absent", "0")
                    ),
                    "convexity_years_squared",
                )
                delta_yield = _decimal(yield_bps, "yield_shifts_bps") / Decimal(10_000)
                yield_return = (
                    -duration_value * delta_yield + Decimal("0.5") * convexity * delta_yield**2
                )
                price_multiplier *= Decimal(1) + yield_return
                if price_multiplier < 0:
                    raise ScenarioValidationError(
                        "duration/convexity shock produces negative value"
                    )
        elif instrument_type not in {
            "spot",
            "crypto_spot",
            "equity_spot",
            "manual_spot",
            "cash",
            "currency",
        }:
            return {}, "UNSUPPORTED_INSTRUMENT_TYPE"

        try_path, usd_path = _default_fx_paths(position.get("native_currency", ""))
        if "fx_path_to_try" in position:
            try_path = position["fx_path_to_try"]
        if "fx_path_to_usd" in position:
            usd_path = position["fx_path_to_usd"]
        if shocks.get("fx_pair_changes") and position.get("native_currency") not in {"TRY", "USD"}:
            if "fx_path_to_try" not in position or "fx_path_to_usd" not in position:
                return {}, "FX_PATH_MISSING"
        try_fx = _fx_multiplier(try_path, shocks.get("fx_pair_changes", {}))
        usd_fx = _fx_multiplier(usd_path, shocks.get("fx_pair_changes", {}))
        post_try = pre_try * price_multiplier * try_fx
        post_usd = pre_usd * price_multiplier * usd_fx
        pnl_try = post_try - pre_try
        pnl_usd = post_usd - pre_usd
    except (KeyError, TypeError, ScenarioValidationError):
        return {}, "INVALID_SCENARIO_INPUT"

    return {
        "snapshot_line_id": position.get("snapshot_line_id"),
        "instrument_id": instrument_id,
        "state": "valid",
        "reason_codes": [],
        "pre_value_try": str(pre_try),
        "post_value_try": str(post_try),
        "pnl_try": str(pnl_try),
        "pre_value_usd": str(pre_usd),
        "post_value_usd": str(post_usd),
        "pnl_usd": str(pnl_usd),
        "price_return": str(price_return),
        "yield_return": str(yield_return),
        "fx_multiplier_try": str(try_fx),
        "fx_multiplier_usd": str(usd_fx),
    }, None


def _post_metrics(pre_metrics: dict[str, Any], shocks: dict[str, Any]) -> dict[str, Any]:
    volatility_shocks = shocks.get("volatility_multipliers", {})
    correlation_target = shocks.get("correlation_target")
    correlation_blend = _decimal(shocks.get("correlation_blend") or "0", "correlation_blend")
    if not Decimal(0) <= correlation_blend <= Decimal(1):
        raise ScenarioValidationError("correlation_blend must be between zero and one")
    if correlation_target is not None:
        target = _decimal(correlation_target, "correlation_target")
        if not Decimal(-1) <= target <= Decimal(1):
            raise ScenarioValidationError("correlation_target must be between minus one and one")
    else:
        target = None
    for key, multiplier in volatility_shocks.items():
        if key != "*" and not key:
            raise ScenarioValidationError("volatility multiplier keys must not be empty")
        if _decimal(multiplier, f"volatility_multiplier.{key}") < 0:
            raise ScenarioValidationError("volatility multipliers must be non-negative")
    if volatility_shocks and not pre_metrics.get("volatility"):
        raise ScenarioValidationError("pre-shock volatility metrics are required")
    if correlation_target is not None and not pre_metrics.get("correlations"):
        raise ScenarioValidationError("pre-shock correlation metrics are required")

    data_quality = pre_metrics.get("data_quality", "valid")
    if data_quality not in {"valid", "degraded", "blocked"}:
        raise ScenarioValidationError("invalid pre-shock risk metric data quality")
    if data_quality == "blocked":
        raise ScenarioValidationError("pre-shock risk metrics are blocked")
    degraded = data_quality == "degraded"

    def metric_value(value: Any, field: str) -> Decimal:
        nonlocal degraded
        if isinstance(value, dict):
            if value.get("state") == "blocked":
                raise ScenarioValidationError(f"pre-shock {field} metric is blocked")
            if value.get("state") == "degraded":
                degraded = True
            elif value.get("state", "valid") != "valid":
                raise ScenarioValidationError(f"invalid pre-shock {field} metric state")
            value = value.get("annualized" if field.startswith("volatility") else "coefficient")
            if value is None:
                raise ScenarioValidationError(f"pre-shock {field} metric is unavailable")
        if isinstance(value, float):
            value = str(value)
        result = _decimal(value, field)
        if field.startswith("volatility") and result < 0:
            raise ScenarioValidationError(f"{field} cannot be negative")
        if field.startswith("correlations") and not Decimal(-1) <= result <= Decimal(1):
            raise ScenarioValidationError(f"{field} must be between minus one and one")
        return result

    post_volatility = {
        key: str(
            metric_value(value, f"volatility.{key}")
            * _decimal(
                volatility_shocks.get(key, volatility_shocks.get("*", "1")),
                f"volatility_multiplier.{key}",
            )
        )
        for key, value in pre_metrics.get("volatility", {}).items()
    }
    post_correlations = {}
    for key, value in pre_metrics.get("correlations", {}).items():
        coefficient = metric_value(value, f"correlations.{key}")
        if target is not None:
            coefficient += (target - coefficient) * correlation_blend
        post_correlations[key] = str(coefficient)
    return {
        "volatility": post_volatility,
        "correlations": post_correlations,
        "data_quality": "degraded" if degraded else "valid",
    }


def evaluate_scenario(payload: dict[str, Any]) -> dict[str, Any]:
    """Revalue a sealed valuation bundle and return deterministic risk-job output."""
    version = payload.get("scenario_version")
    snapshot_id = payload.get("snapshot_id")
    valuation_id = payload.get("valuation_id")
    sealed_input = payload.get("sealed_input")
    positions = payload.get("positions")
    if (
        not isinstance(version, dict)
        or not isinstance(snapshot_id, str)
        or not snapshot_id
        or not isinstance(valuation_id, str)
        or not valuation_id
        or not isinstance(sealed_input, dict)
        or not isinstance(positions, list)
        or any(not isinstance(position, dict) for position in positions)
    ):
        raise ScenarioValidationError(
            "scenario_version, snapshot_id, valuation_id, sealed_input, and positions are required"
        )
    if (
        sealed_input.get("valuation_run_id") != valuation_id
        or sealed_input.get("snapshot_id") != snapshot_id
        or sealed_input.get("state") != "valid"
    ):
        raise ScenarioValidationError("sealed valuation provenance does not match scenario inputs")
    if (
        not isinstance(version.get("scenario_id"), str)
        or not version["scenario_id"]
        or not isinstance(version.get("version"), int)
        or isinstance(version.get("version"), bool)
        or version["version"] < 1
    ):
        raise ScenarioValidationError("scenario identity and integer version are required")
    if version.get("template_key") not in {"try_depreciation", "rates_up", "risk_off"}:
        raise ScenarioValidationError("unknown scenario template")
    if not isinstance(version.get("units", {}), dict) or not isinstance(
        version.get("mappings", {}), dict
    ):
        raise ScenarioValidationError("scenario units and mappings must be objects")
    shocks = version.get("shocks")
    if not isinstance(shocks, dict) or not isinstance(version.get("assumptions"), dict):
        raise ScenarioValidationError("versioned shocks and assumptions are required")
    for field in (
        "fx_pair_changes",
        "asset_class_returns",
        "yield_shifts_bps",
        "volatility_multipliers",
    ):
        if field in shocks and not isinstance(shocks[field], dict):
            raise ScenarioValidationError(f"{field} must be an object")
    pre_metrics = payload.get("pre_metrics", {})
    if not isinstance(pre_metrics, dict):
        raise ScenarioValidationError("pre_metrics must be an object")
    for field in ("volatility", "correlations"):
        if field in pre_metrics and not isinstance(pre_metrics[field], dict):
            raise ScenarioValidationError(f"pre_metrics.{field} must be an object")

    with localcontext() as decimal_context:
        decimal_context.prec = 50
        results: list[dict[str, Any]] = []
        unmapped: list[dict[str, str]] = []
        for position in positions:
            result, error = _position_result(position, version)
            if error:
                instrument_id = str(position.get("instrument_id", ""))
                results.append(
                    {
                        "snapshot_line_id": position.get("snapshot_line_id"),
                        "instrument_id": instrument_id,
                        "state": "blocked",
                        "reason_codes": [error],
                    }
                )
                if error in {"UNMAPPED_FACTOR", "UNMAPPED_YIELD_CURVE", "FX_PATH_MISSING"}:
                    unmapped.append({"instrument_id": instrument_id, "reason_code": error})
            else:
                results.append(result)

        coverage_policy = version.get("assumptions", {}).get("coverage_policy", "block")
        if coverage_policy not in {"block", "degrade"}:
            raise ScenarioValidationError("coverage_policy must be block or degrade")
        blocked = any(result["state"] == "blocked" for result in results)
        metric_reason: str | None = None
        try:
            metrics = _post_metrics(pre_metrics, shocks)
        except ScenarioValidationError as error:
            if not str(error).startswith("pre-shock"):
                raise
            metrics = {"volatility": {}, "correlations": {}}
            metric_reason = "REQUIRED_RISK_METRICS_MISSING"
            blocked = True
        if metrics.get("data_quality") == "degraded":
            metric_reason = "PRE_SHOCK_METRICS_DEGRADED"
            blocked = True
        state = (
            "blocked"
            if blocked and coverage_policy == "block"
            else "degraded"
            if blocked
            else "valid"
        )
        pnl_try = sum(
            (_decimal(item["pnl_try"], "pnl_try") for item in results if item["state"] == "valid"),
            Decimal(0),
        )
        pnl_usd = sum(
            (_decimal(item["pnl_usd"], "pnl_usd") for item in results if item["state"] == "valid"),
            Decimal(0),
        )
    return {
        "scenario_id": version["scenario_id"],
        "scenario_version": version["version"],
        "scenario_engine_version": SCENARIO_ENGINE_VERSION,
        "state": state,
        "reason_codes": sorted(
            {code for item in results for code in item["reason_codes"]}
            | ({metric_reason} if metric_reason else set())
        ),
        "unmapped_instruments": sorted(unmapped, key=lambda item: item["instrument_id"]),
        "positions": results,
        "portfolio_pnl_try": str(pnl_try) if state != "blocked" else None,
        "portfolio_pnl_usd": str(pnl_usd) if state != "blocked" else None,
        "pnl_tolerance": version.get("assumptions", {}).get("pnl_tolerance", "0.00000001"),
        "pre_metrics": pre_metrics,
        "post_metrics": metrics,
    }
