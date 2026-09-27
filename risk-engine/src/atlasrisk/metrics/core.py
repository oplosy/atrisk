"""Pure, versioned portfolio metrics with explicit sample and quality coverage."""

from __future__ import annotations

import itertools
import math
import statistics
from collections.abc import Mapping
from datetime import date

from atlasrisk.returns import (
    Calendar,
    compute_aligned_log_returns,
    compute_log_returns,
    count_missing_intervals,
)

_VOLATILITY_WINDOW = 63
_CORRELATION_WINDOW = 252
_MIN_CORRELATION_OVERLAP = 60


def _finite_values(values: Mapping[date, float | None]) -> dict[date, float]:
    return {
        observed_at: float(value)
        for observed_at, value in values.items()
        if value is not None and math.isfinite(value)
    }


def _invalid_price_count(values: Mapping[date, float | None], calendar: Calendar) -> int:
    eligible = (
        (observed_at, value)
        for observed_at, value in values.items()
        if calendar == "crypto_daily" or observed_at.weekday() < 5
    )
    return sum(value is None or not math.isfinite(value) or value <= 0 for _, value in eligible)


def _correlation(left: list[float], right: list[float]) -> float | None:
    mean_left = math.fsum(left) / len(left)
    mean_right = math.fsum(right) / len(right)
    centered_left = [value - mean_left for value in left]
    centered_right = [value - mean_right for value in right]
    sum_left = math.fsum(value * value for value in centered_left)
    sum_right = math.fsum(value * value for value in centered_right)
    if sum_left == 0 or sum_right == 0:
        return None
    covariance = math.fsum(a * b for a, b in zip(centered_left, centered_right))
    return max(-1.0, min(1.0, covariance / math.sqrt(sum_left * sum_right)))


def calculate_metrics(
    price_history: Mapping[str, Mapping[date, float | None]],
    nav_history: Mapping[date, float | None],
    signed_exposures: Mapping[str, float],
    *,
    calendar: Calendar = "business_daily",
) -> dict:
    """Calculate returns, volatility, correlation, drawdown, leverage, and concentration.

    Financial persistence remains decimal-based. This numerical boundary accepts
    finite floats and exposes the selected calendar, annualization, and coverage.
    """
    if calendar not in ("business_daily", "crypto_daily"):
        raise ValueError(f"unsupported risk calendar: {calendar}")
    annualization = 365 if calendar == "crypto_daily" else 252
    annualization_factor = math.sqrt(annualization)

    returns = {
        instrument: compute_log_returns(prices, calendar=calendar)
        for instrument, prices in sorted(price_history.items())
    }
    volatility: dict[str, dict] = {}
    quality_states: list[str] = []
    for instrument, values in returns.items():
        window = list(values.values())[-_VOLATILITY_WINDOW:]
        invalid_prices = _invalid_price_count(price_history[instrument], calendar)
        missing_intervals = count_missing_intervals(price_history[instrument], calendar=calendar)
        if len(window) >= _VOLATILITY_WINDOW and invalid_prices == 0 and missing_intervals == 0:
            state = "valid"
            value = statistics.stdev(window) * annualization_factor
        elif window:
            state = "degraded"
            value = statistics.stdev(window) * annualization_factor if len(window) > 1 else None
        else:
            state = "blocked"
            value = None
        quality_states.append(state)
        volatility[instrument] = {
            "annualized": value,
            "annualization_factor": annualization_factor,
            "observations": len(window),
            "required_observations": _VOLATILITY_WINDOW,
            "invalid_price_observations": invalid_prices,
            "missing_intervals": missing_intervals,
            "state": state,
        }

    if not price_history:
        quality_states.append("blocked")

    correlations: dict[str, dict] = {}
    for left_id, right_id in itertools.combinations(sorted(returns), 2):
        left_pair, right_pair = compute_aligned_log_returns(
            price_history[left_id], price_history[right_id], calendar=calendar
        )
        overlap = sorted(set(left_pair).intersection(right_pair))[-_CORRELATION_WINDOW:]
        left_values = [left_pair[observed_at] for observed_at in overlap]
        right_values = [right_pair[observed_at] for observed_at in overlap]
        if len(overlap) < _MIN_CORRELATION_OVERLAP:
            value, state, reason = None, "blocked", "INSUFFICIENT_OVERLAP"
        else:
            value = _correlation(left_values, right_values)
            state, reason = ("valid", None) if value is not None else ("degraded", "ZERO_VARIANCE")
        quality_states.append(state)
        correlations[f"{left_id}|{right_id}"] = {
            "coefficient": value,
            "overlap_count": len(overlap),
            "required_overlap": _MIN_CORRELATION_OVERLAP,
            "reason": reason,
            "state": state,
        }

    finite_nav = _finite_values(nav_history)
    invalid_exposures = any(not math.isfinite(value) for value in signed_exposures.values())
    invalid_nav = any(
        value is not None and (not math.isfinite(value) or value <= 0)
        for value in nav_history.values()
    )
    if invalid_nav or not finite_nav:
        drawdown = {"maximum": None, "observations": len(finite_nav), "state": "blocked"}
        quality_states.append("blocked")
        leverage = {"gross": None, "net": None, "state": "blocked"}
    else:
        peak = -math.inf
        drawdowns: list[float] = []
        for value in (finite_nav[key] for key in sorted(finite_nav)):
            peak = max(peak, value)
            drawdowns.append(value / peak - 1.0)
        drawdown_state = "degraded" if len(finite_nav) != len(nav_history) else "valid"
        drawdown = {
            "maximum": min(drawdowns, default=0.0),
            "observations": len(finite_nav),
            "state": drawdown_state,
        }
        quality_states.append(drawdown_state)
        latest_nav = finite_nav[max(finite_nav)]
        if not signed_exposures or invalid_exposures:
            leverage = {"gross": None, "net": None, "state": "blocked"}
            quality_states.append("blocked")
        else:
            gross = math.fsum(abs(value) for value in signed_exposures.values()) / latest_nav
            net = math.fsum(signed_exposures.values()) / latest_nav
            leverage = {"gross": gross, "net": net, "state": "valid"}

    exposure_total = (
        math.fsum(abs(value) for value in signed_exposures.values())
        if not invalid_exposures
        else math.nan
    )
    if (
        not signed_exposures
        or invalid_exposures
        or exposure_total == 0
        or not math.isfinite(exposure_total)
    ):
        concentration = {
            "hhi": None,
            "top_1_share": None,
            "top_5_share": None,
            "top_weights": [],
            "state": "blocked",
        }
        quality_states.append("blocked")
    else:
        weighted = sorted(
            (
                (instrument, abs(value) / exposure_total)
                for instrument, value in signed_exposures.items()
            ),
            key=lambda item: (-item[1], item[0]),
        )
        weights = [weight for _, weight in weighted]
        concentration = {
            "hhi": math.fsum(weight * weight for weight in weights),
            "top_1_share": math.fsum(weights[:1]),
            "top_5_share": math.fsum(weights[:5]),
            "top_weights": [
                {"instrument_id": key, "weight": weight} for key, weight in weighted[:5]
            ],
            "state": "valid",
        }

    overall_state = (
        "blocked"
        if "blocked" in quality_states
        else "degraded"
        if "degraded" in quality_states
        else "valid"
    )
    return {
        "calendar": calendar,
        "annualization_factor": annualization_factor,
        "returns": {
            instrument: {observed_at.isoformat(): value for observed_at, value in values.items()}
            for instrument, values in returns.items()
        },
        "volatility": volatility,
        "correlations": correlations,
        "drawdown": drawdown,
        "leverage": leverage,
        "concentration": concentration,
        "data_quality": overall_state,
    }
