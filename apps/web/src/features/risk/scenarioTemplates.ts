export type ScenarioTemplate = "try_depreciation" | "rates_up" | "risk_off";

interface ScenarioConfig {
  units: Record<string, unknown>;
  shocks: Record<string, unknown>;
  mappings: Record<string, unknown>;
  assumptions: Record<string, unknown>;
}

const templates: Record<ScenarioTemplate, ScenarioConfig> = {
  try_depreciation: {
    units: { fx_pair_changes: "relative", yield_shifts_bps: "basis_points" },
    shocks: {
      fx_pair_changes: { "USD/TRY": "0.25" },
      asset_class_returns: {},
      yield_shifts_bps: {},
      volatility_multipliers: {},
      correlation_target: null,
      correlation_blend: null,
    },
    mappings: { instrument_asset_classes: {} },
    assumptions: { coverage_policy: "block", pnl_tolerance: "0.00000001" },
  },
  rates_up: {
    units: { fx_pair_changes: "relative", yield_shifts_bps: "basis_points" },
    shocks: {
      fx_pair_changes: {},
      asset_class_returns: {},
      yield_shifts_bps: { TRY: 500, USD: 200 },
      volatility_multipliers: {},
      correlation_target: null,
      correlation_blend: null,
    },
    mappings: { instrument_asset_classes: {} },
    assumptions: {
      coverage_policy: "block",
      pnl_tolerance: "0.00000001",
      convexity_when_absent: "0",
    },
  },
  risk_off: {
    units: { fx_pair_changes: "relative", yield_shifts_bps: "basis_points" },
    shocks: {
      fx_pair_changes: {},
      asset_class_returns: {
        crypto: "-0.40",
        equity: "-0.20",
        fixed_rate_bond: "0",
      },
      yield_shifts_bps: {},
      volatility_multipliers: { "*": "2.0" },
      correlation_target: "0.75",
      correlation_blend: "0.50",
    },
    mappings: { instrument_asset_classes: {} },
    assumptions: { coverage_policy: "block", pnl_tolerance: "0.00000001" },
  },
};

export function scenarioConfigJSON(template: ScenarioTemplate): string {
  return JSON.stringify(templates[template], null, 2);
}
