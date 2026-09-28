import type {
  AccountPage,
  CreateSnapshotRequest,
  InstrumentPage,
  PortfolioPage,
  Snapshot,
  SnapshotPage,
  ValuationRequest,
  ValuationRun,
} from "../../../../../contracts/generated/typescript/contracts";

export interface ImportPreview {
  token?: string;
  content_sha256: string;
  valid: boolean;
  row_count: number;
  diagnostics: { row?: number; code: string; message: string }[];
  expires_at?: string;
}

export interface ImportResult {
  content_sha256: string;
  snapshot_id: string;
}

export interface ReconciliationCheckpoint {
  id: string;
  account_id: string;
  valuation_id: string;
  currency: "TRY" | "USD";
  cutoff: string;
  external_nav: string;
  valuation_nav: string;
  absolute_difference: string;
  relative_difference?: string | null;
  effective_tolerance: string;
  tolerance_version: number;
  state: "reconciled" | "unreconciled";
  line_check_state: string;
}

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(path, {
      ...init,
      headers: { Accept: "application/json", ...init?.headers },
    });
  } catch {
    throw new Error("API unavailable. Check the local server and retry.");
  }
  if (!response.ok) {
    let message = `HTTP ${response.status}`;
    try {
      const body = (await response.json()) as { message?: string };
      message = body.message || message;
    } catch {
      // An empty or non-JSON error response still has a useful HTTP status.
    }
    throw new Error(message);
  }
  return (await response.json()) as T;
}

export function jsonPost<T>(path: string, value: unknown): Promise<T> {
  return api<T>(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(value),
  });
}

export const portfolioApi = {
  portfolios: () => api<PortfolioPage>("/api/v1/portfolios"),
  accounts: (portfolioId: string) =>
    api<AccountPage>(
      `/api/v1/portfolios/${encodeURIComponent(portfolioId)}/accounts`,
    ),
  instruments: () => api<InstrumentPage>("/api/v1/instruments"),
  snapshots: (portfolioId: string) =>
    api<SnapshotPage>(
      `/api/v1/portfolios/${encodeURIComponent(portfolioId)}/snapshots`,
    ),
  createSnapshot: (portfolioId: string, value: CreateSnapshotRequest) =>
    jsonPost<Snapshot>(
      `/api/v1/portfolios/${encodeURIComponent(portfolioId)}/snapshots`,
      value,
    ),
  createValuation: (value: ValuationRequest) =>
    jsonPost<ValuationRun>("/api/v1/valuations", value),
  reconcile: (valuationId: string, value: unknown) =>
    jsonPost<ReconciliationCheckpoint>(
      `/api/v1/valuations/${encodeURIComponent(valuationId)}/reconciliations`,
      value,
    ),
};

export async function sha256(file: File): Promise<string> {
  const bytes = await file.arrayBuffer();
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

export function importBody(
  file: File,
  portfolioId: string,
  capturedAt: string,
): FormData {
  const body = new FormData();
  body.append("file", file);
  body.append("schema_version", "1.0");
  body.append("target_id", portfolioId);
  body.append("captured_at", capturedAt);
  return body;
}
