"""Deterministic log-return calculations with explicit observation calendars."""

from __future__ import annotations

import math
from collections.abc import Mapping
from datetime import date, timedelta
from typing import Literal

Calendar = Literal["business_daily", "crypto_daily"]


def _next_calendar_date(observed_at: date, calendar: Calendar) -> date:
    next_date = observed_at + timedelta(days=1)
    while calendar == "business_daily" and next_date.weekday() >= 5:
        next_date += timedelta(days=1)
    return next_date


def _valid_price(price: float | None) -> bool:
    return price is not None and math.isfinite(price) and price > 0


def count_missing_intervals(prices: Mapping[date, float | None], *, calendar: Calendar) -> int:
    """Count gaps between valid observations on the selected expected calendar."""
    dates = sorted(
        observed_at
        for observed_at, price in prices.items()
        if (calendar == "crypto_daily" or observed_at.weekday() < 5) and _valid_price(price)
    )
    return sum(
        _next_calendar_date(previous, calendar) != current
        for previous, current in zip(dates, dates[1:])
    )


def compute_log_returns(
    prices: Mapping[date, float | None], *, calendar: Calendar = "business_daily"
) -> dict[date, float]:
    """Compute returns between observed positive finite prices, without filling gaps.

    Weekend observations are excluded for the cross-asset business-day calendar.
    A missing or invalid quote is never synthesized, and no return is emitted
    across a missing expected calendar interval.
    """
    if calendar not in ("business_daily", "crypto_daily"):
        raise ValueError(f"unsupported return calendar: {calendar}")

    observations: list[tuple[date, float]] = []
    for observed_at, price in sorted(prices.items()):
        if calendar == "business_daily" and observed_at.weekday() >= 5:
            continue
        if not _valid_price(price):
            continue
        observations.append((observed_at, float(price)))

    returns = {}
    for (previous_at, previous_price), (observed_at, price) in zip(observations, observations[1:]):
        if _next_calendar_date(previous_at, calendar) == observed_at:
            returns[observed_at] = math.log(price / previous_price)
    return returns


def compute_aligned_log_returns(
    left_prices: Mapping[date, float | None],
    right_prices: Mapping[date, float | None],
    *,
    calendar: Calendar = "business_daily",
) -> tuple[dict[date, float], dict[date, float]]:
    """Compute paired returns over identical observed price intervals only."""
    if calendar not in ("business_daily", "crypto_daily"):
        raise ValueError(f"unsupported return calendar: {calendar}")
    common_dates = sorted(set(left_prices).intersection(right_prices))
    shared_left = {
        observed_at: left_prices[observed_at]
        for observed_at in common_dates
        if _valid_price(left_prices[observed_at]) and _valid_price(right_prices[observed_at])
    }
    shared_right = {observed_at: right_prices[observed_at] for observed_at in shared_left}
    return (
        compute_log_returns(shared_left, calendar=calendar),
        compute_log_returns(shared_right, calendar=calendar),
    )
