import type { ResourceState } from "../app/useApiResource";

interface StatusPanelProps {
  state: ResourceState;
  onRetry?: () => void;
}

const copy: Record<
  Exclude<ResourceState, "ready">,
  { title: string; body: string }
> = {
  loading: {
    title: "Loading portfolio context",
    body: "AtlasRisk is reading the latest available snapshot.",
  },
  empty: {
    title: "No portfolio yet",
    body: "Create an account and immutable snapshot to start a decision-support workflow.",
  },
  stale: {
    title: "Showing stale data",
    body: "The last known snapshot is visible, but a newer source has not been confirmed.",
  },
  offline: {
    title: "Offline",
    body: "AtlasRisk API’ye ulaşılamıyor. Yerel sunucuyu başlatıp tekrar deneyin.",
  },
  "unauthorized-proxy": {
    title: "Access blocked by proxy",
    body: "The access proxy did not authorize this request. Check the proxy session before retrying.",
  },
  "server-error": {
    title: "Server error",
    body: "The API returned a server error. No risk result is presented as healthy.",
  },
  error: {
    title: "Could not load data",
    body: "The response was not understood. Retry the request or inspect the API logs.",
  },
};

export function StatusPanel({ state, onRetry }: StatusPanelProps) {
  if (state === "ready") return null;
  const stateCopy = copy[state];
  return (
    <section
      className={`status-panel status-panel--${state}`}
      aria-live="polite"
      role={state === "loading" ? "status" : "alert"}
    >
      <div className="status-panel__mark" aria-hidden="true">
        {state === "loading" ? "…" : state === "empty" ? "○" : "!"}
      </div>
      <div>
        <h3>{stateCopy.title}</h3>
        <p>{stateCopy.body}</p>
        {state !== "empty" && onRetry ? (
          <button
            className="button button--quiet"
            type="button"
            onClick={onRetry}
          >
            Retry request
          </button>
        ) : null}
      </div>
    </section>
  );
}
