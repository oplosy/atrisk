import { requestJson } from "../../app/api";

export interface EvidenceReference {
  kind: string;
  reference: string;
  description?: string;
}

export interface InvalidationCondition {
  condition: string;
  metric?: string;
  threshold?: string;
}

export interface Decision {
  id: string;
  account_id: string;
  thesis: string;
  alternatives: string[];
  evidence_references: EvidenceReference[];
  invalidation_conditions: InvalidationCondition[];
  horizon: { start: string; end: string };
  risk_budget: {
    amount: string;
    currency: string;
    measure: string;
    horizon: string;
  };
  intended_action: string;
  tags: string[];
  status: "draft" | "finalized";
  author: string;
  source_metadata: Record<string, unknown>;
  created_at: string;
  finalized_at?: string | null;
}

export interface DecisionEvidence {
  manifest: Record<string, unknown>;
  sha256: string;
}

export interface DecisionReview {
  id: string;
  decision_id: string;
  review: string;
  outcome: string;
  author: string;
  source_metadata: Record<string, unknown>;
  created_at: string;
}

export interface DecisionAmendment {
  id: string;
  decision_id: string;
  summary: string;
  changes: Record<string, unknown>;
  author: string;
  source_metadata: Record<string, unknown>;
  created_at: string;
}

export interface DecisionTimelineEvent {
  id: string;
  decision_id: string;
  kind: "decision" | "review" | "amendment";
  payload: unknown;
  author: string;
  source_metadata: Record<string, unknown>;
  created_at: string;
}

export interface DecisionTimeline {
  decision: Decision;
  events: DecisionTimelineEvent[];
}

export interface DecisionRequest {
  account_id: string;
  thesis: string;
  alternatives: string[];
  evidence_references: EvidenceReference[];
  invalidation_conditions: InvalidationCondition[];
  horizon: { start: string; end: string };
  risk_budget: {
    amount: string;
    currency: string;
    measure: string;
    horizon: string;
  };
  intended_action: string;
  tags: string[];
  author: string;
  source_metadata: Record<string, unknown>;
}

export interface ReviewRequest {
  review: string;
  outcome: string;
  author: string;
  source_metadata?: Record<string, unknown>;
}

export interface AmendmentRequest {
  summary: string;
  changes: Record<string, unknown>;
  author: string;
  source_metadata?: Record<string, unknown>;
}

export class JournalApiError extends Error {
  readonly code?: string;
  readonly status?: number;

  constructor(message: string, status?: number, code?: string) {
    super(message);
    this.name = "JournalApiError";
    this.status = status;
    this.code = code;
  }
}

async function readResponseError(response: Response): Promise<JournalApiError> {
  try {
    const body = (await response.json()) as { code?: string; message?: string };
    return new JournalApiError(
      body.message || `Request failed with HTTP ${response.status}`,
      response.status,
      body.code,
    );
  } catch {
    return new JournalApiError(
      `Request failed with HTTP ${response.status}`,
      response.status,
    );
  }
}

async function writeJson<T>(path: string, body?: unknown) {
  let response: Response;
  try {
    response = await fetch(path, {
      method: "POST",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new JournalApiError(
      "AtlasRisk API’ye ulaşılamıyor. Yerel sunucunun çalıştığını kontrol edin.",
    );
  }
  if (!response.ok) throw await readResponseError(response);
  return (await response.json()) as T;
}

export function createDecision(request: DecisionRequest) {
  return writeJson<Decision>("/api/v1/decisions", request);
}

export function getDecision(decisionID: string) {
  return requestJson<Decision>(
    `/api/v1/decisions/${encodeURIComponent(decisionID)}`,
  );
}

export function finalizeDecision(decisionID: string) {
  return writeJson<Decision>(
    `/api/v1/decisions/${encodeURIComponent(decisionID)}/finalize`,
  );
}

export function getDecisionEvidence(decisionID: string) {
  return requestJson<DecisionEvidence>(
    `/api/v1/decisions/${encodeURIComponent(decisionID)}/evidence`,
  );
}

export function appendDecisionReview(
  decisionID: string,
  request: ReviewRequest,
) {
  return writeJson<DecisionReview>(
    `/api/v1/decisions/${encodeURIComponent(decisionID)}/reviews`,
    request,
  );
}

export function appendDecisionAmendment(
  decisionID: string,
  request: AmendmentRequest,
) {
  return writeJson<DecisionAmendment>(
    `/api/v1/decisions/${encodeURIComponent(decisionID)}/amendments`,
    request,
  );
}

export function getDecisionTimeline(decisionID: string) {
  return requestJson<DecisionTimeline>(
    `/api/v1/decisions/${encodeURIComponent(decisionID)}/timeline`,
  );
}
