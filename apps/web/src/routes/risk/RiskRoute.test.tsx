import { act, fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";
import type { RiskRun } from "../../../../../contracts/generated/typescript/contracts";
import { getRiskRun, submitRiskRun } from "../../features/risk/riskApi";
import { RiskRoute } from "./RiskRoute";

vi.mock("../../features/risk/riskApi", () => ({
  getRiskRun: vi.fn(),
  submitRiskRun: vi.fn(),
}));

const run = {
  id: "run-1",
  status: "completed",
  data_quality: "blocked",
  scenario_id: "scenario-1",
  scenario_version: 2,
  account_id: "account-1",
  job_id: "job-1",
  scenario_template: "risk_off",
  snapshot_id: "snapshot-1",
  valuation_id: "valuation-1",
  engine_version: "1.0.0",
  schema_version: "1.0",
  created_at: "2026-01-01T00:00:00Z",
  input_snapshot_ids: ["snapshot-1"],
  scenario_content_sha256: "scenario-hash",
  request_hash: "request-hash",
  reason_codes: ["UNMAPPED_FACTOR"],
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
      pre_metrics: {
        calendar: "business_daily",
        annualization_factor: 252,
        volatility: {
          "asset-1": {
            annualized: null,
            observations: 10,
            required_observations: 63,
            state: "blocked",
          },
        },
        correlations: {},
      },
      post_metrics: {},
      attribution: {
        method: "shapley",
        method_version: "1.0.0",
        currency: "try",
        state: "blocked",
        total_pnl: null,
        baseline_pnl: "0",
        factor_contributions: [],
        interaction_residual: null,
        position_contributions: [
          {
            snapshot_line_id: "line-1",
            instrument_id: "asset-1",
            state: "blocked",
            total_pnl: null,
            factor_contributions: [],
            residual: null,
          },
        ],
        tolerance: "0.00000001",
        reconciles: false,
      },
      input_provenance: {
        state: "blocked",
        cutoff: "2026-01-01T00:00:00Z",
        knowledge_mode: "system_as_of",
        known_at: "2026-01-02T00:00:00Z",
        valuation_result_hash: "valuation-result-hash",
        lines: [
          {
            snapshot_line_id: "line-1",
            price_revision_id: "price-revision-1",
            price_quote_unit: "TRY",
            try_fx_path: [
              {
                quote_revision_id: "try-fx-1",
                pair: "USD/TRY",
                direction: "forward",
              },
            ],
            usd_fx_path: [],
          },
        ],
      },
    },
  },
} as RiskRun;

const reconciledRun = {
  ...run,
  data_quality: "healthy",
  result: {
    output: {
      ...(run.result as { output: Record<string, unknown> }).output,
      state: "valid",
      reason_codes: [],
      unmapped_instruments: [],
      positions: [
        {
          snapshot_line_id: "line-1",
          instrument_id: "asset-1",
          state: "valid",
          reason_codes: [],
          pnl_try: "-100",
          pnl_usd: "-3",
        },
      ],
      portfolio_pnl_try: "-100",
      portfolio_pnl_usd: "-3",
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
      input_provenance: {
        state: "valid",
        cutoff: "2026-01-01T00:00:00Z",
        knowledge_mode: "system_as_of",
        known_at: "2026-01-02T00:00:00Z",
        valuation_result_hash: "valuation-result-hash",
        lines: [
          {
            snapshot_line_id: "line-1",
            price_revision_id: "price-revision-1",
            price_quote_unit: "TRY",
            try_fx_path: [
              {
                quote_revision_id: "try-fx-1",
                pair: "USD/TRY",
                direction: "forward",
              },
            ],
            usd_fx_path: [],
          },
        ],
      },
    },
  },
} as RiskRun;

it("keeps blocked losses incomplete, unmapped assets visible, and provenance inspectable", async () => {
  vi.mocked(getRiskRun).mockResolvedValue(run);
  render(<RiskRoute />);
  fireEvent.change(screen.getByLabelText("Run ID"), {
    target: { value: "run-1" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Open run" }));
  expect(
    await screen.findByRole("heading", { name: "Blocked" }),
  ).toBeInTheDocument();
  expect(screen.getByText(/unmapped instruments/i)).toBeInTheDocument();
  expect(screen.getByText(/asset-1: UNMAPPED_FACTOR/)).toBeInTheDocument();
  expect(screen.getByText("TRY covered-position P&L")).toBeInTheDocument();
  expect(screen.queryByText("TRY total P&L")).not.toBeInTheDocument();
  expect(
    screen.getByText(/Attribution does not reconcile/),
  ).toBeInTheDocument();
  expect(screen.getByText("Server total")).toBeInTheDocument();
  fireEvent.click(screen.getByText(/Position attribution/));
  expect(
    screen.getByRole("table", {
      name: "Server-calculated position contributions",
    }),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByText("Provenance and immutable versions"));
  expect(screen.getByText("scenario-hash")).toBeInTheDocument();
  expect(screen.getByText("account-1")).toBeInTheDocument();
  expect(screen.getAllByText("snapshot-1").length).toBeGreaterThan(0);
  expect(screen.getByText("valuation-1")).toBeInTheDocument();
  expect(screen.getByText("valuation-result-hash")).toBeInTheDocument();
  expect(screen.getByText(/price-revision-1/)).toBeInTheDocument();
  expect(screen.getByText(/try-fx-1/)).toBeInTheDocument();
});

it("shows server factor and residual reconciliation in a loss waterfall", async () => {
  vi.mocked(getRiskRun).mockResolvedValue(reconciledRun);
  render(<RiskRoute />);
  fireEvent.change(screen.getByLabelText("Run ID"), {
    target: { value: "run-1" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Open run" }));

  expect(await screen.findByText("Loss waterfall · TRY")).toBeInTheDocument();
  expect(screen.getAllByText("equity").length).toBeGreaterThan(0);
  expect(screen.getAllByText("-80").length).toBeGreaterThan(0);
  expect(screen.getAllByText("-15").length).toBeGreaterThan(0);
  expect(screen.getAllByText("-5").length).toBeGreaterThan(0);
  expect(
    screen.getByText(/Server reports reconciliation within tolerance/),
  ).toBeInTheDocument();
});

it("reports API failures and preserves the run ID for retry", async () => {
  vi.mocked(getRiskRun)
    .mockRejectedValueOnce(new Error("Temporary server error"))
    .mockResolvedValueOnce(run);
  render(<RiskRoute />);
  fireEvent.change(screen.getByLabelText("Run ID"), {
    target: { value: "run-2" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Open run" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Temporary server error",
  );
  expect(screen.getByLabelText("Run ID")).toHaveValue("run-2");
  fireEvent.click(screen.getByRole("button", { name: "Retry run" }));
  expect(
    await screen.findByRole("heading", { name: "Blocked" }),
  ).toBeInTheDocument();
});

it("submits a versioned run bound to account, snapshot, and valuation IDs", async () => {
  vi.mocked(submitRiskRun).mockResolvedValue(run);
  vi.mocked(getRiskRun).mockResolvedValue(run);
  render(<RiskRoute />);

  fireEvent.change(screen.getByLabelText("Account ID"), {
    target: { value: "account-1" },
  });
  fireEvent.change(screen.getByLabelText("Snapshot ID"), {
    target: { value: "snapshot-1" },
  });
  fireEvent.change(screen.getByLabelText("Valuation ID"), {
    target: { value: "valuation-1" },
  });
  fireEvent.change(screen.getByLabelText("Scenario name"), {
    target: { value: "Downside review" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Run scenario" }));

  expect(
    await screen.findByRole("heading", { name: "Blocked" }),
  ).toBeInTheDocument();
  expect(submitRiskRun).toHaveBeenCalledWith(
    expect.objectContaining({
      account_id: "account-1",
      snapshot_id: "snapshot-1",
      valuation_id: "valuation-1",
      name: "Downside review",
      template_key: "risk_off",
      mappings: { instrument_asset_classes: {} },
    }),
    expect.any(String),
  );
});

it("does not let a late create response replace a run selected meanwhile", async () => {
  let resolveSubmit: (value: RiskRun) => void = () => {};
  vi.mocked(submitRiskRun).mockReturnValue(
    new Promise((resolve) => {
      resolveSubmit = resolve;
    }),
  );
  const otherRun = {
    ...reconciledRun,
    id: "other-run",
    account_id: "other-account",
  };
  vi.mocked(getRiskRun).mockResolvedValue(otherRun);
  render(<RiskRoute />);

  fireEvent.change(screen.getByLabelText("Account ID"), {
    target: { value: "account-1" },
  });
  fireEvent.change(screen.getByLabelText("Snapshot ID"), {
    target: { value: "snapshot-1" },
  });
  fireEvent.change(screen.getByLabelText("Valuation ID"), {
    target: { value: "valuation-1" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Run scenario" }));

  fireEvent.change(screen.getByLabelText("Run ID"), {
    target: { value: "other-run" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Open run" }));
  expect(
    await screen.findByRole("heading", { name: "Valid" }),
  ).toBeInTheDocument();
  fireEvent.click(screen.getByText("Provenance and immutable versions"));
  expect(screen.getByText("other-account")).toBeInTheDocument();

  await act(async () => resolveSubmit(run));
  expect(screen.getByText("other-account")).toBeInTheDocument();
  expect(screen.getByLabelText("Run ID")).toHaveValue("other-run");

  vi.mocked(submitRiskRun).mockResolvedValueOnce(run);
  fireEvent.click(screen.getByRole("button", { name: "Run scenario" }));
  await screen.findByRole("heading", { name: "Blocked" });
  expect(submitRiskRun).toHaveBeenCalledTimes(2);
  expect(vi.mocked(submitRiskRun).mock.calls[1]?.[1]).toBe(
    vi.mocked(submitRiskRun).mock.calls[0]?.[1],
  );
});

it("preserves risk input and reuses its idempotency key after a recoverable error", async () => {
  vi.mocked(submitRiskRun)
    .mockRejectedValueOnce(new Error("Temporary server error"))
    .mockResolvedValueOnce(run);
  vi.mocked(getRiskRun).mockResolvedValue(run);
  render(<RiskRoute />);

  fireEvent.change(screen.getByLabelText("Account ID"), {
    target: { value: "account-1" },
  });
  fireEvent.change(screen.getByLabelText("Snapshot ID"), {
    target: { value: "snapshot-1" },
  });
  fireEvent.change(screen.getByLabelText("Valuation ID"), {
    target: { value: "valuation-1" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Run scenario" }));

  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Temporary server error",
  );
  expect(screen.getByLabelText("Account ID")).toHaveValue("account-1");
  expect(screen.getByLabelText("Snapshot ID")).toHaveValue("snapshot-1");
  expect(screen.getByLabelText("Valuation ID")).toHaveValue("valuation-1");
  const configuration = screen.getByLabelText("Configuration JSON");
  fireEvent.click(screen.getByRole("button", { name: "Run scenario" }));

  await screen.findByRole("heading", { name: "Blocked" });
  expect(submitRiskRun).toHaveBeenCalledTimes(2);
  expect(vi.mocked(submitRiskRun).mock.calls[0]?.[1]).toBe(
    vi.mocked(submitRiskRun).mock.calls[1]?.[1],
  );
  expect((configuration as HTMLTextAreaElement).value).toContain(
    "instrument_asset_classes",
  );
});
