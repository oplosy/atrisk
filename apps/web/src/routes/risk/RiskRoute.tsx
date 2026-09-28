import { useEffect, useRef, useState, type FormEvent } from "react";
import type { RiskRun } from "../../../../../contracts/generated/typescript/contracts";
import {
  getRiskRun,
  submitRiskRun,
  type RiskRunSubmission,
} from "../../features/risk/riskApi";
import {
  lifecycleLabel,
  scenarioAttribution,
  scenarioOutput,
  type ScenarioAttribution,
} from "../../features/risk/riskResult";
import {
  scenarioConfigJSON,
  type ScenarioTemplate,
} from "../../features/risk/scenarioTemplates";
import "./risk.css";

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {};
}

function scalar(value: unknown): string {
  if (value === null || value === undefined) return "Unavailable";
  if (
    typeof value === "string" ||
    typeof value === "number" ||
    typeof value === "boolean"
  ) {
    return String(value);
  }
  return "See source evidence";
}

function isObject(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function idempotencyKey(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) {
    return crypto.randomUUID();
  }
  return `risk-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

interface WaterfallSegment {
  label: string;
  amount: string;
  from: number;
  to: number;
  kind: "positive" | "negative" | "total";
}

function waterfallSegments(
  attribution: ScenarioAttribution,
): WaterfallSegment[] | null {
  if (
    attribution.total_pnl === null ||
    attribution.interaction_residual === null
  ) {
    return null;
  }
  const baseline = Number(attribution.baseline_pnl);
  if (!Number.isFinite(baseline)) return null;
  let cursor = baseline;
  const segments: WaterfallSegment[] = [
    {
      label: "Baseline",
      amount: attribution.baseline_pnl,
      from: 0,
      to: cursor,
      kind: cursor < 0 ? "negative" : "positive",
    },
  ];
  for (const item of attribution.factor_contributions) {
    const amount = Number(item.contribution);
    if (!Number.isFinite(amount)) return null;
    const next = cursor + amount;
    if (!Number.isFinite(next)) return null;
    segments.push({
      label: item.factor,
      amount: item.contribution,
      from: cursor,
      to: next,
      kind: amount < 0 ? "negative" : "positive",
    });
    cursor = next;
  }
  const residual = Number(attribution.interaction_residual);
  const total = Number(attribution.total_pnl);
  if (!Number.isFinite(residual) || !Number.isFinite(total)) return null;
  segments.push({
    label: "Interaction residual",
    amount: attribution.interaction_residual,
    from: cursor,
    to: cursor + residual,
    kind: residual < 0 ? "negative" : "positive",
  });
  segments.push({
    label: "Server total",
    amount: attribution.total_pnl,
    from: 0,
    to: total,
    kind: "total",
  });
  const minimum = Math.min(
    0,
    ...segments.map((segment) => Math.min(segment.from, segment.to)),
  );
  const maximum = Math.max(
    0,
    ...segments.map((segment) => Math.max(segment.from, segment.to)),
  );
  const range = maximum - minimum || 1;
  if (!Number.isFinite(range)) return null;
  return segments.map((segment) => ({
    ...segment,
    from: ((Math.min(segment.from, segment.to) - minimum) / range) * 100,
    to: (Math.abs(segment.to - segment.from) / range) * 100,
  }));
}

function LossWaterfall({ attribution }: { attribution: ScenarioAttribution }) {
  const segments = waterfallSegments(attribution);
  if (!segments) return null;
  return (
    <>
      <figure className="risk-waterfall" aria-hidden="true">
        <figcaption>
          Loss waterfall · {attribution.currency.toUpperCase()}
        </figcaption>
        {segments.map((segment) => (
          <div className="risk-waterfall__row" key={segment.label}>
            <span>{segment.label}</span>
            <span className="risk-waterfall__track">
              <span
                className={`risk-waterfall__bar risk-waterfall__bar--${segment.kind}`}
                style={{ left: `${segment.from}%`, width: `${segment.to}%` }}
              />
            </span>
            <span className="risk-waterfall__amount">{segment.amount}</span>
          </div>
        ))}
      </figure>
      <p className="risk-waterfall__note">
        Bars are a visual scale only. Exact amounts and the reconciliation
        verdict come from the risk engine.
      </p>
    </>
  );
}

function MetricTable({
  title,
  values,
}: {
  title: string;
  values: Record<string, unknown>;
}) {
  const rows = Object.entries(values).filter(
    ([key]) => key !== "returns" && key !== "top_weights",
  );
  return (
    <div className="risk-table-wrap">
      <table>
        <caption>{title}</caption>
        <thead>
          <tr>
            <th scope="col">Measure</th>
            <th scope="col">Value</th>
            <th scope="col">Coverage / state</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 && (
            <tr>
              <td colSpan={3}>Not recorded in this run.</td>
            </tr>
          )}
          {rows.map(([key, value]) => {
            const detail = record(value);
            return (
              <tr key={key}>
                <th scope="row">{key.replaceAll("_", " ")}</th>
                <td>
                  {Object.keys(detail).length
                    ? scalar(
                        detail.annualized ??
                          detail.coefficient ??
                          detail.maximum ??
                          detail.gross ??
                          detail.hhi,
                      )
                    : scalar(value)}
                </td>
                <td>
                  {Object.keys(detail).length
                    ? [
                        detail.state && `State: ${scalar(detail.state)}`,
                        detail.observations !== undefined &&
                          `Observations: ${scalar(detail.observations)}`,
                        detail.required_observations !== undefined &&
                          `Required: ${scalar(detail.required_observations)}`,
                        detail.overlap_count !== undefined &&
                          `Overlap: ${scalar(detail.overlap_count)}`,
                        detail.required_overlap !== undefined &&
                          `Required overlap: ${scalar(detail.required_overlap)}`,
                        detail.reason && `Reason: ${scalar(detail.reason)}`,
                      ]
                        .filter(Boolean)
                        .join(" · ") || "See provenance"
                    : "—"}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

export function RiskRoute() {
  const [accountId, setAccountId] = useState("");
  const [snapshotId, setSnapshotId] = useState("");
  const [valuationId, setValuationId] = useState("");
  const [scenarioId, setScenarioId] = useState("");
  const [scenarioName, setScenarioName] = useState("Risk scenario");
  const [templateKey, setTemplateKey] = useState<ScenarioTemplate>("risk_off");
  const [scenarioConfig, setScenarioConfig] = useState(
    scenarioConfigJSON("risk_off"),
  );
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState("");
  const idempotencyRef = useRef<{ body: string; key: string } | null>(null);
  const [draftRunId, setDraftRunId] = useState("");
  const [selectedRunId, setSelectedRunId] = useState("");
  const selectionVersionRef = useRef(0);
  const [refreshNonce, setRefreshNonce] = useState(0);
  const [run, setRun] = useState<RiskRun | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!selectedRunId) return;
    let active = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const refresh = async () => {
      try {
        const latest = await getRiskRun(selectedRunId);
        if (!active) return;
        setRun(latest);
        setError("");
        setLoading(false);
        if (["queued", "running", "retryable"].includes(latest.status)) {
          timer = setTimeout(refresh, 3000);
        }
      } catch (cause) {
        if (active) {
          setError(cause instanceof Error ? cause.message : String(cause));
          setLoading(false);
        }
      }
    };
    void refresh();
    return () => {
      active = false;
      if (timer) clearTimeout(timer);
    };
  }, [selectedRunId, refreshNonce]);

  const openRun = (event: FormEvent) => {
    event.preventDefault();
    const id = draftRunId.trim();
    if (!id) return;
    selectionVersionRef.current += 1;
    setRun(null);
    setError("");
    setLoading(true);
    setSelectedRunId(id);
    setRefreshNonce((current) => current + 1);
  };

  const createRun = async (event: FormEvent) => {
    event.preventDefault();
    setSubmitError("");
    let config: Record<string, unknown>;
    try {
      const parsed: unknown = JSON.parse(scenarioConfig);
      if (
        !isObject(parsed) ||
        !["units", "shocks", "mappings", "assumptions"].every((key) =>
          isObject(parsed[key]),
        )
      ) {
        throw new Error(
          "Include object values for units, shocks, mappings, and assumptions.",
        );
      }
      config = parsed;
    } catch (cause) {
      setSubmitError(
        cause instanceof SyntaxError
          ? "Scenario parameters are not valid JSON. Correct them and retry."
          : cause instanceof Error
            ? cause.message
            : String(cause),
      );
      return;
    }

    const submission: RiskRunSubmission = {
      account_id: accountId.trim(),
      snapshot_id: snapshotId.trim(),
      valuation_id: valuationId.trim(),
      ...(scenarioId.trim() ? { scenario_id: scenarioId.trim() } : {}),
      name: scenarioName.trim(),
      template_key: templateKey,
      units: config.units as Record<string, unknown>,
      shocks: config.shocks as Record<string, unknown>,
      mappings: config.mappings as Record<string, unknown>,
      assumptions: config.assumptions as Record<string, unknown>,
    };
    const body = JSON.stringify(submission);
    if (idempotencyRef.current?.body !== body) {
      idempotencyRef.current = { body, key: idempotencyKey() };
    }
    const selectionVersion = selectionVersionRef.current;

    setSubmitting(true);
    try {
      const created = await submitRiskRun(
        submission,
        idempotencyRef.current.key,
      );
      if (selectionVersionRef.current === selectionVersion) {
        idempotencyRef.current = null;
        setRun(created);
        setDraftRunId(created.id);
        setSelectedRunId(created.id);
        setLoading(true);
        setRefreshNonce((current) => current + 1);
      }
    } catch (cause) {
      setSubmitError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setSubmitting(false);
    }
  };

  const output = run ? scenarioOutput(run) : null;
  const attribution = scenarioAttribution(output, run?.data_quality);
  const metrics = record(output?.pre_metrics);
  const postMetrics = record(output?.post_metrics);
  const complete = run?.status === "completed" && output?.state === "valid";

  return (
    <div className="risk-workspace">
      <section
        className="risk-section risk-create"
        aria-labelledby="risk-create-title"
      >
        <div>
          <h2 id="risk-create-title">Run a versioned scenario</h2>
          <p>
            The API binds the run to an existing account, snapshot, and valid
            valuation. Position amounts and pre-shock metrics are sealed by the
            server; they cannot be supplied by this form.
          </p>
        </div>
        <form className="risk-create__form" onSubmit={createRun}>
          <label>
            Account ID
            <input
              value={accountId}
              onChange={(event) => setAccountId(event.target.value)}
              required
              aria-label="Account ID"
              autoComplete="off"
              placeholder="Account UUID"
            />
          </label>
          <label>
            Snapshot ID
            <input
              value={snapshotId}
              onChange={(event) => setSnapshotId(event.target.value)}
              required
              aria-label="Snapshot ID"
              autoComplete="off"
              placeholder="Snapshot UUID"
            />
          </label>
          <label>
            Valuation ID
            <input
              value={valuationId}
              onChange={(event) => setValuationId(event.target.value)}
              required
              aria-label="Valuation ID"
              autoComplete="off"
              placeholder="Valid valuation UUID"
            />
          </label>
          <label>
            Scenario name
            <input
              value={scenarioName}
              onChange={(event) => setScenarioName(event.target.value)}
              required
              aria-label="Scenario name"
              maxLength={120}
            />
          </label>
          <label>
            Scenario template
            <select
              value={templateKey}
              onChange={(event) => {
                const next = event.target.value as ScenarioTemplate;
                setTemplateKey(next);
                setScenarioConfig(scenarioConfigJSON(next));
              }}
            >
              <option value="try_depreciation">TRY depreciation</option>
              <option value="rates_up">Rates up</option>
              <option value="risk_off">Risk off</option>
            </select>
          </label>
          <label>
            Existing scenario ID (optional)
            <input
              value={scenarioId}
              onChange={(event) => setScenarioId(event.target.value)}
              aria-label="Existing scenario ID (optional)"
              autoComplete="off"
              placeholder="Leave blank to create a scenario"
            />
          </label>
          <details className="risk-config">
            <summary>Versioned scenario parameters</summary>
            <p>
              These settings are stored with the immutable scenario version. Map
              instruments explicitly when the selected template needs an asset
              class; unmapped positions remain blocked in the result.
            </p>
            <label htmlFor="risk-scenario-config">Configuration JSON</label>
            <textarea
              id="risk-scenario-config"
              value={scenarioConfig}
              onChange={(event) => setScenarioConfig(event.target.value)}
              rows={14}
              spellCheck={false}
            />
          </details>
          {submitError && (
            <p className="risk-error" role="alert">
              {submitError} The form values are preserved; correct the input or
              retry.
            </p>
          )}
          <button type="submit" disabled={submitting}>
            {submitting ? "Submitting…" : "Run scenario"}
          </button>
        </form>
      </section>

      <section className="risk-open" aria-labelledby="risk-open-title">
        <div>
          <h2 id="risk-open-title">Inspect a risk run</h2>
          <p>
            Open a recorded run by ID to inspect its job state, evidence, and
            position-level result.
          </p>
        </div>
        <form onSubmit={openRun} className="risk-open__form">
          <label htmlFor="risk-run-id">Run ID</label>
          <input
            id="risk-run-id"
            value={draftRunId}
            onChange={(event) => setDraftRunId(event.target.value)}
            required
            placeholder="Risk run UUID"
          />
          <button type="submit">Open run</button>
        </form>
      </section>

      {loading && (
        <p role="status" className="risk-message">
          Loading run…
        </p>
      )}
      {error && (
        <p role="alert" className="risk-error">
          {error}{" "}
          <button
            type="button"
            onClick={() => {
              setLoading(true);
              setError("");
              setRefreshNonce((current) => current + 1);
            }}
          >
            Retry run
          </button>
        </p>
      )}
      {!run && !loading && !error && (
        <p className="risk-empty">
          No run selected. Create a scenario above or open an existing run by
          ID.
        </p>
      )}

      {run && (
        <>
          <section className="risk-section" aria-labelledby="risk-state-title">
            <div className="risk-section__head">
              <div>
                <h2 id="risk-state-title">{lifecycleLabel(run)}</h2>
                <p>
                  Scenario {run.scenario_template.replaceAll("_", " ")} ·
                  version {run.scenario_version}
                </p>
              </div>
              <span
                className={`risk-quality risk-quality--${run.data_quality}`}
              >
                {run.data_quality}
              </span>
            </div>
            {(run.status === "retryable" || run.status === "permanent") && (
              <p role="alert" className="risk-error">
                {run.error_code || "JOB_ERROR"}:{" "}
                {run.error_message || "No error detail was returned."}
              </p>
            )}
            {output && output.state !== "valid" && (
              <p role="alert" className="risk-warning">
                {output.state} result: required or mapped inputs are incomplete.
                P&L is not a complete portfolio stress loss.
              </p>
            )}
            <dl className="risk-facts">
              <div>
                <dt>Run</dt>
                <dd>{run.id}</dd>
              </div>
              <div>
                <dt>Snapshot</dt>
                <dd>{run.snapshot_id}</dd>
              </div>
              <div>
                <dt>Engine</dt>
                <dd>{run.engine_version}</dd>
              </div>
              <div>
                <dt>Input schema</dt>
                <dd>{run.schema_version}</dd>
              </div>
              <div>
                <dt>Created</dt>
                <dd>{run.created_at}</dd>
              </div>
              <div>
                <dt>Completed</dt>
                <dd>{run.completed_at ?? "Not completed"}</dd>
              </div>
            </dl>
          </section>

          {output && (
            <>
              <section
                className="risk-section"
                aria-labelledby="risk-loss-title"
              >
                <h2 id="risk-loss-title">Stress loss and coverage</h2>
                <div className="risk-losses">
                  <p>
                    <span>
                      TRY {complete ? "total P&L" : "covered-position P&L"}
                    </span>
                    <strong>{output.portfolio_pnl_try ?? "Unavailable"}</strong>
                  </p>
                  <p>
                    <span>
                      USD {complete ? "total P&L" : "covered-position P&L"}
                    </span>
                    <strong>{output.portfolio_pnl_usd ?? "Unavailable"}</strong>
                  </p>
                </div>
                <p>
                  {output.positions.length} position result(s) ·{" "}
                  {output.unmapped_instruments.length} unmapped instrument(s)
                </p>
                {output.unmapped_instruments.length > 0 && (
                  <div className="risk-warning" role="alert">
                    <strong>Unmapped instruments</strong>
                    <ul>
                      {output.unmapped_instruments.map((item, index) => (
                        <li key={`${item.instrument_id}-${index}`}>
                          {item.instrument_id}: {item.reason_code}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}
                {output.reason_codes.length > 0 && (
                  <p>Reasons: {output.reason_codes.join(", ")}</p>
                )}
                <div className="risk-table-wrap">
                  <table>
                    <caption>Position-level stress results</caption>
                    <thead>
                      <tr>
                        <th scope="col">Instrument / snapshot line</th>
                        <th scope="col">State</th>
                        <th scope="col">TRY P&L</th>
                        <th scope="col">USD P&L</th>
                        <th scope="col">Reasons</th>
                      </tr>
                    </thead>
                    <tbody>
                      {output.positions.map((position, index) => (
                        <tr key={`${position.snapshot_line_id}-${index}`}>
                          <th scope="row">
                            {position.instrument_id}
                            <small>{position.snapshot_line_id}</small>
                          </th>
                          <td>{position.state}</td>
                          <td>{position.pnl_try ?? "Unavailable"}</td>
                          <td>{position.pnl_usd ?? "Unavailable"}</td>
                          <td>{position.reason_codes.join(", ") || "—"}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <div className="risk-attribution">
                  <h3>Factor attribution</h3>
                  {attribution ? (
                    <>
                      <p>
                        {attribution.method} {attribution.method_version} ·{" "}
                        {attribution.currency.toUpperCase()} ·{" "}
                        {attribution.state}
                      </p>
                      <p
                        className={
                          attribution.reconciles
                            ? "risk-attribution__result"
                            : "risk-attribution__result risk-attribution__result--warning"
                        }
                        role={attribution.reconciles ? "status" : "alert"}
                      >
                        {attribution.reconciles
                          ? `Server reports reconciliation within tolerance ${attribution.tolerance}.`
                          : `Attribution does not reconcile within tolerance ${attribution.tolerance}. Treat this run as incomplete.`}
                      </p>
                      <LossWaterfall attribution={attribution} />
                      <div className="risk-table-wrap">
                        <table>
                          <caption>
                            {attribution.currency.toUpperCase()} loss
                            reconciliation
                          </caption>
                          <thead>
                            <tr>
                              <th scope="col">Component</th>
                              <th scope="col">P&amp;L</th>
                            </tr>
                          </thead>
                          <tbody>
                            <tr>
                              <th scope="row">Server total</th>
                              <td>{attribution.total_pnl ?? "Unavailable"}</td>
                            </tr>
                            <tr>
                              <th scope="row">Baseline</th>
                              <td>{attribution.baseline_pnl}</td>
                            </tr>
                            {attribution.factor_contributions.map((item) => (
                              <tr key={item.factor}>
                                <th scope="row">{item.factor}</th>
                                <td>{item.contribution}</td>
                              </tr>
                            ))}
                            <tr>
                              <th scope="row">Interaction residual</th>
                              <td>
                                {attribution.interaction_residual ??
                                  "Unavailable"}
                              </td>
                            </tr>
                          </tbody>
                        </table>
                      </div>
                      <details className="risk-position-attribution">
                        <summary>
                          Position attribution (
                          {attribution.position_contributions.length})
                        </summary>
                        <div className="risk-table-wrap">
                          <table>
                            <caption>
                              Server-calculated position contributions
                            </caption>
                            <thead>
                              <tr>
                                <th scope="col">Instrument / snapshot line</th>
                                <th scope="col">State</th>
                                <th scope="col">Total P&amp;L</th>
                                <th scope="col">Factor contributions</th>
                                <th scope="col">Residual</th>
                              </tr>
                            </thead>
                            <tbody>
                              {attribution.position_contributions.map(
                                (item) => (
                                  <tr key={item.snapshot_line_id}>
                                    <th scope="row">
                                      {item.instrument_id}
                                      <small>{item.snapshot_line_id}</small>
                                    </th>
                                    <td>{item.state}</td>
                                    <td>{item.total_pnl ?? "Unavailable"}</td>
                                    <td>
                                      {item.factor_contributions.length > 0
                                        ? item.factor_contributions
                                            .map(
                                              (factor) =>
                                                `${factor.factor}: ${factor.contribution}`,
                                            )
                                            .join(" · ")
                                        : "None recorded"}
                                    </td>
                                    <td>{item.residual ?? "Unavailable"}</td>
                                  </tr>
                                ),
                              )}
                            </tbody>
                          </table>
                        </div>
                      </details>
                    </>
                  ) : (
                    <p>
                      {output.attribution
                        ? "Attribution data is incomplete or malformed; no figures are displayed."
                        : "This run has no server-provided factor attribution. No figures are inferred in the browser."}
                    </p>
                  )}
                </div>
              </section>
              <section
                className="risk-section"
                aria-labelledby="risk-metrics-title"
              >
                <h2 id="risk-metrics-title">Risk metrics</h2>
                <p>
                  Calendar: {scalar(metrics.calendar)} · annualization factor:{" "}
                  {scalar(metrics.annualization_factor)} · pre-shock quality:{" "}
                  {scalar(metrics.data_quality)}
                </p>
                <div className="risk-metric-grid">
                  <MetricTable
                    title="Volatility and sample coverage"
                    values={record(metrics.volatility)}
                  />
                  <MetricTable
                    title="Correlation and overlap coverage"
                    values={record(metrics.correlations)}
                  />
                  <MetricTable
                    title="Drawdown"
                    values={record(metrics.drawdown)}
                  />
                  <MetricTable
                    title="Leverage"
                    values={record(metrics.leverage)}
                  />
                  <MetricTable
                    title="Concentration"
                    values={record(metrics.concentration)}
                  />
                  <MetricTable
                    title="Post-shock volatility"
                    values={record(postMetrics.volatility)}
                  />
                  <MetricTable
                    title="Post-shock correlation"
                    values={record(postMetrics.correlations)}
                  />
                </div>
              </section>
            </>
          )}

          <details className="risk-section risk-provenance">
            <summary>Provenance and immutable versions</summary>
            <dl className="risk-facts">
              <div>
                <dt>Account ID</dt>
                <dd>{run.account_id}</dd>
              </div>
              <div>
                <dt>Snapshot ID</dt>
                <dd>{run.snapshot_id}</dd>
              </div>
              <div>
                <dt>Valuation ID</dt>
                <dd>{run.valuation_id}</dd>
              </div>
              <div>
                <dt>Scenario ID</dt>
                <dd>{run.scenario_id}</dd>
              </div>
              <div>
                <dt>Version</dt>
                <dd>{run.scenario_version}</dd>
              </div>
              <div>
                <dt>Scenario content SHA-256</dt>
                <dd>{run.scenario_content_sha256}</dd>
              </div>
              <div>
                <dt>Request hash</dt>
                <dd>{run.request_hash}</dd>
              </div>
              <div>
                <dt>Result hash</dt>
                <dd>{run.result_hash || "Not recorded"}</dd>
              </div>
              <div>
                <dt>Input snapshots</dt>
                <dd>{run.input_snapshot_ids.join(", ") || "None"}</dd>
              </div>
              {(() => {
                const input = record(output?.input_provenance);
                const lineage = Array.isArray(input.lines) ? input.lines : [];
                const fxLines = (value: unknown) =>
                  Array.isArray(value)
                    ? value
                        .map((entry) => {
                          const quote = record(entry);
                          return [
                            scalar(quote.quote_revision_id ?? quote.id),
                            scalar(quote.pair),
                            scalar(quote.direction),
                          ].join(" → ");
                        })
                        .join("; ") || "None"
                    : scalar(value);
                return (
                  <>
                    <div>
                      <dt>Valuation input state</dt>
                      <dd>{scalar(input.state)}</dd>
                    </div>
                    <div>
                      <dt>Valuation cutoff</dt>
                      <dd>{scalar(input.cutoff)}</dd>
                    </div>
                    <div>
                      <dt>Knowledge mode / known at</dt>
                      <dd>
                        {scalar(input.knowledge_mode)} /{" "}
                        {scalar(input.known_at)}
                      </dd>
                    </div>
                    <div>
                      <dt>Valuation result hash</dt>
                      <dd>{scalar(input.valuation_result_hash)}</dd>
                    </div>
                    <div>
                      <dt>Price / FX maximum age (seconds)</dt>
                      <dd>
                        {scalar(input.price_max_age_seconds)} /{" "}
                        {scalar(input.fx_max_age_seconds)}
                      </dd>
                    </div>
                    {lineage.map((value, index) => {
                      const line = record(value);
                      return (
                        <div key={`${scalar(line.snapshot_line_id)}-${index}`}>
                          <dt>
                            Price / FX lineage · {scalar(line.snapshot_line_id)}
                          </dt>
                          <dd>
                            Line {scalar(line.snapshot_line_id)}; price revision{" "}
                            {scalar(line.price_revision_id)} (
                            {scalar(line.price_quote_unit)}); TRY FX{" "}
                            {fxLines(line.try_fx_path)}; USD FX{" "}
                            {fxLines(line.usd_fx_path)}
                          </dd>
                        </div>
                      );
                    })}
                  </>
                );
              })()}
            </dl>
          </details>
        </>
      )}
    </div>
  );
}
