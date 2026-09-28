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
    { items: ["bad"] },
    { items: [{ id: "portfolio-1", name: "Missing currency" }] },
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
