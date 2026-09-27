from __future__ import annotations

import json
from decimal import Decimal
from pathlib import Path

from atlasrisk.scenarios import evaluate_scenario

ROOT = Path(__file__).resolve().parents[3]
FIXTURE = ROOT / "test" / "fixtures" / "risk" / "scenario-revaluation-golden.json"


def test_scenario_revaluation_golden_with_declared_tolerance() -> None:
    fixture = json.loads(FIXTURE.read_text(encoding="utf-8"))
    result = evaluate_scenario(fixture["payload"])
    expected = fixture["expected"]
    tolerance = Decimal(fixture["absolute_tolerance"])
    assert result["state"] == expected["state"]
    for actual_key, expected_key in (
        ("portfolio_pnl_try", "portfolio_pnl_try"),
        ("portfolio_pnl_usd", "portfolio_pnl_usd"),
    ):
        assert abs(Decimal(result[actual_key]) - Decimal(expected[expected_key])) <= tolerance
    assert (
        abs(
            Decimal(result["post_metrics"]["volatility"]["crypto"])
            - Decimal(expected["crypto_volatility"])
        )
        <= tolerance
    )
    assert (
        abs(
            Decimal(result["post_metrics"]["correlations"]["crypto|equity"])
            - Decimal(expected["crypto_equity_correlation"])
        )
        <= tolerance
    )
