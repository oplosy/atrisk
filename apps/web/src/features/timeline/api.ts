import { ApiRequestError, requestJson } from "../../app/api";

export type Mode = "latest" | "source-as-of" | "system-as-of";
export type QualityLabel =
  | "missing"
  | "stale"
  | "partial"
  | "suspect"
  | "revised"
  | "fresh"
  | "unknown";

export interface Series {
  id: string;
  source_code: string;
  source_name?: string;
  name: string;
  unit: string;
  frequency: string;
  source_timezone?: string;
  capabilities: {
    latest: boolean;
    source_as_of: boolean;
    system_as_of: boolean;
    revisions: boolean;
  };
}

export interface Observation {
  id: string;
  series_id: string;
  observation_time: string;
  value?: string;
  value_text?: string;
  unit: string;
  frequency: string;
  data_source_code: string;
  clocks: {
    source_known_at?: string;
    system_known_at: string;
    knowledge_time_basis: string;
  };
  quality: Record<string, unknown>;
  raw_provenance_id: string;
  raw_object_sha256: string;
}

export interface Page<T> {
  items: T[];
  limit: number;
  has_more: boolean;
  next_cursor?: string;
}

export interface QualityResult {
  state: "valid" | "degraded" | "blocked";
  classification: QualityLabel;
  reasons: Array<{ code: string; message: string }>;
}

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function validSeries(value: unknown): value is Series {
  if (!record(value) || !record(value.capabilities)) return false;
  const caps = value.capabilities;
  return (
    [
      value.id,
      value.source_code,
      value.name,
      value.unit,
      value.frequency,
    ].every((part) => typeof part === "string" && part.length > 0) &&
    [caps.latest, caps.source_as_of, caps.system_as_of, caps.revisions].every(
      (part) => typeof part === "boolean",
    )
  );
}

function validObservation(value: unknown): value is Observation {
  if (!record(value) || !record(value.clocks) || !record(value.quality))
    return false;
  return (
    [
      value.id,
      value.series_id,
      value.observation_time,
      value.unit,
      value.frequency,
      value.data_source_code,
      value.raw_provenance_id,
      value.raw_object_sha256,
      value.clocks.system_known_at,
      value.clocks.knowledge_time_basis,
    ].every((part) => typeof part === "string" && part.length > 0) &&
    (value.value === undefined || typeof value.value === "string") &&
    (value.value_text === undefined || typeof value.value_text === "string")
  );
}

function parsePage<T>(
  value: unknown,
  validItem: (item: unknown) => item is T,
): Page<T> {
  if (
    !record(value) ||
    !Array.isArray(value.items) ||
    !value.items.every(validItem) ||
    typeof value.limit !== "number" ||
    typeof value.has_more !== "boolean" ||
    (value.has_more &&
      (typeof value.next_cursor !== "string" || !value.next_cursor))
  ) {
    throw new ApiRequestError(
      "error",
      "The timeline API returned an invalid page.",
    );
  }
  return value as unknown as Page<T>;
}

export async function listSeries(
  cursor: string,
  signal: AbortSignal,
): Promise<Page<Series>> {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor) query.set("cursor", cursor);
  return parsePage(
    await requestJson<unknown>(`/api/v1/series?${query}`, signal),
    validSeries,
  );
}

export async function listObservations(
  seriesID: string,
  mode: Mode,
  cutoff: string,
  cursor: string,
  signal: AbortSignal,
): Promise<Page<Observation>> {
  const query = new URLSearchParams({ mode, limit: "50" });
  if (mode !== "latest") query.set("as_of", new Date(cutoff).toISOString());
  if (cursor) query.set("cursor", cursor);
  return parsePage(
    await requestJson<unknown>(
      `/api/v1/series/${encodeURIComponent(seriesID)}/observations?${query}`,
      signal,
    ),
    validObservation,
  );
}

export async function listRevisions(
  seriesID: string,
  cursor: string,
  signal: AbortSignal,
): Promise<Page<Observation>> {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor) query.set("cursor", cursor);
  return parsePage(
    await requestJson<unknown>(
      `/api/v1/series/${encodeURIComponent(seriesID)}/revisions?${query}`,
      signal,
    ),
    validObservation,
  );
}

export async function evaluateQuality(
  seriesID: string,
  cutoff: string,
  signal: AbortSignal,
): Promise<QualityResult> {
  const asOf = cutoff ? new Date(cutoff) : new Date();
  const from = new Date(asOf.getTime() - 90 * 24 * 60 * 60 * 1000);
  const payload = await requestJson<unknown>(
    "/api/v1/quality/evaluate",
    signal,
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        from: from.toISOString(),
        to: asOf.toISOString(),
        as_of: asOf.toISOString(),
        inputs: [{ series_id: seriesID, required: true }],
      }),
    },
  );
  if (
    !record(payload) ||
    !Array.isArray(payload.items) ||
    !record(payload.items[0])
  ) {
    throw new ApiRequestError(
      "error",
      "The quality API returned an invalid result.",
    );
  }
  const item = payload.items[0];
  if (
    !["valid", "degraded", "blocked"].includes(String(item.state)) ||
    !["fresh", "stale", "missing", "partial", "suspect", "revised"].includes(
      String(item.classification),
    ) ||
    !Array.isArray(item.reasons) ||
    !item.reasons.every(
      (reason: unknown) =>
        record(reason) &&
        typeof reason.code === "string" &&
        typeof reason.message === "string",
    ) ||
    (item.state === "valid" &&
      ["missing", "stale", "partial", "suspect"].includes(
        String(item.classification),
      ))
  ) {
    throw new ApiRequestError(
      "error",
      "The quality API returned an invalid result.",
    );
  }
  return item as unknown as QualityResult;
}

export function qualityLabel(observation: Observation): QualityLabel {
  if (
    observation.value === undefined &&
    (observation.value_text === undefined ||
      observation.value_text === "." ||
      observation.value_text === "null")
  ) {
    return "missing";
  }
  for (const label of [
    "missing",
    "suspect",
    "partial",
    "stale",
    "revised",
  ] as const) {
    if (
      observation.quality[label] === true ||
      observation.quality.quality === label
    )
      return label;
  }
  return observation.quality.quality === "fresh" ? "fresh" : "unknown";
}
