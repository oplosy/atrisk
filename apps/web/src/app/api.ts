import type {
  ErrorEnvelope,
  PortfolioPage,
} from "../../../../contracts/generated/typescript/contracts";

export type ApiFailureKind =
  | "offline"
  | "unauthorized-proxy"
  | "server-error"
  | "error";

export class ApiRequestError extends Error {
  readonly kind: ApiFailureKind;
  readonly status?: number;

  constructor(kind: ApiFailureKind, message: string, status?: number) {
    super(message);
    this.name = "ApiRequestError";
    this.kind = kind;
    this.status = status;
  }
}

async function readError(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as ErrorEnvelope;
    return body.message || `Request failed with HTTP ${response.status}`;
  } catch {
    return `Request failed with HTTP ${response.status}`;
  }
}

export async function requestJson<T>(
  path: string,
  signal?: AbortSignal,
): Promise<T> {
  let response: Response;

  try {
    response = await fetch(path, {
      headers: { Accept: "application/json" },
      signal,
    });
  } catch {
    throw new ApiRequestError(
      "offline",
      "AtlasRisk API’ye ulaşılamıyor. Yerel sunucunun çalıştığını kontrol edin.",
    );
  }

  if (!response.ok) {
    const message = await readError(response);
    if (response.status === 401 || response.status === 403) {
      throw new ApiRequestError("unauthorized-proxy", message, response.status);
    }
    if (response.status >= 500) {
      throw new ApiRequestError("server-error", message, response.status);
    }
    throw new ApiRequestError("error", message, response.status);
  }

  return (await response.json()) as T;
}

function isPortfolioItem(value: unknown): boolean {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }

  const record = value as Record<string, unknown>;
  return (
    typeof record.id === "string" &&
    record.id.trim().length > 0 &&
    typeof record.name === "string" &&
    record.name.trim().length > 0 &&
    (record.reporting_currency === "TRY" ||
      record.reporting_currency === "USD") &&
    typeof record.metadata === "object" &&
    record.metadata !== null &&
    !Array.isArray(record.metadata) &&
    typeof record.created_at === "string" &&
    record.created_at.trim().length > 0 &&
    typeof record.updated_at === "string" &&
    record.updated_at.trim().length > 0
  );
}

export function listPortfolios(signal?: AbortSignal) {
  return requestJson<unknown>("/api/v1/portfolios", signal).then((payload) => {
    if (
      typeof payload !== "object" ||
      payload === null ||
      Array.isArray(payload) ||
      !("items" in payload) ||
      !Array.isArray(payload.items) ||
      !payload.items.every(isPortfolioItem)
    ) {
      throw new ApiRequestError(
        "error",
        "AtlasRisk API returned an invalid portfolio page.",
      );
    }
    return payload as PortfolioPage;
  });
}
