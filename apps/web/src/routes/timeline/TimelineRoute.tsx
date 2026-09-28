import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";

import { ApiRequestError } from "../../app/api";
import { QualityBadge } from "../../components/QualityBadge";
import {
  evaluateQuality,
  listObservations,
  listRevisions,
  listSeries,
  qualityLabel,
  type Mode,
  type Observation,
  type QualityResult,
  type Series,
} from "../../features/timeline/api";

interface Props {
  mode: Mode;
  cutoff: string;
}
type LoadState =
  | "loading"
  | "ready"
  | "error"
  | "choose-cutoff"
  | "unsupported";

function dateLabel(value?: string): string {
  if (!value) return "Not supplied by source";
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleString(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
        timeZone: "UTC",
      }) + " UTC";
}

function valueLabel(point: Observation): string {
  return point.value ?? point.value_text ?? "Missing";
}

function errorLabel(error: unknown): string {
  return error instanceof Error
    ? error.message
    : "The timeline could not be loaded.";
}

function chartPoints(items: Observation[]): string {
  // Number conversion is only for SVG geometry. Displayed values stay exact strings.
  const numeric = items
    .map((item) => Number(item.value))
    .filter(Number.isFinite);
  if (numeric.length === 0) return "";
  const low = Math.min(...numeric);
  const span = Math.max(...numeric) - low || 1;
  return numeric
    .map(
      (value, index) =>
        `${20 + (index * 560) / Math.max(numeric.length - 1, 1)},${130 - ((value - low) / span) * 105}`,
    )
    .join(" ");
}

export function TimelineRoute({ mode, cutoff }: Props) {
  const [series, setSeries] = useState<Series[]>([]);
  const [seriesCursor, setSeriesCursor] = useState("");
  const [seriesMore, setSeriesMore] = useState(false);
  const [seriesState, setSeriesState] = useState<LoadState>("loading");
  const [seriesError, setSeriesError] = useState("");
  const [seriesRetry, setSeriesRetry] = useState(0);
  const [loadingNextSeries, setLoadingNextSeries] = useState(false);
  const loadingNextSeriesRef = useRef(false);
  const [search, setSearch] = useState("");
  const [selectedID, setSelectedID] = useState("");
  const [observations, setObservations] = useState<Observation[]>([]);
  const [observationCursor, setObservationCursor] = useState("");
  const [observationMore, setObservationMore] = useState(false);
  const [observationState, setObservationState] =
    useState<LoadState>("loading");
  const [observationError, setObservationError] = useState("");
  const [observationRetry, setObservationRetry] = useState(0);
  const [loadingNextObservations, setLoadingNextObservations] = useState(false);
  const loadingNextObservationsRef = useRef(false);
  const [revisions, setRevisions] = useState<Observation[]>([]);
  const [revisionCursor, setRevisionCursor] = useState("");
  const [revisionMore, setRevisionMore] = useState(false);
  const [revisionState, setRevisionState] = useState<LoadState>("loading");
  const [loadingNextRevisions, setLoadingNextRevisions] = useState(false);
  const loadingNextRevisionsRef = useRef(false);
  const [revisionError, setRevisionError] = useState("");
  const [quality, setQuality] = useState<QualityResult>();
  const [qualityError, setQualityError] = useState("");
  const [inspectedID, setInspectedID] = useState("");
  const seriesContextKey = String(seriesRetry);
  const seriesContextRef = useRef({ key: seriesContextKey, generation: 0 });
  const observationContextKey = JSON.stringify([
    selectedID,
    seriesRetry,
    mode,
    cutoff,
    observationRetry,
  ]);
  const observationContextRef = useRef({
    key: observationContextKey,
    generation: 0,
  });
  const revisionContextKey = JSON.stringify([selectedID, seriesRetry]);
  const revisionContextRef = useRef({ key: revisionContextKey, generation: 0 });

  useLayoutEffect(() => {
    if (seriesContextRef.current.key !== seriesContextKey) {
      seriesContextRef.current = {
        key: seriesContextKey,
        generation: seriesContextRef.current.generation + 1,
      };
    }
    if (observationContextRef.current.key !== observationContextKey) {
      observationContextRef.current = {
        key: observationContextKey,
        generation: observationContextRef.current.generation + 1,
      };
    }
    if (revisionContextRef.current.key !== revisionContextKey) {
      revisionContextRef.current = {
        key: revisionContextKey,
        generation: revisionContextRef.current.generation + 1,
      };
    }
  }, [seriesContextKey, observationContextKey, revisionContextKey]);

  useEffect(() => {
    loadingNextSeriesRef.current = false;
    setLoadingNextSeries(false);
    const controller = new AbortController();
    setSeriesState("loading");
    void listSeries("", controller.signal)
      .then((page) => {
        setSeries(page.items);
        setSeriesCursor(page.next_cursor ?? "");
        setSeriesMore(page.has_more);
        setSelectedID((previous) => previous || page.items[0]?.id || "");
        setSeriesState("ready");
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setSeriesError(errorLabel(error));
        setSeriesState("error");
      });
    return () => controller.abort();
  }, [seriesRetry]);

  const selected = series.find((item) => item.id === selectedID);
  useEffect(() => {
    loadingNextObservationsRef.current = false;
    setLoadingNextObservations(false);
    if (!selected) return;
    if (mode !== "latest" && !cutoff) {
      setObservations([]);
      setObservationState("choose-cutoff");
      return;
    }
    if (mode === "source-as-of" && !selected.capabilities.source_as_of) {
      setObservations([]);
      setObservationState("unsupported");
      return;
    }
    const controller = new AbortController();
    setObservationState("loading");
    setQuality(undefined);
    setQualityError("");
    void listObservations(selected.id, mode, cutoff, "", controller.signal)
      .then((page) => {
        setObservations(page.items);
        setObservationCursor(page.next_cursor ?? "");
        setObservationMore(page.has_more);
        setInspectedID(page.items[0]?.id ?? "");
        setObservationState("ready");
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setObservationError(errorLabel(error));
        setObservationState(
          error instanceof ApiRequestError && error.status === 409
            ? "unsupported"
            : "error",
        );
      });
    void evaluateQuality(
      selected.id,
      mode === "latest" ? "" : cutoff,
      controller.signal,
    )
      .then(setQuality)
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setQualityError(errorLabel(error));
      });
    return () => controller.abort();
  }, [selected, mode, cutoff, observationRetry]);

  useEffect(() => {
    loadingNextRevisionsRef.current = false;
    setLoadingNextRevisions(false);
    if (!selected) return;
    const controller = new AbortController();
    setRevisionState("loading");
    void listRevisions(selected.id, "", controller.signal)
      .then((page) => {
        setRevisions(page.items);
        setRevisionCursor(page.next_cursor ?? "");
        setRevisionMore(page.has_more);
        setRevisionState("ready");
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setRevisionError(errorLabel(error));
        setRevisionState("error");
      });
    return () => controller.abort();
  }, [selected]);

  const visibleSeries = series.filter((item) =>
    `${item.name} ${item.source_code} ${item.unit}`
      .toLowerCase()
      .includes(search.toLowerCase()),
  );
  const inspected =
    observations.find((item) => item.id === inspectedID) ?? observations[0];
  const points = useMemo(() => chartPoints(observations), [observations]);
  const comparisons = useMemo(() => {
    const groups = new Map<string, Observation[]>();
    for (const item of revisions)
      groups.set(item.observation_time, [
        ...(groups.get(item.observation_time) ?? []),
        item,
      ]);
    return [...groups.values()].flatMap((group) => {
      const sorted = group.sort((a, b) =>
        a.clocks.system_known_at.localeCompare(b.clocks.system_known_at),
      );
      return sorted
        .slice(1)
        .map((newer, index) => ({ older: sorted[index], newer }));
    });
  }, [revisions]);

  async function nextSeries() {
    if (!seriesCursor || loadingNextSeriesRef.current) return;
    loadingNextSeriesRef.current = true;
    setLoadingNextSeries(true);
    const generation = seriesContextRef.current.generation;
    const controller = new AbortController();
    try {
      const page = await listSeries(seriesCursor, controller.signal);
      if (
        controller.signal.aborted ||
        generation !== seriesContextRef.current.generation
      )
        return;
      setSeries((previous) => [...previous, ...page.items]);
      setSeriesCursor(page.next_cursor ?? "");
      setSeriesMore(page.has_more);
    } catch (error) {
      if (
        controller.signal.aborted ||
        generation !== seriesContextRef.current.generation
      )
        return;
      setSeriesError(errorLabel(error));
      setSeriesState("error");
    } finally {
      if (generation === seriesContextRef.current.generation) {
        loadingNextSeriesRef.current = false;
        setLoadingNextSeries(false);
      }
    }
  }

  async function nextObservations() {
    if (!selected || !observationCursor || loadingNextObservationsRef.current)
      return;
    loadingNextObservationsRef.current = true;
    setLoadingNextObservations(true);
    const generation = observationContextRef.current.generation;
    const controller = new AbortController();
    try {
      const page = await listObservations(
        selected.id,
        mode,
        cutoff,
        observationCursor,
        controller.signal,
      );
      if (
        controller.signal.aborted ||
        generation !== observationContextRef.current.generation
      )
        return;
      setObservations((previous) => [...previous, ...page.items]);
      setObservationCursor(page.next_cursor ?? "");
      setObservationMore(page.has_more);
    } catch (error) {
      if (
        controller.signal.aborted ||
        generation !== observationContextRef.current.generation
      )
        return;
      setObservationError(errorLabel(error));
      setObservationState("error");
    } finally {
      if (generation === observationContextRef.current.generation) {
        loadingNextObservationsRef.current = false;
        setLoadingNextObservations(false);
      }
    }
  }

  async function nextRevisions() {
    if (!selected || !revisionCursor || loadingNextRevisionsRef.current) return;
    loadingNextRevisionsRef.current = true;
    setLoadingNextRevisions(true);
    const generation = revisionContextRef.current.generation;
    const controller = new AbortController();
    try {
      const page = await listRevisions(
        selected.id,
        revisionCursor,
        controller.signal,
      );
      if (
        controller.signal.aborted ||
        generation !== revisionContextRef.current.generation
      )
        return;
      setRevisions((previous) => [...previous, ...page.items]);
      setRevisionCursor(page.next_cursor ?? "");
      setRevisionMore(page.has_more);
    } catch (error) {
      if (
        controller.signal.aborted ||
        generation !== revisionContextRef.current.generation
      )
        return;
      setRevisionError(errorLabel(error));
      setRevisionState("error");
    } finally {
      if (generation === revisionContextRef.current.generation) {
        loadingNextRevisionsRef.current = false;
        setLoadingNextRevisions(false);
      }
    }
  }

  return (
    <div className="timeline-workspace">
      <section className="timeline-series" aria-labelledby="series-title">
        <div className="section-heading">
          <div>
            <p className="eyebrow">01 / source index</p>
            <h2 id="series-title">Series</h2>
          </div>
          <span className="section-heading__meta">{series.length} loaded</span>
        </div>
        <label className="timeline-search">
          Search loaded series
          <input
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Name, source, or unit"
          />
        </label>
        {seriesState === "loading" && <p role="status">Loading series…</p>}
        {seriesState === "error" && (
          <p role="alert">
            {seriesError}{" "}
            <button
              type="button"
              onClick={() => setSeriesRetry((value) => value + 1)}
            >
              Retry
            </button>
          </p>
        )}
        {seriesState === "ready" && series.length === 0 && (
          <p>No series are available yet.</p>
        )}
        {seriesState === "ready" &&
          visibleSeries.length === 0 &&
          series.length > 0 && <p>No loaded series match this search.</p>}
        <div className="timeline-series__list">
          {visibleSeries.map((item) => (
            <button
              key={item.id}
              className={
                item.id === selectedID
                  ? "timeline-series__item timeline-series__item--active"
                  : "timeline-series__item"
              }
              type="button"
              aria-pressed={item.id === selectedID}
              onClick={() => setSelectedID(item.id)}
            >
              <strong>{item.name}</strong>
              <span>
                {item.source_code} · {item.unit} · {item.frequency}
              </span>
            </button>
          ))}
        </div>
        {seriesMore && (
          <button
            className="button button--quiet"
            type="button"
            disabled={loadingNextSeries}
            onClick={() => void nextSeries()}
          >
            {loadingNextSeries ? "Loading series…" : "Load more series"}
          </button>
        )}
      </section>

      <div className="timeline-main">
        <section aria-labelledby="reading-title">
          <div className="section-heading">
            <div>
              <p className="eyebrow">02 / point-in-time reading</p>
              <h2 id="reading-title">{selected?.name ?? "Select a series"}</h2>
            </div>
            <span className="section-heading__meta">
              {selected?.unit ?? "—"} ·{" "}
              {selected?.source_name ?? selected?.source_code ?? "—"}
            </span>
          </div>
          <div className="timeline-context" aria-live="polite">
            <span>
              Mode <strong>{mode}</strong>
            </span>
            <span>
              Knowledge cutoff{" "}
              <strong>
                {mode === "latest"
                  ? "Latest available"
                  : cutoff
                    ? dateLabel(new Date(cutoff).toISOString())
                    : "Required"}
              </strong>
            </span>
            <span>
              Observation clock <strong>UTC</strong>
            </span>
          </div>
          {selected && (
            <p className="timeline-detail">
              Source timezone: {selected.source_timezone ?? "Not supplied"} ·
              Frequency: {selected.frequency}. Values and chart use the mode and
              cutoff shown above.
            </p>
          )}
          {observationState === "choose-cutoff" && (
            <p className="timeline-callout" role="status">
              Choose a knowledge cutoff above before requesting an as-of
              reading.
            </p>
          )}
          {observationState === "unsupported" && (
            <p className="timeline-callout" role="status">
              Source as-of is unavailable for this series: the source did not
              provide a defensible source-knowledge clock. Choose system as-of
              or latest; publication time is not inferred.
            </p>
          )}
          {observationState === "loading" && selected && (
            <p role="status">Loading observations…</p>
          )}
          {observationState === "error" && (
            <p role="alert">
              {observationError}{" "}
              <button
                type="button"
                onClick={() => setObservationRetry((value) => value + 1)}
              >
                Retry
              </button>
            </p>
          )}
          {observationState === "ready" && observations.length === 0 && (
            <p className="timeline-callout">
              No observations exist for this reading. Missing data is not a
              healthy result.
            </p>
          )}
          {observationState === "ready" && observations.length > 0 && (
            <>
              {points ? (
                <figure className="timeline-chart">
                  <figcaption>
                    Loaded observation window · {mode} ·{" "}
                    {mode === "latest"
                      ? "latest"
                      : dateLabel(new Date(cutoff).toISOString())}
                  </figcaption>
                  <svg
                    viewBox="0 0 600 150"
                    role="img"
                    aria-label={`Chart of ${selected?.name ?? "series"} using ${mode} observations`}
                    preserveAspectRatio="none"
                  >
                    <path
                      d="M20 130H580"
                      stroke="currentColor"
                      opacity="0.25"
                    />
                    <polyline
                      points={points}
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2.5"
                      vectorEffect="non-scaling-stroke"
                    />
                  </svg>
                </figure>
              ) : (
                <p className="timeline-callout">
                  No numeric points to chart. Text and missing values remain in
                  the table.
                </p>
              )}
              <div className="timeline-table-wrap">
                <table className="timeline-table">
                  <caption>
                    Observations · {mode} ·{" "}
                    {mode === "latest"
                      ? "latest available"
                      : dateLabel(new Date(cutoff).toISOString())}
                  </caption>
                  <thead>
                    <tr>
                      <th scope="col">Observed at</th>
                      <th scope="col">Value / unit</th>
                      <th scope="col">Quality</th>
                      <th scope="col">Source known</th>
                      <th scope="col">System known</th>
                      <th scope="col">Evidence</th>
                    </tr>
                  </thead>
                  <tbody>
                    {observations.map((item) => (
                      <tr key={item.id}>
                        <td>{dateLabel(item.observation_time)}</td>
                        <td>
                          {valueLabel(item)} {item.unit}
                        </td>
                        <td>
                          <span
                            className={`timeline-quality timeline-quality--${qualityLabel(item)}`}
                          >
                            {qualityLabel(item)}
                          </span>
                        </td>
                        <td>{dateLabel(item.clocks.source_known_at)}</td>
                        <td>{dateLabel(item.clocks.system_known_at)}</td>
                        <td>
                          <button
                            type="button"
                            className="timeline-inspect"
                            onClick={() => setInspectedID(item.id)}
                          >
                            Inspect
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              {observationMore && (
                <button
                  className="button button--quiet"
                  type="button"
                  disabled={loadingNextObservations}
                  onClick={() => void nextObservations()}
                >
                  {loadingNextObservations
                    ? "Loading observations…"
                    : "Load more observations"}
                </button>
              )}
            </>
          )}
        </section>

        <div className="timeline-panels">
          <section className="timeline-panel" aria-labelledby="quality-title">
            <p className="eyebrow">03 / quality gate</p>
            <h2 id="quality-title">Freshness and completeness</h2>
            <p>
              Evaluation clock: system knowledge ·{" "}
              {mode === "latest"
                ? "current UTC"
                : cutoff
                  ? dateLabel(new Date(cutoff).toISOString())
                  : "cutoff required"}
              .
              {mode === "source-as-of"
                ? " This quality evaluation does not reselect the source-vintage observation."
                : ""}
            </p>
            {quality && observationState === "ready" ? (
              <>
                <QualityBadge
                  state={
                    quality.state === "valid"
                      ? "valid"
                      : quality.state === "degraded"
                        ? "degraded"
                        : "blocked"
                  }
                >
                  {quality.classification}
                </QualityBadge>
                <ul>
                  {quality.reasons.map((reason) => (
                    <li key={reason.code}>{reason.message}</li>
                  ))}
                </ul>
              </>
            ) : (
              <p>
                {qualityError
                  ? `Quality unavailable: ${qualityError}`
                  : "Quality not evaluated for this reading."}
              </p>
            )}
          </section>
          <section
            className="timeline-panel"
            aria-labelledby="provenance-title"
          >
            <p className="eyebrow">04 / provenance</p>
            <h2 id="provenance-title">Raw evidence</h2>
            {inspected && observationState === "ready" ? (
              <dl>
                <dt>Revision ID</dt>
                <dd>{inspected.id}</dd>
                <dt>Raw object ID</dt>
                <dd>{inspected.raw_provenance_id}</dd>
                <dt>SHA-256</dt>
                <dd>{inspected.raw_object_sha256}</dd>
                <dt>Knowledge basis</dt>
                <dd>{inspected.clocks.knowledge_time_basis}</dd>
              </dl>
            ) : (
              <p>
                Select an observation to inspect its immutable source reference.
              </p>
            )}
          </section>
        </div>

        <section
          className="timeline-revisions"
          aria-labelledby="revisions-title"
        >
          <div className="section-heading">
            <div>
              <p className="eyebrow">05 / revision history</p>
              <h2 id="revisions-title">What changed, and when</h2>
            </div>
            <span className="section-heading__meta">
              {revisions.length} revisions loaded
            </span>
          </div>
          {revisionState === "loading" && selected && (
            <p role="status">Loading revisions…</p>
          )}
          {revisionState === "error" && <p role="alert">{revisionError}</p>}
          {revisionState === "ready" && revisions.length === 0 && (
            <p>No revisions have been recorded for this series.</p>
          )}
          {revisionState === "ready" && revisions.length === 1 && (
            <p>One revision exists; there is no earlier value to compare.</p>
          )}
          {revisionState === "ready" &&
            comparisons.length === 0 &&
            revisions.length > 1 && (
              <p>
                Loaded revisions cover different observation dates. Load further
                pages to inspect earlier versions of the same date.
              </p>
            )}
          {comparisons.length > 0 && (
            <div className="timeline-table-wrap">
              <table className="timeline-table">
                <caption>
                  Loaded revision comparisons with both knowledge clocks
                </caption>
                <thead>
                  <tr>
                    <th scope="col">Observation</th>
                    <th scope="col">Old value</th>
                    <th scope="col">New value</th>
                    <th scope="col">Old source / system known</th>
                    <th scope="col">New source / system known</th>
                  </tr>
                </thead>
                <tbody>
                  {comparisons.map(({ older, newer }) => (
                    <tr key={`${older.id}-${newer.id}`}>
                      <td>{dateLabel(newer.observation_time)}</td>
                      <td>
                        {valueLabel(older)} {older.unit}
                      </td>
                      <td>
                        {valueLabel(newer)} {newer.unit}
                      </td>
                      <td>
                        {dateLabel(older.clocks.source_known_at)}
                        <br />
                        {dateLabel(older.clocks.system_known_at)}
                      </td>
                      <td>
                        {dateLabel(newer.clocks.source_known_at)}
                        <br />
                        {dateLabel(newer.clocks.system_known_at)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          {revisionMore && (
            <button
              className="button button--quiet"
              type="button"
              disabled={loadingNextRevisions}
              onClick={() => void nextRevisions()}
            >
              {loadingNextRevisions
                ? "Loading revisions…"
                : "Load more revisions"}
            </button>
          )}
        </section>
      </div>
    </div>
  );
}
