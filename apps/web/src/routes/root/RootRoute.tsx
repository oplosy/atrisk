import { useCallback, useMemo, useState } from "react";

import { listPortfolios } from "../../app/api";
import { useApiResource, type ResourceState } from "../../app/useApiResource";
import { QualityBadge, type QualityState } from "../../components/QualityBadge";
import { StatusPanel } from "../../components/StatusPanel";
import { PortfolioRoute } from "../portfolio/PortfolioRoute";
import { TimelineRoute } from "../timeline/TimelineRoute";
import type { Mode } from "../../features/timeline/api";
import { RiskRoute } from "../risk/RiskRoute";
import { DecisionJournalRoute } from "../decisions/DecisionsRoute";

export type ShellRoute =
  | "/"
  | "/timeline"
  | "/portfolio"
  | "/risk"
  | "/journal"
  | "/decisions"
  | "/settings";

const pages: Record<ShellRoute, { title: string; description: string }> = {
  "/": {
    title: "Overview",
    description:
      "Portfolio state, data quality, and the evidence window for this workspace.",
  },
  "/timeline": {
    title: "Information timeline",
    description:
      "Observations from each source, read at an explicit point in time.",
  },
  "/portfolio": {
    title: "Portfolio",
    description:
      "Record position snapshots, run valuations, and reconcile against statements.",
  },
  "/risk": {
    title: "Risk workspace",
    description:
      "Run versioned stress scenarios and inspect results with their data-quality state.",
  },
  "/journal": {
    title: "Decision journal",
    description:
      "Record decisions against sealed evidence and append reviews without rewriting history.",
  },
  "/decisions": {
    title: "Decision journal",
    description:
      "Record decisions against sealed evidence and append reviews without rewriting history.",
  },
  "/settings": {
    title: "Workspace settings",
    description:
      "Connection and display preferences for this self-hosted installation.",
  },
};

type NavIconName =
  | "overview"
  | "timeline"
  | "portfolio"
  | "risk"
  | "journal"
  | "settings";

const navGroups: Array<{
  label?: string;
  items: Array<{ path: ShellRoute; label: string; icon: NavIconName }>;
}> = [
  { items: [{ path: "/", label: "Overview", icon: "overview" }] },
  {
    label: "Analyze",
    items: [
      { path: "/timeline", label: "Information timeline", icon: "timeline" },
      { path: "/portfolio", label: "Portfolio", icon: "portfolio" },
      { path: "/risk", label: "Risk workspace", icon: "risk" },
    ],
  },
  {
    label: "Record",
    items: [{ path: "/decisions", label: "Decision journal", icon: "journal" }],
  },
  {
    label: "Administration",
    items: [
      { path: "/settings", label: "Workspace settings", icon: "settings" },
    ],
  },
];

const iconPaths: Record<NavIconName, string> = {
  overview: "M3 3h4v4H3zM9 3h4v4H9zM3 9h4v4H3zM9 9h4v4H9z",
  timeline: "M2 12l3.5-4 3 2.5L14 4M2 14h12",
  portfolio: "M2.5 5h11v8h-11zM6 5V3h4v2M2.5 8.5h11",
  risk: "M8 2l5 2v4c0 3-2.2 5.2-5 6-2.8-.8-5-3-5-6V4z",
  journal: "M4 2h8v12H4zM6 5h4M6 8h4M6 11h2",
  settings: "M3 4h10M3 8h10M3 12h10M6 2.5v3M10 6.5v3M5 10.5v3",
};

function NavIcon({ name }: { name: NavIconName }) {
  return (
    <svg
      className="nav-link__icon"
      viewBox="0 0 16 16"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.25"
      strokeLinejoin="round"
      strokeLinecap="round"
    >
      <path d={iconPaths[name]} />
    </svg>
  );
}

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
  const [mode, setMode] = useState<Mode>("latest");
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
  const page = pages[route];
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
          <span className="brand__mark" aria-hidden="true" />
          <strong>AtlasRisk</strong>
        </a>
        <span className="topbar__env">Local</span>
        <div className="topbar__meta">
          {route === "/timeline" ? (
            <span className="connection">
              Timeline quality shown with series
            </span>
          ) : (
            <QualityBadge state={quality}>
              {quality === "valid"
                ? "Data path ready"
                : quality === "degraded"
                  ? "Review data quality"
                  : "Result blocked"}
            </QualityBadge>
          )}
          <span className="connection">
            <span className="connection__dot" aria-hidden="true" />
            Self-hosted
          </span>
        </div>
      </header>

      <div className="shell-layout">
        <aside className="sidebar" aria-label="Workspace sidebar">
          <nav className="sidebar__nav" aria-label="Primary navigation">
            {navGroups.map((group) => (
              <div className="nav-group" key={group.label ?? "root"}>
                {group.label ? (
                  <p className="nav-group__label">{group.label}</p>
                ) : null}
                {group.items.map((item) => (
                  <a
                    key={item.path}
                    className={
                      route === item.path
                        ? "nav-link nav-link--active"
                        : "nav-link"
                    }
                    aria-current={route === item.path ? "page" : undefined}
                    href={item.path}
                    onClick={(event) => {
                      event.preventDefault();
                      onNavigate(item.path);
                    }}
                  >
                    <NavIcon name={item.icon} />
                    {item.label}
                  </a>
                ))}
              </div>
            ))}
          </nav>
          <div className="sidebar__footer">
            <span>Version 1 · Decision support only</span>
            <p>
              Prices, shocks, and risk figures come from deterministic server
              paths.
            </p>
          </div>
        </aside>

        <main className="content" id="main-content">
          <header className="page-header">
            {route !== "/" ? (
              <nav className="breadcrumb" aria-label="Breadcrumb">
                <a
                  href="/"
                  onClick={(event) => {
                    event.preventDefault();
                    onNavigate("/");
                  }}
                >
                  AtlasRisk
                </a>
                <span aria-hidden="true">/</span>
                <span aria-current="page">{page.title}</span>
              </nav>
            ) : null}
            <h1>{page.title}</h1>
            <p className="page-header__description">{page.description}</p>
          </header>

          {(route === "/" || route === "/timeline") && (
            <section className="control-strip" aria-labelledby="as-of-title">
              <div className="control-strip__title">
                <h2 id="as-of-title">Evidence window</h2>
                <p>Explicit as-of. Publication time is never inferred.</p>
              </div>
              <label>
                Knowledge mode
                <select
                  value={mode}
                  onChange={(event) => setMode(event.target.value as Mode)}
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
          ) : route === "/timeline" ? (
            <TimelineRoute mode={mode} cutoff={cutoff} />
          ) : route === "/risk" ? (
            <RiskRoute />
          ) : route === "/decisions" || route === "/journal" ? (
            <DecisionJournalRoute />
          ) : route === "/" ? (
            <>
              <section className="metric-grid" aria-label="Workspace summary">
                <article className="metric">
                  <h2 className="metric__label">Portfolios</h2>
                  <strong className="metric__value">{portfolioCount}</strong>
                  <p className="metric__detail">
                    {portfolioCount === 0
                      ? "No portfolios returned yet."
                      : "Portfolio contexts available"}
                  </p>
                  <QualityBadge state={quality}>
                    {quality === "valid"
                      ? "Valid path"
                      : quality === "degraded"
                        ? "Degraded path"
                        : "Blocked path"}
                  </QualityBadge>
                </article>
                <article className="metric">
                  <h2 className="metric__label">Knowledge mode</h2>
                  <strong className="metric__value metric__value--text">
                    {mode === "latest" ? "Latest" : mode.replace("-", " ")}
                  </strong>
                  <p className="metric__detail">
                    {cutoff
                      ? `Cutoff ${cutoff.replace("T", " ")}`
                      : "Cutoff not selected"}
                  </p>
                </article>
                <article className="metric">
                  <h2 className="metric__label">Order execution</h2>
                  <strong className="metric__value metric__value--text">
                    Disabled
                  </strong>
                  <p className="metric__detail">
                    Decision support only. No orders, custody, or
                    recommendations.
                  </p>
                </article>
              </section>

              <div className="overview-grid">
                <section
                  className="panel"
                  aria-labelledby="portfolio-state-title"
                >
                  <div className="panel__header">
                    <h2 id="portfolio-state-title">Portfolio connection</h2>
                    <code className="panel__meta">GET /api/v1/portfolios</code>
                  </div>
                  <div className="panel__body">
                    {portfolio.state === "ready" ? (
                      <p className="panel__ok">
                        Connected. {portfolioCount} portfolio
                        {portfolioCount === 1 ? "" : "s"} returned.
                      </p>
                    ) : (
                      <StatusPanel
                        state={portfolio.state}
                        onRetry={portfolio.retry}
                      />
                    )}
                  </div>
                </section>

                <section className="panel" aria-labelledby="quick-links-title">
                  <div className="panel__header">
                    <h2 id="quick-links-title">Workspaces</h2>
                  </div>
                  <ul className="link-list">
                    {(
                      [
                        "/timeline",
                        "/portfolio",
                        "/risk",
                        "/decisions",
                      ] as const
                    ).map((path) => (
                      <li key={path}>
                        <a
                          href={path}
                          onClick={(event) => {
                            event.preventDefault();
                            onNavigate(path);
                          }}
                        >
                          <span>
                            <strong>{pages[path].title}</strong>
                            <small>{pages[path].description}</small>
                          </span>
                          <span aria-hidden="true">›</span>
                        </a>
                      </li>
                    ))}
                  </ul>
                </section>
              </div>
            </>
          ) : (
            <section className="panel" aria-labelledby="route-state-title">
              <div className="panel__header">
                <h2 id="route-state-title">Preferences</h2>
              </div>
              <div className="panel__body">
                <div className="empty-state">
                  <h3>No configurable settings yet</h3>
                  <p>
                    The API address and storage are set by the deployment.
                    Display preferences will appear here when available.
                  </p>
                </div>
              </div>
            </section>
          )}
        </main>
      </div>
    </div>
  );
}
