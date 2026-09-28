import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { vi } from "vitest";

import {
  api,
  portfolioApi,
  sha256,
} from "../../features/portfolio/portfolioApi";
import { PortfolioRoute } from "./PortfolioRoute";

vi.mock("../../features/portfolio/portfolioApi", () => ({
  api: vi.fn(),
  sha256: vi.fn(),
  importBody: vi.fn(() => new FormData()),
  portfolioApi: {
    portfolios: vi.fn(),
    accounts: vi.fn(),
    instruments: vi.fn(),
    snapshots: vi.fn(),
    createSnapshot: vi.fn(),
    createValuation: vi.fn(),
    reconcile: vi.fn(),
  },
}));

const portfolio = {
  id: "portfolio-1",
  name: "Primary",
  reporting_currency: "TRY" as const,
  metadata: {},
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

const secondaryPortfolio = {
  ...portfolio,
  id: "portfolio-2",
  name: "Secondary",
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(portfolioApi.portfolios).mockResolvedValue({
    items: [portfolio, secondaryPortfolio],
  });
  vi.mocked(portfolioApi.accounts).mockResolvedValue({
    items: [
      {
        id: "account-1",
        portfolio_id: portfolio.id,
        name: "Account",
        metadata: {},
        created_at: portfolio.created_at,
        updated_at: portfolio.updated_at,
      },
    ],
  });
  vi.mocked(portfolioApi.instruments).mockResolvedValue({
    items: [
      {
        id: "instrument-1",
        canonical_symbol: "USD",
        instrument_type: "currency",
        native_unit: "USD",
        external_ids: [],
        status: "active",
        created_at: portfolio.created_at,
      },
    ],
  });
  vi.mocked(portfolioApi.snapshots).mockResolvedValue({ items: [] });
  vi.mocked(sha256).mockResolvedValue("a".repeat(64));
});

it("locks CSV commit until a valid preview of the same content", async () => {
  vi.mocked(api).mockResolvedValueOnce({
    token: "preview-token",
    content_sha256: "a".repeat(64),
    valid: true,
    row_count: 1,
    diagnostics: [],
  });
  render(<PortfolioRoute />);
  await screen.findByRole("option", { name: "Primary" });
  const commit = screen.getByRole("button", { name: "Commit previewed CSV" });
  expect(commit).toBeDisabled();
  fireEvent.change(screen.getByLabelText("CSV file"), {
    target: { files: [new File(["x"], "positions.csv", { type: "text/csv" })] },
  });
  expect(commit).toBeDisabled();
  fireEvent.click(screen.getByRole("button", { name: "Preview CSV" }));
  await waitFor(() => expect(commit).toBeEnabled());
  fireEvent.change(screen.getByLabelText("Captured at"), {
    target: { value: "2026-01-02T12:00" },
  });
  expect(commit).toBeDisabled();
});

it("keeps commit locked when the server preview hash differs", async () => {
  vi.mocked(api).mockResolvedValueOnce({
    token: "preview-token",
    content_sha256: "b".repeat(64),
    valid: true,
    row_count: 1,
    diagnostics: [],
  });
  render(<PortfolioRoute />);
  await screen.findByRole("option", { name: "Primary" });
  fireEvent.change(screen.getByLabelText("CSV file"), {
    target: { files: [new File(["x"], "positions.csv", { type: "text/csv" })] },
  });
  fireEvent.click(screen.getByRole("button", { name: "Preview CSV" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Preview hash differs",
  );
  expect(
    screen.getByRole("button", { name: "Commit previewed CSV" }),
  ).toBeDisabled();
});

it("sends all manual positions in one immutable snapshot", async () => {
  vi.mocked(portfolioApi.createSnapshot).mockResolvedValue({
    id: "snapshot-2",
    portfolio_id: portfolio.id,
    captured_at: portfolio.created_at,
    created_at: portfolio.created_at,
    lines: [],
  });
  render(<PortfolioRoute />);
  await screen.findByRole("option", { name: "Primary" });
  await waitFor(() => expect(portfolioApi.accounts).toHaveBeenCalled());
  fireEvent.change(screen.getByLabelText("Instrument"), {
    target: { value: "instrument-1" },
  });
  fireEvent.change(screen.getByLabelText("Quantity"), {
    target: { value: "12.345" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Add position" }));
  fireEvent.change(screen.getAllByLabelText("Instrument")[1], {
    target: { value: "instrument-1" },
  });
  fireEvent.change(screen.getAllByLabelText("Quantity")[1], {
    target: { value: "7.5" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create snapshot" }));
  await waitFor(() =>
    expect(portfolioApi.createSnapshot).toHaveBeenCalledWith(
      portfolio.id,
      expect.objectContaining({
        lines: [
          {
            account_id: "account-1",
            instrument_id: "instrument-1",
            quantity: "12.345",
          },
          {
            account_id: "account-1",
            instrument_id: "instrument-1",
            quantity: "7.5",
          },
        ],
      }),
    ),
  );
});

it("ignores a delayed snapshot result after switching portfolios", async () => {
  const pending = deferred<{
    id: string;
    portfolio_id: string;
    captured_at: string;
    created_at: string;
    lines: never[];
  }>();
  vi.mocked(portfolioApi.createSnapshot).mockReturnValueOnce(pending.promise);
  render(<PortfolioRoute />);
  await screen.findByRole("option", { name: "Primary" });
  await waitFor(() => expect(portfolioApi.accounts).toHaveBeenCalled());
  fireEvent.change(screen.getByLabelText("Instrument"), {
    target: { value: "instrument-1" },
  });
  fireEvent.change(screen.getByLabelText("Quantity"), {
    target: { value: "12.345" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create snapshot" }));
  await waitFor(() => expect(portfolioApi.createSnapshot).toHaveBeenCalled());

  fireEvent.change(screen.getByLabelText("Portfolio"), {
    target: { value: secondaryPortfolio.id },
  });
  await waitFor(() =>
    expect(portfolioApi.accounts).toHaveBeenCalledWith(secondaryPortfolio.id),
  );
  pending.resolve({
    id: "snapshot-old",
    portfolio_id: portfolio.id,
    captured_at: portfolio.created_at,
    created_at: portfolio.created_at,
    lines: [],
  });

  await waitFor(() =>
    expect(
      screen.queryByText("Snapshot snapshot-old created."),
    ).not.toBeInTheDocument(),
  );
  expect(screen.getByLabelText("Portfolio")).toHaveValue(secondaryPortfolio.id);
});

it("keeps manual input after a recoverable server error", async () => {
  vi.mocked(portfolioApi.createSnapshot).mockRejectedValue(
    new Error("Temporary server error"),
  );
  render(<PortfolioRoute />);
  await screen.findByRole("option", { name: "Primary" });
  await waitFor(() => expect(portfolioApi.accounts).toHaveBeenCalled());
  fireEvent.change(screen.getByLabelText("Instrument"), {
    target: { value: "instrument-1" },
  });
  fireEvent.change(screen.getByLabelText("Quantity"), {
    target: { value: "12.345" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Create snapshot" }));
  expect(await screen.findByRole("alert")).toHaveTextContent(
    "Temporary server error",
  );
  expect(screen.getByLabelText("Quantity")).toHaveValue("12.345");
});

it("ignores a delayed valuation result after switching portfolios", async () => {
  const valuation = deferred<{
    id: string;
    snapshot_id: string;
    cutoff: string;
    known_at: string;
    knowledge_mode: "system_as_of";
    price_max_age_seconds: number;
    fx_max_age_seconds: number;
    state: "blocked";
    result_hash: string;
    created_at: string;
    totals: { try: null; usd: null };
    lines: never[];
  }>();
  vi.mocked(portfolioApi.snapshots).mockResolvedValue({
    items: [
      {
        id: "snapshot-1",
        portfolio_id: portfolio.id,
        captured_at: portfolio.created_at,
        created_at: portfolio.created_at,
        lines: [],
      },
    ],
  });
  vi.mocked(portfolioApi.createValuation).mockReturnValueOnce(
    valuation.promise,
  );
  render(<PortfolioRoute />);
  await waitFor(() =>
    expect(screen.getByLabelText("Snapshot")).toHaveValue("snapshot-1"),
  );
  fireEvent.click(screen.getByRole("button", { name: "Record valuation" }));
  await waitFor(() => expect(portfolioApi.createValuation).toHaveBeenCalled());

  fireEvent.change(screen.getByLabelText("Portfolio"), {
    target: { value: secondaryPortfolio.id },
  });
  await waitFor(() =>
    expect(portfolioApi.accounts).toHaveBeenCalledWith(secondaryPortfolio.id),
  );
  valuation.resolve({
    id: "valuation-old",
    snapshot_id: "snapshot-1",
    cutoff: portfolio.created_at,
    known_at: portfolio.created_at,
    knowledge_mode: "system_as_of",
    price_max_age_seconds: 100,
    fx_max_age_seconds: 100,
    state: "blocked",
    result_hash: "hash",
    created_at: portfolio.created_at,
    totals: { try: null, usd: null },
    lines: [],
  });

  await waitFor(() =>
    expect(
      screen.queryByRole("heading", { name: "Valuation result" }),
    ).not.toBeInTheDocument(),
  );
  expect(screen.getByLabelText("Portfolio")).toHaveValue(secondaryPortfolio.id);
});

it("ignores a delayed CSV snapshot refresh after switching portfolios", async () => {
  const refresh = deferred<{ items: never[] }>();
  vi.mocked(portfolioApi.snapshots).mockReset();
  vi.mocked(portfolioApi.snapshots)
    .mockResolvedValueOnce({ items: [] })
    .mockReturnValueOnce(refresh.promise)
    .mockResolvedValue({ items: [] });
  vi.mocked(api)
    .mockResolvedValueOnce({
      token: "preview-token",
      content_sha256: "a".repeat(64),
      valid: true,
      row_count: 1,
      diagnostics: [],
    })
    .mockResolvedValueOnce({
      snapshot_id: "snapshot-old",
      content_sha256: "a".repeat(64),
    });
  render(<PortfolioRoute />);
  await screen.findByRole("option", { name: "Primary" });
  fireEvent.change(screen.getByLabelText("CSV file"), {
    target: { files: [new File(["x"], "positions.csv", { type: "text/csv" })] },
  });
  fireEvent.click(screen.getByRole("button", { name: "Preview CSV" }));
  const commit = await screen.findByRole("button", {
    name: "Commit previewed CSV",
  });
  await waitFor(() => expect(commit).toBeEnabled());
  fireEvent.click(commit);
  await waitFor(() => expect(portfolioApi.snapshots).toHaveBeenCalledTimes(2));

  fireEvent.change(screen.getByLabelText("Portfolio"), {
    target: { value: secondaryPortfolio.id },
  });
  refresh.resolve({ items: [] });

  await waitFor(() =>
    expect(
      screen.queryByText("CSV committed as snapshot snapshot-old."),
    ).not.toBeInTheDocument(),
  );
  expect(screen.getByLabelText("Portfolio")).toHaveValue(secondaryPortfolio.id);
});

it("shows blocked valuation without a full NAV and exposes ordered FX evidence", async () => {
  vi.mocked(portfolioApi.snapshots).mockResolvedValue({
    items: [
      {
        id: "snapshot-1",
        portfolio_id: portfolio.id,
        captured_at: portfolio.created_at,
        created_at: portfolio.created_at,
        lines: [],
      },
    ],
  });
  vi.mocked(portfolioApi.createValuation).mockResolvedValue({
    id: "valuation-1",
    snapshot_id: "snapshot-1",
    cutoff: portfolio.created_at,
    known_at: portfolio.created_at,
    knowledge_mode: "system_as_of",
    price_max_age_seconds: 100,
    fx_max_age_seconds: 100,
    state: "blocked",
    result_hash: "hash",
    created_at: portfolio.created_at,
    totals: { try: null, usd: null },
    lines: [
      {
        snapshot_line_id: "line-1",
        instrument_id: "instrument-1",
        native_currency: "USD",
        native_amount: "5",
        try_amount: null,
        usd_amount: "5",
        state: "blocked",
        reason_codes: [{ code: "MISSING_FX", message: "No TRY quote" }],
        price_method: "revision",
        price_revision_id: "price-1",
        price_quote_unit: "USD",
        try_fx_path: [
          { quote_revision_id: "fx-1", direction: "forward" },
          { quote_revision_id: "fx-2", direction: "reverse" },
        ],
        usd_fx_path: [],
      },
    ],
  });
  render(<PortfolioRoute />);
  await waitFor(() =>
    expect(screen.getByLabelText("Snapshot")).toHaveValue("snapshot-1"),
  );
  fireEvent.click(screen.getByRole("button", { name: "Record valuation" }));
  expect(await screen.findByText(/Incomplete valuation/)).toBeInTheDocument();
  expect(screen.queryByText(/Complete NAV:/)).not.toBeInTheDocument();
  fireEvent.click(screen.getByText("Price and FX evidence"));
  expect(screen.getByText(/price-1/)).toBeInTheDocument();
  expect(screen.getByText(/fx-1/)).toBeInTheDocument();
  expect(screen.getByText(/fx-2/)).toBeInTheDocument();
});
