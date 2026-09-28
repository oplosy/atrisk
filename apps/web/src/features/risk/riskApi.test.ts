import { afterEach, expect, it, vi } from "vitest";
import { submitRiskRun } from "./riskApi";

afterEach(() => {
  vi.unstubAllGlobals();
});

it("posts only the sealed valuation references with an idempotency key", async () => {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify({ id: "run-1" }), {
      status: 202,
      headers: { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fetchMock);

  const submission = {
    account_id: "account-1",
    snapshot_id: "snapshot-1",
    valuation_id: "valuation-1",
    name: "Risk scenario",
    template_key: "risk_off" as const,
    units: {},
    shocks: {},
    mappings: {},
    assumptions: {},
  };
  await submitRiskRun(submission, "idempotency-1");

  expect(fetchMock).toHaveBeenCalledTimes(1);
  const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
  expect(url).toBe("/api/v1/risk/runs");
  expect(init.method).toBe("POST");
  const headers = new Headers(init.headers);
  expect(headers.get("Idempotency-Key")).toBe("idempotency-1");
  expect(headers.get("Content-Type")).toBe("application/json");
  expect(JSON.parse(String(init.body))).toEqual(submission);
  expect(JSON.parse(String(init.body))).not.toHaveProperty("positions");
  expect(JSON.parse(String(init.body))).not.toHaveProperty("pre_metrics");
});
