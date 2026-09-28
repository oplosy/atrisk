import { useCallback, useMemo, useState } from "react";

import { listPortfolios } from "../../app/api";
import { useApiResource, type ResourceState } from "../../app/useApiResource";
import { QualityBadge, type QualityState } from "../../components/QualityBadge";
import { StatusPanel } from "../../components/StatusPanel";
import { PortfolioRoute } from "../portfolio/PortfolioRoute";

export type ShellRoute =
  | "/"
  | "/timeline"
  | "/portfolio"
  | "/risk"
  | "/journal"
  | "/settings";

const routes: Record<
  Exclude<ShellRoute, "/">,
  { eyebrow: string; title: string; body: string }
> = {
  "/timeline": {
    eyebrow: "Information timeline",
    title: "Read the market record in context.",
    body: "Source-as-of and system-as-of views will keep each observation tied to the evidence available at that point.",
  },
  "/portfolio": {
    eyebrow: "Portfolio",
    title: "A clean place for immutable snapshots.",
    body: "Positions, valuation paths, and reconciliation checkpoints belong here. No client-side totals are inferred.",
  },
  "/risk": {
    eyebrow: "Risk workspace",
    title: "Risk results with their quality attached.",
    body: "Every result will expose its snapshot, engine version, and data-quality state before it is interpreted.",
  },
  "/journal": {
    eyebrow: "Decision journal",
    title: "Keep decisions beside their evidence.",
    body: "Journal entries will link to immutable snapshots without turning AtlasRisk into an execution or recommendation system.",
  },
  "/settings": {
    eyebrow: "Workspace settings",
    title: "Local-first controls.",
    body: "Connection and display preferences stay explicit so a self-hosted installation remains inspectable.",
  },
};

function qualityForResource(state: ResourceState): QualityState {
  if (state === "ready") return "valid";
  if (state === "stale" || state === "offline") return "degraded";
  return "blocked";
}

interface RootRouteProps {
  route: ShellRoute;
  onNavigate: (route: ShellRoute) => void;
}

export function RootRoute({ route, onNavigate }: RootRouteProps) {
  const [mode, setMode] = useState("latest");
  const [cutoff, setCutoff] = useState("");
  const loadPortfolios = useCallback(
    (signal: AbortSignal) => listPortfolios(signal),
    [],
  );
  const isEmpty = useCallback(
    (data: { items: unknown[] }) => data.items.length === 0,
    [],
  );
  const portfolio = useApiResource(loadPortfolios, isEmpty);
  const page = route === "/" ? undefined : routes[route];
  const quality = qualityForResource(portfolio.state);
  const portfolioCount = portfolio.data?.items.length ?? 0;
  const asOfLabel = useMemo(() => {
    if (mode === "latest") return "Latest available";
    return cutoff ? `${mode} · ${cutoff}` : `${mode} · Choose a cutoff`;
  }, [cutoff, mode]);

  return (
    <div className="app-shell">
      <header className="topbar">
        <a
          className="brand"
          href="/"
          onClick={(event) => {
            event.preventDefault();
            onNavigate("/");
          }}
        >
          <span className="brand__mark" aria-hidden="true">
            AR
          </span>
          <span>
            <strong>AtlasRisk</strong>
            <small>decision support / local-first</small>
          </span>
        </a>
        <div className="topbar__meta">
          <QualityBadge state={quality}>
            {quality === "valid"
              ? "Data path ready"
              : quality === "degraded"
                ? "Review data quality"
                : "Result blocked"}
          </QualityBadge>
          <span className="connection">
            <span className="connection__dot" aria-hidden="true" /> self-hosted
          </span>
        </div>
      </header>

      <div className="shell-layout">
        <aside className="sidebar" aria-label="Workspace sidebar">
          <p className="sidebar__label">Workspace</p>
          <nav aria-label="Primary navigation">
            <a
              className={
                route === "/" ? "nav-link nav-link--active" : "nav-link"
              }
              aria-current={route === "/" ? "page" : undefined}
              href="/"
              onClick={(event) => {
                event.preventDefault();
                onNavigate("/");
              }}
            >
              <span aria-hidden="true">⌂</span> Overview
            </a>
            {(Object.keys(routes) as Array<Exclude<ShellRoute, "/">>).map(
              (path) => (
                <a
                  key={path}
                  className={
                    route === path ? "nav-link nav-link--active" : "nav-link"
                  }
                  aria-current={route === path ? "page" : undefined}
                  href={path}
                  onClick={(event) => {
                    event.preventDefault();
                    onNavigate(path);
                  }}
                >
                  <span aria-hidden="true">
                    {path === "/timeline"
                      ? "◌"
                      : path === "/portfolio"
                        ? "▦"
                        : path === "/risk"
                          ? "⌁"
                          : path === "/journal"
                            ? "✎"
                            : "⚙"}
                  </span>
                  {routes[path].eyebrow}
                </a>
              ),
            )}
          </nav>
          <div className="sidebar__footer">
            <span className="sidebar__version">V1 · no execution</span>
            <p>
              Prices, shocks, and risk numbers always come from deterministic
              server paths.
            </p>
          </div>
        </aside>

        <main className="content" id="main-content">
          <div className="content__intro">
            <div>
              <p className="eyebrow">{page?.eyebrow ?? "Decision workspace"}</p>
              <h1>{page?.title ?? "Know what was knowable."}</h1>
              <p className="lede">
                {page?.body ??
                  "AtlasRisk keeps portfolio state, market evidence, and risk quality in one inspectable timeline."}
              </p>
            </div>
            <div className="intro__stamp">
              <span>Clock semantics</span>
              <strong>Explicit as-of</strong>
              <small>No inferred publication time</small>
            </div>
          </div>

          {route !== "/portfolio" && (
            <section className="control-strip" aria-labelledby="as-of-title">
              <div className="control-strip__title">
                <span className="step-number">01</span>
                <div>
                  <p className="eyebrow" id="as-of-title">
                    Evidence window
                  </p>
                  <strong>Choose the clock before reading a result.</strong>
                </div>
              </div>
              <label>
                Knowledge mode
                <select
                  value={mode}
                  onChange={(event) => setMode(event.target.value)}
                >
                  <option value="latest">Latest</option>
                  <option value="source-as-of">Source as-of</option>
                  <option value="system-as-of">System as-of</option>
                </select>
              </label>
              <label>
                Cutoff
                <input
                  type="datetime-local"
                  value={cutoff}
                  onChange={(event) => setCutoff(event.target.value)}
                />
              </label>
              <output className="control-strip__output" aria-live="polite">
                <span>Reading</span>
                <strong>{asOfLabel}</strong>
              </output>
            </section>
          )}

          {route === "/portfolio" ? (
            <PortfolioRoute />
          ) : route === "/" ? (
            <>
              <section className="signal-grid" aria-label="Workspace signals">
                <article className="signal-card signal-card--accent">
                  <span className="signal-card__index">A / context</span>
                  <h2>Portfolio surface</h2>
                  <strong className="signal-card__value">
                    {portfolioCount}
                  </strong>
                  <p>
                    {portfolioCount === 0
                      ? "No portfolios returned yet."
                      : "portfolio contexts available"}
                  </p>
                  <QualityBadge state={quality}>
                    {quality === "valid"
                      ? "Valid path"
                      : quality === "degraded"
                        ? "Degraded path"
                        : "Blocked path"}
                  </QualityBadge>
                </article>
                <article className="signal-card">
                  <span className="signal-card__index">B / provenance</span>
                  <h2>As-of reading</h2>
                  <strong className="signal-card__value signal-card__value--text">
                    {mode === "latest" ? "Latest" : mode.replace("-", " ")}
                  </strong>
                  <p>Cutoff is explicit and preserved with the view.</p>
                  <span className="signal-card__note">
                    {cutoff
                      ? cutoff.replace("T", " · ")
                      : "Cutoff not selected"}
                  </span>
                </article>
                <article className="signal-card">
                  <span className="signal-card__index">C / boundary</span>
                  <h2>Decision support</h2>
                  <strong className="signal-card__value signal-card__value--text">
                    No trades
                  </strong>
                  <p>
                    Execution, custody, and recommendations stay outside V1.
                  </p>
                  <span className="signal-card__note">
                    Deterministic inputs only
                  </span>
                </article>
              </section>
              <section
                className="state-section"
                aria-labelledby="portfolio-state-title"
              >
                <div className="section-heading">
                  <div>
                    <p className="eyebrow">API state</p>
                    <h2 id="portfolio-state-title">Portfolio connection</h2>
                  </div>
                  <span className="section-heading__meta">
                    GET /api/v1/portfolios
                  </span>
                </div>
                <StatusPanel
                  state={portfolio.state}
                  onRetry={portfolio.retry}
                />
              </section>
            </>
          ) : (
            <section
              className="state-section state-section--route"
              aria-labelledby="route-state-title"
            >
              <div className="section-heading">
                <div>
                  <p className="eyebrow">Route shell</p>
                  <h2 id="route-state-title">
                    This surface is ready for its domain task
                  </h2>
                </div>
                <QualityBadge state={quality}>
                  {quality === "valid"
                    ? "Data path ready"
                    : quality === "degraded"
                      ? "Review data quality"
                      : "Result blocked"}
                </QualityBadge>
              </div>
              <div className="route-note">
                <span className="route-note__mark" aria-hidden="true">
                  ↗
                </span>
                <p>
                  Navigation, evidence controls, focus treatment, and quality
                  language are shared. The next task can add domain data here
                  without replacing the shell.
                </p>
              </div>
            </section>
          )}
        </main>
      </div>
    </div>
  );
}
