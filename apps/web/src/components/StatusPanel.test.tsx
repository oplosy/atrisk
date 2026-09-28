import { render, screen } from "@testing-library/react";

import { QualityBadge } from "./QualityBadge";
import { StatusPanel } from "./StatusPanel";

describe("quality and API states", () => {
  it.each([
    ["loading", "Loading portfolio context"],
    ["empty", "No portfolio yet"],
    ["stale", "Showing stale data"],
    ["offline", "Offline"],
    ["unauthorized-proxy", "Access blocked by proxy"],
    ["server-error", "Server error"],
  ] as const)("renders the %s state as visible text", (state, title) => {
    render(<StatusPanel state={state} />);
    expect(
      screen.getByRole(state === "loading" ? "status" : "alert"),
    ).toHaveTextContent(title);
  });

  it("does not rely on color alone for quality", () => {
    render(
      <QualityBadge state="blocked">
        Blocked — required input missing
      </QualityBadge>,
    );
    expect(
      screen.getByText("Blocked — required input missing"),
    ).toBeInTheDocument();
    expect(screen.getByText("×")).toBeInTheDocument();
  });
});
