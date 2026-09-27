from __future__ import annotations

import json
import math
from datetime import date
from pathlib import Path

from atlasrisk.metrics import calculate_metrics

ROOT = Path(__file__).resolve().parents[3]
FIXTURE = ROOT / "test" / "fixtures" / "risk" / "risk-metrics-golden.json"


def test_risk_metrics_golden_with_declared_tolerance() -> None:
    fixture = json.loads(FIXTURE.read_text(encoding="utf-8"))
    prices = {
        instrument: {date.fromisoformat(day): value for day, value in values.items()}
        for instrument, values in fixture["price_history"].items()
    }
    nav = {date.fromisoformat(day): value for day, value in fixture["nav_history"].items()}
    actual = calculate_metrics(prices, nav, fixture["signed_exposures"])
    assert actual == calculate_metrics(prices, nav, fixture["signed_exposures"])
    expected = fixture["expected"]
    tolerance = fixture["absolute_tolerance"]

    assert actual["calendar"] == expected["calendar"]
    assert actual["annualization_factor"] == expected["annualization_factor"]
    assert math.isclose(
        actual["volatility"]["a"]["annualized"],
        expected["annualized_volatility_a"],
        rel_tol=0,
        abs_tol=tolerance,
    )
    assert math.isclose(
        actual["drawdown"]["maximum"], expected["drawdown"], rel_tol=0, abs_tol=tolerance
    )
    assert math.isclose(
        actual["concentration"]["hhi"], expected["hhi"], rel_tol=0, abs_tol=tolerance
    )
    assert math.isclose(
        actual["concentration"]["top_1_share"],
        expected["top_1_share"],
        rel_tol=0,
        abs_tol=tolerance,
    )
    assert math.isclose(
        actual["leverage"]["gross"], expected["gross_leverage"], rel_tol=0, abs_tol=tolerance
    )
    assert math.isclose(
        actual["leverage"]["net"], expected["net_leverage"], rel_tol=0, abs_tol=tolerance
    )
    assert actual["correlations"]["a|b"]["overlap_count"] == expected["correlation_overlap"]
    assert actual["correlations"]["a|b"]["state"] == "insufficient_coverage"
