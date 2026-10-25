import { fireEvent, render, screen, within } from "@testing-library/react";
import { vi } from "vitest";

import { App } from "./App";

const primaryNav = () =>
  within(screen.getByRole("navigation", { name: /primary navigation/i }));

describe("App", () => {
  it("renders an accessible shell with explicit evidence controls", () => {
    render(<App />);
    expect(
      screen.getByRole("heading", { level: 1, name: "Overview" }),
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
    fireEvent.click(
      primaryNav().getByRole("link", { name: /risk workspace/i }),
    );
    expect(
      screen.getByRole("heading", { level: 1, name: "Risk workspace" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Inspect a risk run" }),
    ).toBeInTheDocument();
  });

  it("opens the decision journal at its dedicated route", () => {
    render(<App />);
    const decisionLink = primaryNav()
      .getAllByRole("link", { name: /decision journal/i })
      .find((link) => link.getAttribute("href") === "/decisions");
    expect(decisionLink).toBeDefined();
    fireEvent.click(decisionLink!);
    expect(
      screen.getByRole("heading", { level: 1, name: "Decision journal" }),
    ).toBeInTheDocument();
  });

  it("keeps /timeline in navigation history and restores the previous route", async () => {
    window.history.replaceState({}, "", "/");
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string) => {
        if (url.startsWith("/api/v1/series?"))
          return Promise.resolve(
            new Response(
              JSON.stringify({ items: [], limit: 50, has_more: false }),
              { status: 200 },
            ),
          );
        return Promise.resolve(
          new Response(JSON.stringify({ items: [] }), { status: 200 }),
        );
      }),
    );
    render(<App />);
    fireEvent.click(
      primaryNav().getByRole("link", { name: /information timeline/i }),
    );
    expect(window.location.pathname).toBe("/timeline");
    expect(
      await screen.findByRole("heading", { name: "Series" }),
    ).toBeInTheDocument();
    window.history.pushState({}, "", "/");
    fireEvent.popState(window);
    expect(
      screen.getByRole("heading", { level: 1, name: "Overview" }),
    ).toBeInTheDocument();
    vi.unstubAllGlobals();
  });
});
