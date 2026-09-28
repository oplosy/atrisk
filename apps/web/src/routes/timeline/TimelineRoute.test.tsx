import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, vi } from "vitest";

import { TimelineRoute } from "./TimelineRoute";

const series = {
  id: "00000000-0000-0000-0000-000000000001",
  source_code: "FRED",
  source_name: "Federal Reserve",
  name: "Policy rate",
  unit: "percent",
  frequency: "daily",
  capabilities: {
    latest: true,
    source_as_of: true,
    system_as_of: true,
    revisions: true,
  },
};

function observation(
  id: string,
  value: string,
  systemKnown: string,
  observedAt = "2026-01-01T00:00:00Z",
) {
  return {
    id,
    series_id: series.id,
    observation_time: observedAt,
    value,
    unit: "percent",
    frequency: "daily",
    data_source_code: "FRED",
    clocks: {
      source_known_at: "2026-01-02T00:00:00Z",
      system_known_at: systemKnown,
      knowledge_time_basis: "source_published_at",
    },
    quality: { stale: true },
    raw_provenance_id: "00000000-0000-0000-0000-000000000010",
    raw_object_sha256: "a".repeat(64),
  };
}

function response(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

afterEach(() => vi.unstubAllGlobals());

it("explains empty and unsupported source-vintage readings without requesting unsupported data", async () => {
  const fetcher = vi.fn((url: string) => {
    if (url.startsWith("/api/v1/series?"))
      return Promise.resolve(
        response({
          items: [
            {
              ...series,
              capabilities: { ...series.capabilities, source_as_of: false },
            },
          ],
          limit: 50,
          has_more: false,
        }),
      );
    if (url.includes("/revisions?"))
      return Promise.resolve(
        response({ items: [], limit: 50, has_more: false }),
      );
    return Promise.resolve(response({ items: [], limit: 50, has_more: false }));
  });
  vi.stubGlobal("fetch", fetcher);
  render(<TimelineRoute mode="source-as-of" cutoff="2026-01-10T12:00" />);
  expect(
    await screen.findByText(/source as-of is unavailable for this series/i),
  ).toBeInTheDocument();
  expect(
    screen.getByText(/publication time is not inferred/i),
  ).toBeInTheDocument();
  expect(
    fetcher.mock.calls.some(([url]) => url.includes("/observations?")),
  ).toBe(false);
});

it("shows one-point value, quality, raw provenance and both clocks in revision comparison", async () => {
  const first = observation("revision-1", "12.5", "2026-01-03T00:00:00Z");
  const second = observation("revision-2", "12.75", "2026-01-05T00:00:00Z");
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      if (url.startsWith("/api/v1/series?"))
        return Promise.resolve(
          response({ items: [series], limit: 50, has_more: false }),
        );
      if (url.includes("/observations?"))
        return Promise.resolve(
          response({ items: [second], limit: 50, has_more: false }),
        );
      if (url.includes("/revisions?"))
        return Promise.resolve(
          response({ items: [second, first], limit: 50, has_more: false }),
        );
      if (url.includes("/quality/evaluate"))
        return Promise.resolve(
          response({
            items: [
              {
                state: "degraded",
                classification: "stale",
                reasons: [{ code: "STALE", message: "Older than policy" }],
              },
            ],
          }),
        );
      throw new Error(`unexpected URL ${url}`);
    }),
  );
  render(<TimelineRoute mode="system-as-of" cutoff="2026-01-10T12:00" />);
  expect(
    within(
      await screen.findByRole("table", {
        name: /observations · system-as-of/i,
      }),
    ).getByText("12.75 percent"),
  ).toBeInTheDocument();
  expect(screen.getByText("Older than policy")).toBeInTheDocument();
  expect(screen.getByText("source_published_at")).toBeInTheDocument();
  expect(
    screen.getByRole("table", { name: /loaded revision comparisons/i }),
  ).toHaveTextContent("12.5 percent");
  expect(screen.getByText(/knowledge cutoff/i)).toBeInTheDocument();
});

it("loads paginated dense observations and exposes request failures", async () => {
  const firstPage = Array.from({ length: 50 }, (_, index) =>
    observation(
      `revision-${index}`,
      String(index),
      "2026-01-03T00:00:00Z",
      new Date(Date.UTC(2026, 0, index + 1)).toISOString(),
    ),
  );
  const fetcher = vi.fn((url: string) => {
    if (url.startsWith("/api/v1/series?") && url.includes("cursor=series-next"))
      return Promise.resolve(
        response({
          items: [{ ...series, id: "00000000-0000-0000-0000-000000000002" }],
          limit: 50,
          has_more: false,
        }),
      );
    if (url.startsWith("/api/v1/series?"))
      return Promise.resolve(
        response({
          items: [series],
          limit: 50,
          has_more: true,
          next_cursor: "series-next",
        }),
      );
    if (url.includes("/observations?") && url.includes("cursor=next"))
      return Promise.resolve(
        response({
          items: [
            observation(
              "revision-50",
              "50",
              "2026-01-03T00:00:00Z",
              new Date(Date.UTC(2026, 0, 51)).toISOString(),
            ),
          ],
          limit: 50,
          has_more: false,
        }),
      );
    if (url.includes("/observations?"))
      return Promise.resolve(
        response({
          items: firstPage,
          limit: 50,
          has_more: true,
          next_cursor: "next",
        }),
      );
    if (url.includes("/revisions?") && url.includes("cursor=revision-next"))
      return Promise.resolve(
        response({
          items: [observation("revision-next", "1", "2026-01-04T00:00:00Z")],
          limit: 50,
          has_more: false,
        }),
      );
    if (url.includes("/revisions?"))
      return Promise.resolve(
        response({
          items: [],
          limit: 50,
          has_more: true,
          next_cursor: "revision-next",
        }),
      );
    if (url.includes("/quality/evaluate"))
      return Promise.resolve(
        response({
          items: [{ state: "blocked", classification: "missing", reasons: [] }],
        }),
      );
    throw new Error(`unexpected URL ${url}`);
  });
  vi.stubGlobal("fetch", fetcher);
  render(<TimelineRoute mode="latest" cutoff="" />);
  const loadSeries = await screen.findByRole("button", {
    name: /load more series/i,
  });
  const loadObservations = await screen.findByRole("button", {
    name: /load more observations/i,
  });
  const loadRevisions = await screen.findByRole("button", {
    name: /load more revisions/i,
  });
  fireEvent.click(loadSeries);
  fireEvent.click(loadSeries);
  fireEvent.click(loadObservations);
  fireEvent.click(loadObservations);
  fireEvent.click(loadRevisions);
  fireEvent.click(loadRevisions);
  await waitFor(() =>
    expect(
      screen
        .getByRole("table", { name: /observations · latest/i })
        .querySelectorAll("tbody tr"),
    ).toHaveLength(51),
  );
  await waitFor(() => {
    expect(
      fetcher.mock.calls.filter(([url]) => url.includes("cursor=series-next")),
    ).toHaveLength(1);
    expect(
      fetcher.mock.calls.filter(([url]) =>
        url.includes("cursor=revision-next"),
      ),
    ).toHaveLength(1);
  });
  const points = screen
    .getByRole("img", { name: /chart of policy rate/i })
    .querySelector("polyline")
    ?.getAttribute("points")
    ?.split(" ")
    .map((point) => point.split(",").map(Number));
  expect(points).toHaveLength(51);
  expect(points?.[0]?.[0]).toBeLessThan(points?.at(-1)?.[0] ?? 0);
  expect(points?.[0]?.[1]).toBeGreaterThan(points?.at(-1)?.[1] ?? 0);
  expect(
    fetcher.mock.calls.filter(([url]) => url.includes("/observations?")),
  ).toHaveLength(2);
});

it("ignores a delayed observation page after the point-in-time query changes", async () => {
  let resolveStalePage: ((value: Response) => void) | undefined;
  const stalePage = new Promise<Response>((resolve) => {
    resolveStalePage = resolve;
  });
  const fetcher = vi.fn((url: string) => {
    if (url.startsWith("/api/v1/series?"))
      return Promise.resolve(
        response({ items: [series], limit: 50, has_more: false }),
      );
    if (url.includes("/observations?") && url.includes("cursor=old-next"))
      return stalePage;
    if (url.includes("/observations?") && url.includes("cursor=new-next"))
      return Promise.resolve(
        response({
          items: [observation("new-second", "3", "2026-01-05T00:00:00Z")],
          limit: 50,
          has_more: false,
        }),
      );
    if (url.includes("/observations?") && url.includes("mode=system-as-of"))
      return Promise.resolve(
        response({
          items: [observation("new-first", "2", "2026-01-04T00:00:00Z")],
          limit: 50,
          has_more: true,
          next_cursor: "new-next",
        }),
      );
    if (url.includes("/observations?"))
      return Promise.resolve(
        response({
          items: [observation("old-first", "1", "2026-01-03T00:00:00Z")],
          limit: 50,
          has_more: true,
          next_cursor: "old-next",
        }),
      );
    if (url.includes("/revisions?"))
      return Promise.resolve(
        response({ items: [], limit: 50, has_more: false }),
      );
    if (url.includes("/quality/evaluate"))
      return Promise.resolve(
        response({
          items: [{ state: "valid", classification: "fresh", reasons: [] }],
        }),
      );
    throw new Error(`unexpected URL ${url}`);
  });
  vi.stubGlobal("fetch", fetcher);
  const view = render(<TimelineRoute mode="latest" cutoff="" />);
  expect(await screen.findByText("1 percent")).toBeInTheDocument();
  fireEvent.click(
    screen.getByRole("button", { name: /load more observations/i }),
  );
  await waitFor(() =>
    expect(
      fetcher.mock.calls.some(([url]) => url.includes("cursor=old-next")),
    ).toBe(true),
  );

  view.rerender(
    <TimelineRoute mode="system-as-of" cutoff="2026-01-10T12:00" />,
  );
  expect(await screen.findByText("2 percent")).toBeInTheDocument();
  resolveStalePage?.(
    response({
      items: [observation("stale", "99", "2026-01-06T00:00:00Z")],
      limit: 50,
      has_more: true,
      next_cursor: "stale-next",
    }),
  );

  fireEvent.click(
    screen.getByRole("button", { name: /load more observations/i }),
  );
  expect(await screen.findByText("3 percent")).toBeInTheDocument();
  expect(screen.queryByText("99 percent")).not.toBeInTheDocument();
  expect(
    fetcher.mock.calls.filter(([url]) => url.includes("cursor=new-next")),
  ).toHaveLength(1);
  expect(
    fetcher.mock.calls.some(([url]) => url.includes("cursor=stale-next")),
  ).toBe(false);
});

it("reports an API error with retry action", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      response(
        {
          code: "INTERNAL_ERROR",
          message: "Unavailable",
          request_id: "test",
        },
        503,
      ),
    ),
  );
  render(<TimelineRoute mode="latest" cutoff="" />);
  expect(await screen.findByText(/Unavailable/)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: /retry/i })).toBeInTheDocument();
});
