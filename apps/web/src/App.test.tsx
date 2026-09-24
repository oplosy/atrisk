import { render, screen } from "@testing-library/react";

import { App } from "./App";

describe("App", () => {
  it("renders the toolchain status", () => {
    render(<App />);
    expect(
      screen.getByRole("heading", { name: "AtlasRisk" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Toolchain skeleton ready.")).toBeInTheDocument();
  });
});
