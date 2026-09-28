import { afterEach, describe, expect, it, vi } from "vitest";

import { listPortfolios } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("portfolio API boundary", () => {
  it("rejects a malformed successful response before it reaches the shell", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify({ portfolios: [] }), { status: 200 }),
        ),
    );

    await expect(listPortfolios()).rejects.toMatchObject({
      kind: "error",
      message: "AtlasRisk API returned an invalid portfolio page.",
    });
  });

  it("accepts the generated minimum page shape", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify({ items: [] }), { status: 200 }),
        ),
    );

    await expect(listPortfolios()).resolves.toEqual({ items: [] });
  });

  it.each([
    [],
    { items: ["bad"] },
    {
      items: [
        {
          id: "portfolio-1",
          name: "Missing metadata",
          reporting_currency: "TRY",
          created_at: "2026-09-28T00:00:00Z",
          updated_at: "2026-09-28T00:00:00Z",
        },
      ],
    },
    {
      items: [
        {
          id: " ",
          name: "Blank id",
          reporting_currency: "TRY",
          metadata: {},
          created_at: "2026-09-28T00:00:00Z",
          updated_at: "2026-09-28T00:00:00Z",
        },
      ],
    },
    {
      items: [
        {
          id: "portfolio-1",
          name: " ",
          reporting_currency: "TRY",
          metadata: {},
          created_at: "2026-09-28T00:00:00Z",
          updated_at: "2026-09-28T00:00:00Z",
        },
      ],
    },
    {
      items: [
        {
          id: "portfolio-1",
          name: "Missing created_at",
          reporting_currency: "TRY",
          metadata: {},
          updated_at: "2026-09-28T00:00:00Z",
        },
      ],
    },
    {
      items: [
        {
          id: "portfolio-1",
          name: "Missing updated_at",
          reporting_currency: "TRY",
          metadata: {},
          created_at: "2026-09-28T00:00:00Z",
        },
      ],
    },
    {
      items: [
        {
          id: "portfolio-1",
          name: "Blank timestamps",
          reporting_currency: "TRY",
          metadata: {},
          created_at: " ",
          updated_at: "2026-09-28T00:00:00Z",
        },
      ],
    },
  ])("rejects a malformed portfolio item: %j", async (payload) => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(JSON.stringify(payload), { status: 200 }),
        ),
    );

    await expect(listPortfolios()).rejects.toMatchObject({ kind: "error" });
  });
});
