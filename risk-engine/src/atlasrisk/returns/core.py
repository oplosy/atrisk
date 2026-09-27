"""Deterministic log-return calculations with explicit observation calendars."""

from __future__ import annotations

import math
from collections.abc import Mapping
from datetime import date
from typing import Literal

Calendar = Literal["business_daily", "crypto_daily"]


def compute_log_returns(
    prices: Mapping[date, float | None], *, calendar: Calendar = "business_daily"
) -> dict[date, float]:
    """Compute returns between observed positive finite prices, without filling gaps.

    Weekend observations are excluded for the cross-asset business-day calendar.
    A missing or invalid quote is never synthesized; each result is tied to the
    later observation date and may therefore span a multi-day gap.
    """
    if calendar not in ("business_daily", "crypto_daily"):
        raise ValueError(f"unsupported return calendar: {calendar}")

    observations: list[tuple[date, float]] = []
    for observed_at, price in sorted(prices.items()):
        if calendar == "business_daily" and observed_at.weekday() >= 5:
            continue
        if price is None or not math.isfinite(price) or price <= 0:
            continue
        observations.append((observed_at, float(price)))

    return {
        observed_at: math.log(price / previous_price)
        for (_, previous_price), (observed_at, price) in zip(observations, observations[1:])
    }
