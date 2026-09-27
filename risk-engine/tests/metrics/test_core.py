from __future__ import annotations

import math
from datetime import date, timedelta

from atlasrisk.metrics import calculate_metrics
from atlasrisk.returns import compute_log_returns


def test_business_calendar_excludes_weekends_without_filling_prices() -> None:
    prices = {
        date(2026, 1, 2): 100.0,  # Friday
        date(2026, 1, 3): 110.0,  # Saturday, excluded
        date(2026, 1, 5): 121.0,  # Monday
        date(2026, 1, 6): None,  # Missing observation, not filled
        date(2026, 1, 7): 133.1,
    }

    returns = compute_log_returns(prices)

    assert list(returns) == [date(2026, 1, 5)]
    assert returns[date(2026, 1, 5)] > 0


def test_crypto_calendar_keeps_weekend_observations() -> None:
    prices = {date(2026, 1, 2) + timedelta(days=offset): 100 + offset for offset in range(4)}

    returns = compute_log_returns(prices, calendar="crypto_daily")

    assert len(returns) == 3
    result = calculate_metrics({"btc": prices}, {}, {"btc": 1.0}, calendar="crypto_daily")
    assert result["annualization_factor"] == 365**0.5
    assert result["calendar"] == "crypto_daily"


def test_constant_series_has_zero_volatility_and_degraded_self_correlation() -> None:
    prices = {date(2026, 1, 1) + timedelta(days=i): 100.0 for i in range(70)}
    result = calculate_metrics(
        {"a": prices, "b": prices},
        {date(2026, 1, 1): 100.0},
        {"a": 50.0},
        calendar="crypto_daily",
    )

    assert result["volatility"]["a"]["annualized"] == 0
    assert result["correlations"]["a|b"]["overlap_count"] == 69
    assert result["correlations"]["a|b"]["coefficient"] is None
    assert result["correlations"]["a|b"]["state"] == "degraded"
    assert result["correlations"]["a|b"]["reason"] == "ZERO_VARIANCE"
    assert result["data_quality"] == "degraded"


def test_full_volatility_window_and_correlation_coverage_are_labeled() -> None:
    prices = {}
    price = 100.0
    for offset in range(130):
        observed_at = date(2026, 1, 1) + timedelta(days=offset)
        if observed_at.weekday() >= 5:
            continue
        price *= 1.01 if offset % 2 else 0.99
        prices[observed_at] = price
    second = {observed_at: value * 2 for observed_at, value in prices.items()}
    result = calculate_metrics(
        {"a": prices, "b": second},
        {date(2026, 1, 1): 1000.0},
        {"a": 500.0},
    )

    assert result["volatility"]["a"]["observations"] == 63
    assert result["volatility"]["a"]["state"] == "valid"
    assert result["volatility"]["a"]["annualization_factor"] == math.sqrt(252)
    assert result["correlations"]["a|b"]["overlap_count"] >= 60
    assert result["correlations"]["a|b"]["state"] == "valid"
    assert math.isclose(result["correlations"]["a|b"]["coefficient"], 1.0, abs_tol=1e-12)


def test_no_price_series_is_blocked() -> None:
    result = calculate_metrics(
        {},
        {date(2026, 1, 1): 100.0},
        {"a": 10.0},
    )

    assert result["data_quality"] == "blocked"


def test_correlation_does_not_match_returns_from_different_intervals() -> None:
    dates = [date(2026, 1, 5) + timedelta(days=i) for i in range(3)]
    left = dict(zip(dates, [100.0, 110.0, 120.0]))
    right = {dates[0]: 200.0, dates[1]: None, dates[2]: 220.0}
    result = calculate_metrics(
        {"a": left, "b": right},
        {dates[0]: 1000.0},
        {"a": 100.0},
        calendar="crypto_daily",
    )

    pair = result["correlations"]["a|b"]
    assert pair["overlap_count"] == 0
    assert pair["coefficient"] is None
    assert pair["state"] == "blocked"
    assert pair["reason"] == "INSUFFICIENT_OVERLAP"


def test_sparse_and_all_missing_series_are_not_healthy() -> None:
    result = calculate_metrics(
        {
            "sparse": {date(2026, 1, 1): 100.0, date(2026, 1, 5): 101.0},
            "missing": {date(2026, 1, 1): None, date(2026, 1, 2): float("nan")},
        },
        {date(2026, 1, 1): 10.0},
        {"sparse": 1.0},
    )

    assert result["volatility"]["sparse"]["state"] == "blocked"
    assert result["volatility"]["sparse"]["missing_intervals"] == 1
    assert result["volatility"]["missing"]["state"] == "blocked"
    assert result["volatility"]["missing"]["invalid_price_observations"] == 2
    assert result["data_quality"] == "blocked"


def test_correlations_expose_overlap_and_state_for_misaligned_series() -> None:
    first = {date(2026, 1, 1) + timedelta(days=i): 100 + i for i in range(5)}
    second = {date(2026, 1, 1) + timedelta(days=i): 200 + i for i in range(3, 8)}

    result = calculate_metrics(
        {"a": first, "b": second},
        {date(2026, 1, 1): 1.0},
        {"a": 1.0},
        calendar="crypto_daily",
    )

    pair = result["correlations"]["a|b"]
    assert pair == {
        "coefficient": None,
        "overlap_count": 1,
        "required_overlap": 60,
        "reason": "INSUFFICIENT_OVERLAP",
        "state": "blocked",
    }


def test_nonpositive_nav_blocks_drawdown_and_leverage() -> None:
    for nav in (0.0, -1.0):
        result = calculate_metrics(
            {"a": {date(2026, 1, 1): 10.0, date(2026, 1, 2): 11.0}},
            {date(2026, 1, 1): 10.0, date(2026, 1, 2): nav},
            {"a": 10.0},
        )
        assert result["drawdown"]["state"] == "blocked"
        assert result["leverage"]["state"] == "blocked"
        assert result["data_quality"] == "blocked"


def test_negative_position_values_use_absolute_concentration_weights() -> None:
    result = calculate_metrics(
        {"a": {date(2026, 1, 1): 10.0, date(2026, 1, 2): 11.0}},
        {date(2026, 1, 1): 100.0},
        {"long": 60.0, "short": -40.0},
    )

    concentration = result["concentration"]
    assert concentration["hhi"] == 0.52
    assert concentration["top_1_share"] == 0.6
    assert concentration["top_5_share"] == 1.0
    assert result["leverage"]["gross"] == 1.0
    assert result["leverage"]["net"] == 0.2


def test_nonfinite_exposure_blocks_leverage_and_concentration() -> None:
    result = calculate_metrics(
        {"a": {date(2026, 1, 1): 10.0, date(2026, 1, 2): 11.0}},
        {date(2026, 1, 1): 100.0},
        {"a": float("nan")},
    )

    assert result["leverage"]["state"] == "blocked"
    assert result["concentration"]["state"] == "blocked"
    assert result["data_quality"] == "blocked"
