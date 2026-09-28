import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { DecisionJournalRoute } from "./DecisionsRoute";

const decision = {
  id: "decision-1",
  account_id: "account-1",
  thesis: "Rates remain restrictive",
  alternatives: [],
  evidence_references: [
    { kind: "portfolio_snapshot", reference: "snapshot-1" },
    { kind: "valuation_run", reference: "valuation-1" },
    { kind: "risk_run", reference: "risk-1" },
  ],
  invalidation_conditions: [
    { condition: "Inflation falls", metric: "CPI", threshold: "2%" },
  ],
  horizon: { start: "2026-01-01T00:00:00Z", end: "2026-02-01T00:00:00Z" },
  risk_budget: {
    amount: "1000",
    currency: "TRY",
    measure: "absolute loss",
    horizon: "30 days",
  },
  intended_action: "Review monthly",
  tags: [],
  status: "draft" as const,
  author: "Mesut",
  source_metadata: {},
  created_at: "2026-01-01T00:00:00Z",
  finalized_at: null,
};

const timeline = {
  decision,
  events: [
    {
      id: "event-1",
      decision_id: "decision-1",
      kind: "decision" as const,
      payload: { thesis: decision.thesis },
      author: "Mesut",
      source_metadata: {},
      created_at: decision.created_at,
    },
  ],
};

function jsonResponse(body: unknown, status = 200) {
  return Promise.resolve(
    new Response(JSON.stringify(body), {
      status,
      headers: { "Content-Type": "application/json" },
    }),
  );
}

function fillDraft(withEvidence = true) {
  fireEvent.change(screen.getByLabelText("Account ID"), {
    target: { value: "account-1" },
  });
  fireEvent.change(screen.getByLabelText("Author"), {
    target: { value: "Mesut" },
  });
  fireEvent.change(screen.getByLabelText("Thesis"), {
    target: { value: "Rates remain restrictive" },
  });
  fireEvent.change(screen.getByLabelText("Risk budget amount"), {
    target: { value: "1000" },
  });
  fireEvent.change(screen.getByLabelText("Intended action statement"), {
    target: { value: "Review monthly" },
  });
  fireEvent.change(screen.getByLabelText("Condition"), {
    target: { value: "Inflation falls" },
  });
  if (withEvidence) {
    ["snapshot-1", "valuation-1", "risk-1"].forEach((value, index) =>
      fireEvent.change(screen.getAllByLabelText("Reference ID")[index], {
        target: { value },
      }),
    );
  }
}

describe("DecisionJournalRoute", () => {
  beforeEach(() => {
    window.localStorage.clear();
    vi.restoreAllMocks();
  });

  it("recovers a local draft and exposes a non-executable journal boundary", () => {
    window.localStorage.setItem(
      "atlasrisk.decision-journal.draft.v1",
      JSON.stringify({
        ...decision,
        accountId: "account-1",
        thesis: decision.thesis,
      }),
    );
    render(<DecisionJournalRoute />);
    expect(screen.getByText("Recovered local draft.")).toBeInTheDocument();
    expect(
      screen.getByText(/Journal statement · no execution/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/never an executable control/i),
    ).toBeInTheDocument();
  });

  it("requires every required evidence ID before the first draft POST", () => {
    const fetchMock = vi.spyOn(window, "fetch");
    render(<DecisionJournalRoute />);
    fillDraft(false);
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    expect(
      screen.getByText(
        /Add required evidence references before saving: Portfolio Snapshot, Valuation Run, Risk Run/i,
      ),
    ).toBeInTheDocument();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("omits an empty optional evidence row from the draft request", async () => {
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input, init) => {
        const path = String(input);
        if (path.endsWith("/decisions") && init?.method === "POST")
          return jsonResponse(decision, 201);
        if (path.endsWith("/timeline")) return jsonResponse(timeline);
        if (path.endsWith("/decisions/decision-1"))
          return jsonResponse(decision);
        return jsonResponse({}, 404);
      });
    render(<DecisionJournalRoute />);
    fillDraft();
    fireEvent.click(
      screen.getByRole("button", { name: "Add evidence reference" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await screen.findByText(/Draft decision-1 saved/i);
    const createCall = fetchMock.mock.calls.find(
      ([input, init]) =>
        String(input).endsWith("/decisions") && init?.method === "POST",
    );
    expect(createCall).toBeDefined();
    const body = JSON.parse(String(createCall?.[1]?.body));
    expect(body.evidence_references).toEqual(decision.evidence_references);
  });

  it("creates a draft, previews all required seal references, and blocks duplicate finalization", async () => {
    const fetchMock = vi
      .spyOn(window, "fetch")
      .mockImplementation((input, init) => {
        const path = String(input);
        if (path.endsWith("/decisions") && init?.method === "POST")
          return jsonResponse(decision, 201);
        if (path.endsWith("/timeline")) return jsonResponse(timeline);
        if (path.endsWith("/decisions/decision-1"))
          return jsonResponse(decision);
        if (path.endsWith("/finalize"))
          return jsonResponse({ ...decision, status: "finalized" }, 200);
        if (path.endsWith("/evidence"))
          return jsonResponse({
            manifest: {
              snapshot_id: "snapshot-1",
              valuation_id: "valuation-1",
              risk_id: "risk-1",
              engine_version: "risk-v1",
            },
            sha256: "a".repeat(64),
          });
        return jsonResponse({}, 404);
      });
    render(<DecisionJournalRoute />);
    fillDraft();
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await screen.findByText(/Draft decision-1 saved/i);
    expect(screen.getByText("Portfolio Snapshot")).toBeInTheDocument();
    expect(screen.getByText("snapshot-1")).toBeInTheDocument();
    expect(screen.getByText("valuation-1")).toBeInTheDocument();
    expect(screen.getByText("risk-1")).toBeInTheDocument();
    screen
      .getAllByLabelText("Reference ID")
      .forEach((input) => expect(input).toBeDisabled());
    expect(screen.getByRole("button", { name: "Draft saved" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Draft saved" }));
    expect(
      fetchMock.mock.calls.filter(
        ([input, init]) =>
          String(input).endsWith("/decisions") && init?.method === "POST",
      ),
    ).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Finalize decision" }));
    await screen.findByText("Sealed evidence reconstruction");
    expect(
      screen.getByRole("button", { name: "Finalize decision" }),
    ).toBeDisabled();
    expect(fetchMock).toHaveBeenCalledWith(
      "/api/v1/decisions/decision-1/finalize",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("treats an evidence integrity response as a blocking state", async () => {
    vi.spyOn(window, "fetch").mockImplementation((input, init) => {
      const path = String(input);
      if (path.endsWith("/decisions") && init?.method === "POST")
        return jsonResponse(decision, 201);
      if (path.endsWith("/timeline")) return jsonResponse(timeline);
      if (path.endsWith("/decisions/decision-1")) return jsonResponse(decision);
      if (path.endsWith("/finalize"))
        return jsonResponse(
          { code: "EVIDENCE_INTEGRITY_FAILURE", message: "hash mismatch" },
          409,
        );
      return jsonResponse({}, 404);
    });
    render(<DecisionJournalRoute />);
    fillDraft();
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await screen.findByText(/Draft decision-1 saved/i);
    fireEvent.click(screen.getByRole("button", { name: "Finalize decision" }));
    await waitFor(() =>
      expect(
        screen.getByText(
          /Finalization blocked: sealed evidence failed integrity validation/i,
        ),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByText(/Latest data fallback is not permitted/i),
    ).toBeInTheDocument();
  });
});
