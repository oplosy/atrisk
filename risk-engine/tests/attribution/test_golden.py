from __future__ import annotations

import json
from decimal import Decimal
from pathlib import Path

from atlasrisk.attribution import attribute_scenario

ROOT = Path(__file__).resolve().parents[3]
FIXTURE = ROOT / "test" / "fixtures" / "risk" / "scenario-revaluation-golden.json"


def test_factor_attribution_golden() -> None:
    fixture = json.loads(FIXTURE.read_text(encoding="utf-8"))
    result = attribute_scenario(fixture["payload"], tolerance=fixture["absolute_tolerance"])

    assert result["reconciles"] is True
    assert Decimal(result["interaction_residual"]) == 0
    assert Decimal(result["total_pnl"]) == Decimal(fixture["expected"]["portfolio_pnl_try"])
    assert result["unmapped_positions"] == []
