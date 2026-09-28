import type { RiskRun } from "../../../../../contracts/generated/typescript/contracts";
import {
  lifecycleLabel,
  scenarioAttribution,
  scenarioOutput,
} from "./riskResult";

const run = {
  id: "run-1",
  scenario_id: "scenario-1",
  scenario_version: 1,
  account_id: "account-1",
  snapshot_id: "snapshot-1",
  valuation_id: "valuation-1",
  job_id: "job-1",
  status: "completed",
  data_quality: "blocked",
  schema_version: "1.0",
  engine_version: "1.0.0",
  scenario_template: "risk_off",
  scenario_content_sha256: "hash",
  request_hash: "hash",
  input_snapshot_ids: ["snapshot-1"],
  reason_codes: [],
  created_at: "2026-01-01T00:00:00Z",
  result: {
    output: {
      state: "blocked",
      reason_codes: ["UNMAPPED_FACTOR"],
      unmapped_instruments: [
        { instrument_id: "asset-1", reason_code: "UNMAPPED_FACTOR" },
      ],
      positions: [
        {
          snapshot_line_id: "line-1",
          instrument_id: "asset-1",
          state: "blocked",
          reason_codes: ["UNMAPPED_FACTOR"],
        },
      ],
      portfolio_pnl_try: null,
      portfolio_pnl_usd: null,
      pre_metrics: {},
      post_metrics: {},
      attribution: {
        method: "shapley",
        method_version: "1.0.0",
        currency: "try",
        state: "valid",
        total_pnl: "-100",
        baseline_pnl: "0",
        factor_contributions: [
          { factor: "equity", contribution: "-80" },
          { factor: "fx", contribution: "-15" },
        ],
        interaction_residual: "-5",
        position_contributions: [
          {
            snapshot_line_id: "line-1",
            instrument_id: "asset-1",
            state: "valid",
            total_pnl: "-100",
            factor_contributions: [{ factor: "equity", contribution: "-100" }],
            residual: "0",
          },
        ],
        tolerance: "0.01",
        reconciles: true,
      },
    },
  },
} as RiskRun;

it("reads the worker result envelope without hiding unmapped positions", () => {
  expect(scenarioOutput(run)?.unmapped_instruments).toEqual([
    { instrument_id: "asset-1", reason_code: "UNMAPPED_FACTOR" },
  ]);
  expect(lifecycleLabel(run)).toBe("Blocked");
});

it("accepts server-sealed factor, position, and interaction attribution", () => {
  const output = scenarioOutput(run);
  const attribution = scenarioAttribution(output);
  expect(attribution?.factor_contributions).toEqual([
    { factor: "equity", contribution: "-80" },
    { factor: "fx", contribution: "-15" },
  ]);
  expect(attribution?.position_contributions[0]?.total_pnl).toBe("-100");
  expect(attribution?.interaction_residual).toBe("-5");
  expect(attribution?.reconciles).toBe(false);
});

it("fails closed when a blocked output claims reconciled attribution", () => {
  const output = scenarioOutput(run)!;
  const attribution = scenarioAttribution(
    {
      ...output,
      attribution: {
        ...(output.attribution as Record<string, unknown>),
        state: "blocked",
        total_pnl: null,
        interaction_residual: null,
        reconciles: true,
      },
    },
    "blocked",
  );
  expect(attribution?.reconciles).toBe(false);
});

it("does not report reconciliation with incomplete decimal evidence", () => {
  const output = scenarioOutput(run)!;
  const attribution = scenarioAttribution(
    {
      ...output,
      state: "valid",
      attribution: {
        ...(output.attribution as Record<string, unknown>),
        state: "valid",
        total_pnl: null,
        interaction_residual: null,
        reconciles: true,
      },
    },
    "healthy",
  );
  expect(attribution?.reconciles).toBe(false);
});

it("does not report reconciliation when position attribution is empty", () => {
  const output = scenarioOutput(run)!;
  const attribution = scenarioAttribution(
    {
      ...output,
      state: "valid",
      positions: output.positions.map((item) => ({ ...item, state: "valid" })),
      attribution: {
        ...(output.attribution as Record<string, unknown>),
        state: "valid",
        total_pnl: "-100",
        baseline_pnl: "0",
        factor_contributions: [{ factor: "equity", contribution: "-95" }],
        interaction_residual: "-5",
        position_contributions: [],
        tolerance: "0.01",
        reconciles: true,
      },
    },
    "healthy",
  );
  expect(attribution?.reconciles).toBe(false);
});

it("rejects incomplete attribution instead of implying a reconciliation", () => {
  const output = scenarioOutput(run);
  expect(
    scenarioAttribution({
      ...output!,
      attribution: { state: "valid", factor_contributions: [] },
    }),
  ).toBeNull();
});

it("rejects malformed result shapes rather than presenting them as valid", () => {
  expect(
    scenarioOutput({ ...run, result: { output: { state: "valid" } } }),
  ).toBeNull();
  expect(
    scenarioOutput({
      ...run,
      result: {
        output: {
          ...(run.result as { output: Record<string, unknown> }).output,
          positions: [{ instrument_id: "asset-1" }],
        },
      },
    }),
  ).toBeNull();
});

it.each([
  ["queued", "Queued"],
  ["running", "Running"],
  ["retryable", "Retrying after a recoverable failure"],
  ["permanent", "Failed permanently"],
  ["cancelled", "Cancelled"],
] as const)("labels %s explicitly", (status, label) => {
  expect(lifecycleLabel({ ...run, status })).toBe(label);
});
