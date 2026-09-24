import { describe, expect, it } from "vitest";

import { formatDiagnostics } from "./diagnostics.mjs";

describe("web diagnostics", () => {
  it("reports runtime and dependency versions", () => {
    const output = formatDiagnostics({
      node: "v24.16.0",
      npm: "12.0.1",
      react: "19.1.1",
      reactDom: "19.1.1",
      vite: "7.3.6",
      vitest: "5.0.1",
      typescript: "5.9.2",
    });

    expect(output).toContain("runtime node v24.16.0");
    expect(output).toContain("dependency react 19.1.1");
    expect(output).toContain("tool vitest 5.0.1");
  });
});
