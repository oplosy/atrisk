from __future__ import annotations

import json
from decimal import Decimal
from pathlib import Path

import pytest

from atlasrisk.attribution import (
    AttributionValidationError,
    attribute_scenario,
    shapley_allocate,
)

ROOT = Path(__file__).resolve().parents[3]
SCENARIO_FIXTURE = ROOT / "test" / "fixtures" / "risk" / "scenario-revaluation-golden.json"


def test_shapley_symmetry_and_efficiency() -> None:
    values = shapley_allocate(
        ["alpha", "beta"],
        lambda subset: Decimal("3") * len(subset),
    )

    assert values["alpha"] == values["beta"] == Decimal("3")
    assert sum(values.values(), Decimal(0)) == Decimal("6")


def test_dummy_factor_gets_zero_contribution() -> None:
    values = shapley_allocate(
        ["active", "dummy"],
        lambda subset: Decimal("5") if "active" in subset else Decimal(0),
    )

    assert values == {"active": Decimal("5"), "dummy": Decimal(0)}


def test_shapley_is_invariant_to_factor_input_order() -> None:
    def value(subset: frozenset[str]) -> Decimal:
        return (
            Decimal("2") * ("alpha" in subset)
            + Decimal("7") * ("beta" in subset)
            + Decimal("3") * ("alpha" in subset and "beta" in subset)
        )

    first = shapley_allocate(["alpha", "beta"], value)
    second = shapley_allocate(["beta", "alpha"], value)

    assert first == second
    assert first == {"alpha": Decimal("3.5"), "beta": Decimal("8.5")}


def test_factor_bound_is_validated_before_subset_evaluation() -> None:
    calls = 0

    def value(_: frozenset[str]) -> Decimal:
        nonlocal calls
        calls += 1
        return Decimal(0)

    with pytest.raises(AttributionValidationError, match="at most 1"):
        shapley_allocate(["a", "b"], value, max_factors=1)
    assert calls == 0


def test_scenario_attribution_reconciles_golden_stress_loss() -> None:
    payload = json.loads(SCENARIO_FIXTURE.read_text(encoding="utf-8"))["payload"]

    result = attribute_scenario(payload)

    assert result["method"] == "shapley"
    assert result["method_version"] == "1.0.0"
    assert result["tolerance"] == "0.00000001"
    assert result["factor_count"] == 6
    assert result["subset_count"] == 64
    assert result["permutation_count"] == 720
    assert result["state"] == "valid"
    assert Decimal(result["total_pnl"]) == Decimal("-1280.00")
    assert result["reconciles"] is True
    assert Decimal(result["interaction_residual"]) == Decimal(0)
    factor_values = {
        item["factor"]: Decimal(item["contribution"]) for item in result["factor_contributions"]
    }
    assert factor_values["asset_return:crypto"] == Decimal("-1280.00")
    assert all(value == 0 for name, value in factor_values.items() if name != "asset_return:crypto")
    position = result["position_contributions"][0]
    assert Decimal(position["total_pnl"]) == sum(
        (Decimal(item["contribution"]) for item in position["factor_contributions"]),
        Decimal(0),
    )


def test_unsupported_position_remains_visible() -> None:
    payload = json.loads(SCENARIO_FIXTURE.read_text(encoding="utf-8"))["payload"]
    payload["positions"].append(
        {
            "snapshot_line_id": "unsupported-line",
            "instrument_id": "unsupported-option",
            "instrument_type": "option",
            "asset_class": "crypto",
            "native_currency": "USD",
            "value_try": "100",
            "value_usd": "3",
        }
    )

    result = attribute_scenario(payload)

    assert result["state"] == "blocked"
    assert result["total_pnl"] is None
    assert result["unmapped_positions"][-1]["instrument_id"] == "unsupported-option"
    assert result["position_contributions"][1]["state"] == "blocked"
    assert result["position_contributions"][1]["residual"] is None
