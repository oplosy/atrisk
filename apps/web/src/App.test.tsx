import { fireEvent, render, screen } from "@testing-library/react";

import { App } from "./App";

describe("App", () => {
  it("renders an accessible shell with explicit evidence controls", () => {
    render(<App />);
    expect(
      screen.getByRole("heading", { name: /see what was knowable/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("navigation", { name: /primary navigation/i }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Knowledge mode")).toHaveValue("latest");
    expect(screen.getByLabelText("Cutoff")).toBeInTheDocument();
    expect(
      screen.getByText(/result blocked|review data quality|data path ready/i),
    ).toBeInTheDocument();
  });

  it("keeps route context visible when navigating by keyboard-compatible links", () => {
    render(<App />);
    fireEvent.click(screen.getByRole("link", { name: /risk workspace/i }));
    expect(
      screen.getByRole("heading", {
        name: /risk results with their quality attached/i,
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("This surface is ready for its domain task"),
    ).toBeInTheDocument();
  });
});
