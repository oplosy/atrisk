import type { RiskRun } from "../../../../../contracts/generated/typescript/contracts";
import { requestJson, requestJsonWithInit } from "../../app/api";
import type { ScenarioTemplate } from "./scenarioTemplates";

export interface RiskRunSubmission {
  account_id: string;
  snapshot_id: string;
  valuation_id: string;
  scenario_id?: string;
  name: string;
  template_key: ScenarioTemplate;
  units: Record<string, unknown>;
  shocks: Record<string, unknown>;
  mappings: Record<string, unknown>;
  assumptions: Record<string, unknown>;
}

export function getRiskRun(runId: string): Promise<RiskRun> {
  return requestJson<RiskRun>(`/api/v1/risk/runs/${encodeURIComponent(runId)}`);
}

export function submitRiskRun(
  submission: RiskRunSubmission,
  idempotencyKey: string,
  signal?: AbortSignal,
): Promise<RiskRun> {
  return requestJsonWithInit<RiskRun>("/api/v1/risk/runs", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      "Idempotency-Key": idempotencyKey,
    },
    body: JSON.stringify(submission),
    signal,
  });
}
