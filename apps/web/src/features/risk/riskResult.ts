import type { RiskRun } from "../../../../../contracts/generated/typescript/contracts";

export interface ScenarioPosition {
  snapshot_line_id: string;
  instrument_id: string;
  state: "valid" | "blocked";
  reason_codes: string[];
  pnl_try?: string | null;
  pnl_usd?: string | null;
}

export interface ScenarioOutput {
  state: "valid" | "degraded" | "blocked";
  reason_codes: string[];
  unmapped_instruments: { instrument_id: string; reason_code: string }[];
  positions: ScenarioPosition[];
  portfolio_pnl_try: string | null;
  portfolio_pnl_usd: string | null;
  pre_metrics: Record<string, unknown>;
  post_metrics: Record<string, unknown>;
  attribution?: unknown;
  input_provenance?: unknown;
}

export interface FactorContribution {
  factor: string;
  contribution: string;
}

export interface PositionAttribution {
  snapshot_line_id: string;
  instrument_id: string;
  state: "valid" | "degraded" | "blocked";
  total_pnl: string | null;
  factor_contributions: FactorContribution[];
  residual: string | null;
}

export interface ScenarioAttribution {
  method: string;
  method_version: string;
  currency: "try" | "usd";
  state: "valid" | "degraded" | "blocked";
  total_pnl: string | null;
  baseline_pnl: string;
  factor_contributions: FactorContribution[];
  interaction_residual: string | null;
  position_contributions: PositionAttribution[];
  tolerance: string;
  reconciles: boolean;
}

function record(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function position(value: unknown): value is ScenarioPosition {
  const item = record(value);
  return (
    !!item &&
    typeof item.snapshot_line_id === "string" &&
    typeof item.instrument_id === "string" &&
    (item.state === "valid" || item.state === "blocked") &&
    Array.isArray(item.reason_codes) &&
    item.reason_codes.every((code) => typeof code === "string") &&
    (item.pnl_try == null || typeof item.pnl_try === "string") &&
    (item.pnl_usd == null || typeof item.pnl_usd === "string")
  );
}

function unmapped(
  value: unknown,
): value is ScenarioOutput["unmapped_instruments"][number] {
  const item = record(value);
  return (
    !!item &&
    typeof item.instrument_id === "string" &&
    typeof item.reason_code === "string"
  );
}

function factorContribution(value: unknown): value is FactorContribution {
  const item = record(value);
  return (
    !!item &&
    typeof item.factor === "string" &&
    typeof item.contribution === "string"
  );
}

function positionAttribution(value: unknown): value is PositionAttribution {
  const item = record(value);
  return (
    !!item &&
    typeof item.snapshot_line_id === "string" &&
    typeof item.instrument_id === "string" &&
    ["valid", "degraded", "blocked"].includes(String(item.state)) &&
    (item.total_pnl === null || typeof item.total_pnl === "string") &&
    Array.isArray(item.factor_contributions) &&
    item.factor_contributions.every(factorContribution) &&
    (item.residual === null || typeof item.residual === "string")
  );
}

export function scenarioAttribution(
  output: ScenarioOutput | null,
  dataQuality?: string,
): ScenarioAttribution | null {
  const value = record(output?.attribution);
  if (
    !value ||
    typeof value.method !== "string" ||
    typeof value.method_version !== "string" ||
    !["try", "usd"].includes(String(value.currency)) ||
    !["valid", "degraded", "blocked"].includes(String(value.state)) ||
    !(value.total_pnl === null || typeof value.total_pnl === "string") ||
    typeof value.baseline_pnl !== "string" ||
    !Array.isArray(value.factor_contributions) ||
    !value.factor_contributions.every(factorContribution) ||
    !(
      value.interaction_residual === null ||
      typeof value.interaction_residual === "string"
    ) ||
    !Array.isArray(value.position_contributions) ||
    !value.position_contributions.every(positionAttribution) ||
    typeof value.tolerance !== "string" ||
    typeof value.reconciles !== "boolean"
  ) {
    return null;
  }
  const attribution = value as unknown as ScenarioAttribution;
  const decimal = /^[-+]?(?:\d+(?:\.\d*)?|\.\d+)$/;
  const validDecimal = (amount: string | null): amount is string =>
    amount !== null && decimal.test(amount);
  // The engine owns the arithmetic verdict; the client rejects only incomplete
  // or state-contradictory evidence and never recomputes contribution totals.
  const completeEvidence =
    output?.state === "valid" &&
    (dataQuality === undefined || dataQuality === "healthy") &&
    output.positions.length > 0 &&
    output.positions.every((item) => item.state === "valid") &&
    attribution.state === "valid" &&
    validDecimal(attribution.baseline_pnl) &&
    validDecimal(attribution.total_pnl) &&
    validDecimal(attribution.interaction_residual) &&
    validDecimal(attribution.tolerance) &&
    Number(attribution.tolerance) >= 0 &&
    attribution.factor_contributions.every((item) =>
      validDecimal(item.contribution),
    ) &&
    attribution.position_contributions.length > 0 &&
    attribution.position_contributions.every(
      (item) =>
        item.state === "valid" &&
        validDecimal(item.total_pnl) &&
        validDecimal(item.residual) &&
        item.factor_contributions.every((factor) =>
          validDecimal(factor.contribution),
        ),
    );

  return {
    ...attribution,
    reconciles: attribution.reconciles && completeEvidence,
  };
}

export function scenarioOutput(run: RiskRun): ScenarioOutput | null {
  const result = record(run.result);
  const output = record(result?.output) ?? result;
  if (
    !output ||
    !["valid", "degraded", "blocked"].includes(String(output.state))
  ) {
    return null;
  }
  if (
    !Array.isArray(output.positions) ||
    !output.positions.every(position) ||
    !Array.isArray(output.unmapped_instruments) ||
    !output.unmapped_instruments.every(unmapped) ||
    !Array.isArray(output.reason_codes) ||
    !output.reason_codes.every((code) => typeof code === "string") ||
    !(
      output.portfolio_pnl_try === null ||
      typeof output.portfolio_pnl_try === "string"
    ) ||
    !(
      output.portfolio_pnl_usd === null ||
      typeof output.portfolio_pnl_usd === "string"
    ) ||
    !record(output.pre_metrics) ||
    !record(output.post_metrics)
  ) {
    return null;
  }
  return output as unknown as ScenarioOutput;
}

export function lifecycleLabel(run: RiskRun): string {
  switch (run.status) {
    case "queued":
      return "Queued";
    case "running":
      return "Running";
    case "retryable":
      return "Retrying after a recoverable failure";
    case "permanent":
      return "Failed permanently";
    case "cancelled":
      return "Cancelled";
    case "completed":
      return run.data_quality === "healthy"
        ? "Valid"
        : run.data_quality === "degraded"
          ? "Degraded"
          : "Blocked";
  }
}
