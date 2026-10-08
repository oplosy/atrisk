from __future__ import annotations

from copy import deepcopy
from decimal import Decimal

import pytest

from atlasrisk.scenarios import (
    ScenarioValidationError,
    create_template_version,
    evaluate_scenario,
)


def _position(**overrides: object) -> dict[str, object]:
    return {
        "snapshot_line_id": "line-1",
        "instrument_id": "instrument-1",
        "instrument_type": "crypto_spot",
        "asset_class": "crypto",
        "native_currency": "USD",
        "value_try": "3200",
        "value_usd": "100",
        **overrides,
    }


def _payload(template: str, positions: list[dict[str, object]]) -> dict[str, object]:
    version = create_template_version(template, scenario_id="scenario-1")
    return {
        "scenario_version": version,
        "snapshot_id": "snapshot-1",
        "valuation_id": "valuation-1",
        "sealed_input": {
            "snapshot_id": "snapshot-1",
            "valuation_run_id": "valuation-1",
            "state": "valid",
        },
        "positions": positions,
        "pre_metrics": {
            "volatility": {"crypto": {"annualized": 0.4, "state": "valid"}},
            "correlations": {"crypto|equity": {"coefficient": 0.2, "state": "valid"}},
            "data_quality": "valid",
        },
    }


def test_templates_are_versioned_and_defensively_copied() -> None:
    first = create_template_version("risk_off", scenario_id="s-1")
    second = create_template_version("risk_off", scenario_id="s-1", version=2)
    first["shocks"]["asset_class_returns"]["crypto"] = "0"
    assert second["version"] == 2
    assert second["shocks"]["asset_class_returns"]["crypto"] == "-0.40"
    with pytest.raises(ScenarioValidationError):
        create_template_version("custom", scenario_id="s-1")


def test_shocks_require_bounded_decimal_strings() -> None:
    payload = _payload("risk_off", [_position()])
    payload["scenario_version"]["shocks"]["asset_class_returns"]["crypto"] = -0.4
    with pytest.raises(ScenarioValidationError, match="decimal string"):
        evaluate_scenario(payload)

    payload = _payload("risk_off", [_position()])
    payload["scenario_version"]["shocks"]["correlation_target"] = "1e-1"
    with pytest.raises(ScenarioValidationError, match="decimal string"):
        evaluate_scenario(payload)

    payload = _payload("risk_off", [_position()])
    payload["scenario_version"]["shocks"]["correlation_target"] = "123456789012345678901.0"
    with pytest.raises(ScenarioValidationError, match="NUMERIC"):
        evaluate_scenario(payload)

    payload = _payload("risk_off", [_position()])
    payload["scenario_version"]["shocks"]["unexpected"] = "0.1"
    with pytest.raises(ScenarioValidationError, match="unsupported shock field"):
        evaluate_scenario(payload)

    payload = _payload("risk_off", [_position()])
    payload["scenario_version"]["shocks"]["asset_class_returns"] = "-0.1"
    with pytest.raises(ScenarioValidationError, match="must be an object"):
        evaluate_scenario(payload)


def test_try_depreciation_revalues_usd_asset_but_not_try_value() -> None:
    result = evaluate_scenario(_payload("try_depreciation", [_position()]))
    line = result["positions"][0]
    assert result["state"] == "valid"
    assert Decimal(line["post_value_try"]) == Decimal("4000")
    assert Decimal(line["post_value_usd"]) == Decimal("100")
    assert Decimal(result["portfolio_pnl_usd"]) == Decimal("0")


def test_try_depreciation_does_not_require_unshocked_risk_metrics() -> None:
    result = evaluate_scenario(
        {
            "scenario_version": create_template_version("try_depreciation", scenario_id="s-1"),
            "snapshot_id": "snapshot-1",
            "valuation_id": "valuation-1",
            "sealed_input": {
                "snapshot_id": "snapshot-1",
                "valuation_run_id": "valuation-1",
                "state": "valid",
            },
            "positions": [],
        }
    )
    assert result["state"] == "valid"


def test_portfolio_pnl_is_sum_of_supported_position_pnl() -> None:
    first = _position(value_try="3200", value_usd="100")
    second = _position(
        snapshot_line_id="line-2", instrument_id="instrument-2", value_try="6400", value_usd="200"
    )
    result = evaluate_scenario(
        {
            **_payload("risk_off", [first, second]),
            "pre_metrics": {
                "volatility": {"crypto": "0.3"},
                "correlations": {"crypto|equity": "0.1"},
            },
        }
    )
    tolerance = Decimal(result["pnl_tolerance"])
    summed_try = sum((Decimal(item["pnl_try"]) for item in result["positions"]), Decimal(0))
    summed_usd = sum((Decimal(item["pnl_usd"]) for item in result["positions"]), Decimal(0))
    assert abs(Decimal(result["portfolio_pnl_try"]) - summed_try) <= tolerance
    assert abs(Decimal(result["portfolio_pnl_usd"]) - summed_usd) <= tolerance


def test_risk_off_pnl_is_separate_from_volatility_and_correlation() -> None:
    result = evaluate_scenario(
        {
            **_payload("risk_off", [_position()]),
            "pre_metrics": {
                "volatility": {"crypto": "0.4"},
                "correlations": {"crypto|equity": "0.2"},
            },
        }
    )
    line = result["positions"][0]
    assert Decimal(line["pnl_usd"]) == Decimal("-40")
    assert Decimal(result["post_metrics"]["volatility"]["crypto"]) == Decimal("0.8")
    assert Decimal(result["post_metrics"]["correlations"]["crypto|equity"]) == Decimal("0.475")


def test_risk_off_consumes_canonical_ar302_metric_shapes() -> None:
    result = evaluate_scenario(
        {
            **_payload("risk_off", [_position()]),
            "pre_metrics": {
                "volatility": {"crypto": {"annualized": 0.4, "state": "valid"}},
                "correlations": {"crypto|equity": {"coefficient": 0.2, "state": "valid"}},
            },
        }
    )
    assert Decimal(result["post_metrics"]["volatility"]["crypto"]) == Decimal("0.8")
    assert Decimal(result["post_metrics"]["correlations"]["crypto|equity"]) == Decimal("0.475")


def test_undefined_cash_correlation_does_not_block_scenario() -> None:
    payload = _payload("risk_off", [_position(asset_class="cash")])
    payload["scenario_version"]["shocks"]["asset_class_returns"] = {"cash": "0"}
    payload["pre_metrics"] = {
        "data_quality": "valid",
        "volatility": {"cash": {"annualized": 0, "state": "valid"}},
        "correlations": {
            "cash|spot": {
                "coefficient": None,
                "state": "degraded",
                "reason": "ZERO_VARIANCE",
            }
        },
    }
    result = evaluate_scenario(payload)
    assert result["state"] == "valid"
    assert result["post_metrics"]["correlations"]["cash|spot"] is None


def test_rate_shock_uses_basis_points_and_bond_duration_convexity() -> None:
    bond = _position(
        instrument_id="bond-1",
        instrument_type="fixed_rate_bond",
        asset_class="fixed_rate_bond",
        native_currency="TRY",
        value_try="1000",
        value_usd="31.25",
        modified_duration_years="5",
        convexity_years_squared="30",
    )
    result = evaluate_scenario(_payload("rates_up", [bond]))
    expected_return = (
        Decimal(-5) * Decimal("0.05") + Decimal("0.5") * Decimal(30) * Decimal("0.05") ** 2
    )
    assert Decimal(result["positions"][0]["yield_return"]) == expected_return
    assert Decimal(result["positions"][0]["price_return"]) == Decimal(0)
    assert Decimal(result["positions"][0]["pnl_try"]) == Decimal("1000") * expected_return


def test_unmapped_factor_blocks_and_never_returns_partial_portfolio_pnl() -> None:
    position = _position()
    position["asset_class"] = "unknown"
    result = evaluate_scenario(_payload("risk_off", [position]))
    assert result["state"] == "blocked"
    assert result["portfolio_pnl_try"] is None
    assert result["unmapped_instruments"] == [
        {"instrument_id": "instrument-1", "reason_code": "UNMAPPED_FACTOR"}
    ]


def test_unsupported_derivative_and_missing_fx_path_are_blocked() -> None:
    derivative = _position(instrument_type="option")
    derivative_result = evaluate_scenario(_payload("try_depreciation", [derivative]))
    assert derivative_result["positions"][0]["reason_codes"] == ["UNSUPPORTED_INSTRUMENT_TYPE"]

    foreign = _position(native_currency="EUR")
    fx_result = evaluate_scenario(_payload("try_depreciation", [foreign]))
    assert fx_result["positions"][0]["reason_codes"] == ["FX_PATH_MISSING"]


def test_revaluation_does_not_mutate_the_sealed_version() -> None:
    version = create_template_version("rates_up", scenario_id="scenario-1")
    original = deepcopy(version)
    evaluate_scenario(
        {
            "scenario_version": version,
            "snapshot_id": "snapshot-1",
            "valuation_id": "valuation-1",
            "sealed_input": {
                "snapshot_id": "snapshot-1",
                "valuation_run_id": "valuation-1",
                "state": "valid",
            },
            "positions": [],
        }
    )
    assert version == original


def test_invalid_coverage_policy_is_rejected() -> None:
    version = create_template_version("risk_off", scenario_id="scenario-1")
    version["assumptions"]["coverage_policy"] = "ignore"
    with pytest.raises(ScenarioValidationError, match="coverage_policy"):
        evaluate_scenario(
            {
                "scenario_version": version,
                "snapshot_id": "snapshot-1",
                "valuation_id": "valuation-1",
                "sealed_input": {
                    "snapshot_id": "snapshot-1",
                    "valuation_run_id": "valuation-1",
                    "state": "valid",
                },
                "positions": [],
            }
        )


def test_risk_off_without_pre_shock_metrics_is_not_reported_healthy() -> None:
    payload = _payload("risk_off", [_position()])
    payload.pop("pre_metrics")
    result = evaluate_scenario(payload)
    assert result["state"] == "blocked"
    assert result["portfolio_pnl_try"] is None
    assert result["reason_codes"] == ["REQUIRED_RISK_METRICS_MISSING"]


def test_sealed_provenance_must_match_bound_snapshot_and_valuation() -> None:
    payload = _payload("try_depreciation", [_position()])
    payload["sealed_input"]["valuation_run_id"] = "forged-valuation"
    with pytest.raises(ScenarioValidationError, match="sealed valuation provenance"):
        evaluate_scenario(payload)
