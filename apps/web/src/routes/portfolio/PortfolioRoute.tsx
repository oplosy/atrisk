import { useEffect, useRef, useState, type FormEvent } from "react";
import type {
  Account,
  Instrument,
  Portfolio,
  Snapshot,
  ValuationLine,
  ValuationRun,
} from "../../../../../contracts/generated/typescript/contracts";
import {
  api,
  importBody,
  portfolioApi,
  sha256,
  type ImportPreview,
  type ImportResult,
  type ReconciliationCheckpoint,
} from "../../features/portfolio/portfolioApi";
import "./portfolio.css";

const localNow = () => {
  const now = new Date();
  return new Date(now.getTime() - now.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 16);
};

const iso = (value: string) => new Date(value).toISOString();
const decimal = /^-?(?:0|[1-9]\d*)(?:\.\d+)?$/;

interface PreviewLock {
  hash: string;
  portfolioId: string;
  capturedAt: string;
  idempotencyKey: string;
  preview: ImportPreview;
}

interface DraftLine {
  account_id: string;
  instrument_id: string;
  quantity: string;
}

interface PortfolioContext {
  id: string;
  generation: number;
}

function displayLine(line: ValuationLine, currency: "native" | "TRY" | "USD") {
  const amount =
    currency === "native"
      ? line.native_amount
      : currency === "TRY"
        ? line.try_amount
        : line.usd_amount;
  return amount == null
    ? "Unpriced"
    : `${amount} ${currency === "native" ? line.native_currency : currency}`;
}

function FxPath({
  label,
  path,
}: {
  label: string;
  path: ValuationLine["try_fx_path"];
}) {
  return (
    <p>
      <strong>{label}:</strong>{" "}
      {path.length === 0
        ? "No FX conversion"
        : path.map((step, index) => (
            <span key={`${step.quote_revision_id}-${index}`}>
              {index > 0 ? " → " : ""}
              {step.quote_revision_id} ({step.direction})
            </span>
          ))}
    </p>
  );
}

export function PortfolioRoute() {
  const [portfolios, setPortfolios] = useState<Portfolio[]>([]);
  const [portfolioId, setPortfolioId] = useState("");
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [instruments, setInstruments] = useState<Instrument[]>([]);
  const [snapshots, setSnapshots] = useState<Snapshot[]>([]);
  const [draftLines, setDraftLines] = useState<DraftLine[]>([
    { account_id: "", instrument_id: "", quantity: "" },
  ]);
  const [capturedAt, setCapturedAt] = useState(localNow);
  const [file, setFile] = useState<File | null>(null);
  const [previewLock, setPreviewLock] = useState<PreviewLock | null>(null);
  const [snapshotId, setSnapshotId] = useState("");
  const [cutoff, setCutoff] = useState(localNow);
  const [knownAt, setKnownAt] = useState(localNow);
  const [knowledgeMode, setKnowledgeMode] = useState<
    "system_as_of" | "source_as_of"
  >("system_as_of");
  const [priceMaxAge, setPriceMaxAge] = useState("86400");
  const [fxMaxAge, setFxMaxAge] = useState("86400");
  const [valuation, setValuation] = useState<ValuationRun | null>(null);
  const [currency, setCurrency] = useState<"native" | "TRY" | "USD">("TRY");
  const [reconcileAccountId, setReconcileAccountId] = useState("");
  const [reconcileCurrency, setReconcileCurrency] = useState<"TRY" | "USD">(
    "TRY",
  );
  const [externalNav, setExternalNav] = useState("");
  const [sourceLabel, setSourceLabel] = useState("");
  const [reconciliation, setReconciliation] =
    useState<ReconciliationCheckpoint | null>(null);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const portfolioContext = useRef<PortfolioContext>({ id: "", generation: 0 });

  const isCurrentPortfolio = (context: PortfolioContext) => {
    const current = portfolioContext.current;
    return (
      current.id === context.id && current.generation === context.generation
    );
  };

  useEffect(() => {
    Promise.all([portfolioApi.portfolios(), portfolioApi.instruments()])
      .then(([portfolioPage, instrumentPage]) => {
        setPortfolios(portfolioPage.items);
        setInstruments(instrumentPage.items);
        setPortfolioId((current) => {
          if (current || !portfolioPage.items[0]?.id) return current;
          portfolioContext.current = {
            id: portfolioPage.items[0].id,
            generation: portfolioContext.current.generation + 1,
          };
          return portfolioPage.items[0].id;
        });
      })
      .catch((cause: unknown) => setError(String(cause)));
  }, []);

  useEffect(() => {
    if (!portfolioId) return;
    let active = true;
    Promise.all([
      portfolioApi.accounts(portfolioId),
      portfolioApi.snapshots(portfolioId),
    ])
      .then(([accountPage, snapshotPage]) => {
        if (!active) return;
        setAccounts(accountPage.items);
        setSnapshots(snapshotPage.items);
        setDraftLines((current) =>
          current.map((line) => ({
            ...line,
            account_id: line.account_id || accountPage.items[0]?.id || "",
          })),
        );
        setReconcileAccountId(accountPage.items[0]?.id || "");
        setSnapshotId(snapshotPage.items[0]?.id || "");
      })
      .catch((cause: unknown) => {
        if (active) setError(String(cause));
      });
    return () => {
      active = false;
    };
  }, [portfolioId]);

  const run = async (
    label: string,
    action: (context: PortfolioContext) => Promise<void>,
  ) => {
    const context = { ...portfolioContext.current };
    setBusy(label);
    setError("");
    setNotice("");
    try {
      await action(context);
    } catch (cause) {
      if (isCurrentPortfolio(context)) {
        setError(cause instanceof Error ? cause.message : String(cause));
      }
    } finally {
      if (isCurrentPortfolio(context)) setBusy("");
    }
  };

  const createSnapshot = (event: FormEvent) => {
    event.preventDefault();
    void run("snapshot", async (context) => {
      if (
        !portfolioId ||
        draftLines.some(
          (line) =>
            !line.account_id ||
            !line.instrument_id ||
            !decimal.test(line.quantity),
        )
      ) {
        throw new Error(
          "Every position needs an account, instrument, and exact decimal quantity.",
        );
      }
      const snapshot = await portfolioApi.createSnapshot(portfolioId, {
        captured_at: iso(capturedAt),
        lines: draftLines,
      });
      if (!isCurrentPortfolio(context)) return;
      setSnapshots((current) => [snapshot, ...current]);
      setSnapshotId(snapshot.id);
      setNotice(`Snapshot ${snapshot.id} created.`);
    });
  };

  const previewCsv = () =>
    void run("preview", async (context) => {
      setPreviewLock(null);
      if (!file || !portfolioId)
        throw new Error("Choose a CSV file and portfolio.");
      const hash = await sha256(file);
      const captured = iso(capturedAt);
      const preview = await api<ImportPreview>(
        "/api/v1/imports/positions/preview",
        {
          method: "POST",
          body: importBody(file, portfolioId, captured),
        },
      );
      if (!isCurrentPortfolio(context)) return;
      if (preview.content_sha256 !== hash) {
        throw new Error(
          "Preview hash differs from the selected file. Commit remains locked.",
        );
      }
      setPreviewLock({
        hash,
        portfolioId,
        capturedAt: captured,
        idempotencyKey: crypto.randomUUID(),
        preview,
      });
      setNotice(
        `Previewed ${preview.row_count} rows. ${preview.valid ? "Ready to commit." : "Resolve diagnostics before commit."}`,
      );
    });

  const commitCsv = () =>
    void run("commit", async (context) => {
      if (
        !file ||
        !previewLock ||
        !previewLock.preview.valid ||
        !previewLock.preview.token
      ) {
        throw new Error("A valid preview with a commit token is required.");
      }
      const hash = await sha256(file);
      const captured = iso(capturedAt);
      if (
        hash !== previewLock.hash ||
        portfolioId !== previewLock.portfolioId ||
        captured !== previewLock.capturedAt
      ) {
        setPreviewLock(null);
        throw new Error(
          "The file or import context changed. Preview again before commit.",
        );
      }
      const body = importBody(file, portfolioId, captured);
      body.append("token", previewLock.preview.token);
      const result = await api<ImportResult>(
        "/api/v1/imports/positions/commit",
        {
          method: "POST",
          headers: { "Idempotency-Key": previewLock.idempotencyKey },
          body,
        },
      );
      if (!isCurrentPortfolio(context)) return;
      if (result.content_sha256 !== hash)
        throw new Error("Committed hash differs from the previewed file.");
      const page = await portfolioApi.snapshots(portfolioId);
      if (!isCurrentPortfolio(context)) return;
      setSnapshots(page.items);
      setSnapshotId(result.snapshot_id);
      setPreviewLock(null);
      setNotice(`CSV committed as snapshot ${result.snapshot_id}.`);
    });

  const createValuation = (event: FormEvent) => {
    event.preventDefault();
    void run("valuation", async (context) => {
      if (!snapshotId) throw new Error("Choose a snapshot first.");
      const result = await portfolioApi.createValuation({
        snapshot_id: snapshotId,
        cutoff: iso(cutoff),
        known_at: iso(knownAt),
        knowledge_mode: knowledgeMode,
        price_max_age_seconds: Number(priceMaxAge),
        fx_max_age_seconds: Number(fxMaxAge),
      });
      if (!isCurrentPortfolio(context)) return;
      setValuation(result);
      setReconciliation(null);
      setNotice(`Valuation ${result.id} recorded.`);
    });
  };

  const createReconciliation = (event: FormEvent) => {
    event.preventDefault();
    void run("reconciliation", async (context) => {
      if (!valuation || valuation.state !== "valid")
        throw new Error("A valid valuation is required for reconciliation.");
      if (
        !reconcileAccountId ||
        !sourceLabel.trim() ||
        !decimal.test(externalNav)
      ) {
        throw new Error(
          "Select an account, name the source, and enter an exact external NAV.",
        );
      }
      const result = await portfolioApi.reconcile(valuation.id, {
        account_id: reconcileAccountId,
        source_label: sourceLabel.trim(),
        currency: reconcileCurrency,
        cutoff: valuation.cutoff,
        external_nav: externalNav,
      });
      if (!isCurrentPortfolio(context)) return;
      setReconciliation(result);
      setNotice(`Reconciliation ${result.id} recorded.`);
    });
  };

  return (
    <div className="portfolio-workspace">
      <div className="portfolio-workspace__status" aria-live="polite">
        {error && (
          <p role="alert" className="portfolio-error">
            {error}
          </p>
        )}
        {notice && <p className="portfolio-notice">{notice}</p>}
      </div>
      <label className="portfolio-context">
        Portfolio
        <select
          value={portfolioId}
          onChange={(event) => {
            const nextPortfolioId = event.target.value;
            portfolioContext.current = {
              id: nextPortfolioId,
              generation: portfolioContext.current.generation + 1,
            };
            setPortfolioId(nextPortfolioId);
            setBusy("");
            setPreviewLock(null);
            setValuation(null);
            setReconciliation(null);
            setSnapshots([]);
            setSnapshotId("");
            setAccounts([]);
            setDraftLines([
              { account_id: "", instrument_id: "", quantity: "" },
            ]);
          }}
        >
          {portfolios.map((portfolio) => (
            <option key={portfolio.id} value={portfolio.id}>
              {portfolio.name}
            </option>
          ))}
        </select>
      </label>
      <div className="portfolio-grid">
        <section
          aria-labelledby="snapshot-heading"
          className="portfolio-section"
        >
          <h2 id="snapshot-heading">Position snapshot</h2>
          <p>
            Each save creates an immutable snapshot. Quantities stay exact
            decimals.
          </p>
          <form onSubmit={createSnapshot} className="portfolio-form">
            <label>
              Captured at{" "}
              <input
                type="datetime-local"
                required
                value={capturedAt}
                onChange={(event) => {
                  setCapturedAt(event.target.value);
                  setPreviewLock(null);
                }}
              />
            </label>
            {draftLines.map((line, index) => (
              <div className="portfolio-draft-line" key={index}>
                <strong>Position {index + 1}</strong>
                <label>
                  Account
                  <select
                    required
                    value={line.account_id}
                    onChange={(event) =>
                      setDraftLines((current) =>
                        current.map((item, itemIndex) =>
                          itemIndex === index
                            ? { ...item, account_id: event.target.value }
                            : item,
                        ),
                      )
                    }
                  >
                    <option value="">Select account</option>
                    {accounts.map((account) => (
                      <option key={account.id} value={account.id}>
                        {account.name}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  Instrument
                  <select
                    required
                    value={line.instrument_id}
                    onChange={(event) =>
                      setDraftLines((current) =>
                        current.map((item, itemIndex) =>
                          itemIndex === index
                            ? { ...item, instrument_id: event.target.value }
                            : item,
                        ),
                      )
                    }
                  >
                    <option value="">Select instrument</option>
                    {instruments.map((instrument) => (
                      <option key={instrument.id} value={instrument.id}>
                        {instrument.canonical_symbol}
                      </option>
                    ))}
                  </select>
                </label>
                <label>
                  Quantity
                  <input
                    inputMode="decimal"
                    required
                    value={line.quantity}
                    onChange={(event) =>
                      setDraftLines((current) =>
                        current.map((item, itemIndex) =>
                          itemIndex === index
                            ? { ...item, quantity: event.target.value }
                            : item,
                        ),
                      )
                    }
                    placeholder="0.00000000"
                  />
                </label>
                {draftLines.length > 1 && (
                  <button
                    type="button"
                    onClick={() =>
                      setDraftLines((current) =>
                        current.filter((_, itemIndex) => itemIndex !== index),
                      )
                    }
                  >
                    Remove position {index + 1}
                  </button>
                )}
              </div>
            ))}
            <button
              type="button"
              onClick={() =>
                setDraftLines((current) => [
                  ...current,
                  {
                    account_id: accounts[0]?.id || "",
                    instrument_id: "",
                    quantity: "",
                  },
                ])
              }
            >
              Add position
            </button>
            <button disabled={!!busy || !portfolioId} type="submit">
              {busy === "snapshot" ? "Saving…" : "Create snapshot"}
            </button>
          </form>
          <div className="portfolio-import">
            <h3>Import position CSV</h3>
            <label>
              CSV file{" "}
              <input
                type="file"
                accept=".csv,text/csv"
                onChange={(event) => {
                  setFile(event.target.files?.[0] || null);
                  setPreviewLock(null);
                }}
              />
            </label>
            <div className="portfolio-actions">
              <button
                type="button"
                disabled={!!busy || !file || !portfolioId}
                onClick={previewCsv}
              >
                Preview CSV
              </button>
              <button
                type="button"
                disabled={
                  !!busy ||
                  !previewLock?.preview.valid ||
                  !previewLock.preview.token
                }
                onClick={commitCsv}
              >
                Commit previewed CSV
              </button>
            </div>
            {previewLock && (
              <div className="portfolio-preview" aria-label="CSV preview">
                <strong>
                  {previewLock.preview.row_count} rows ·{" "}
                  {previewLock.preview.valid ? "valid" : "invalid"}
                </strong>
                <code>SHA-256 {previewLock.hash}</code>
                {previewLock.preview.diagnostics.map((diagnostic, index) => (
                  <p key={index}>
                    {diagnostic.row ? `Row ${diagnostic.row}: ` : ""}
                    {diagnostic.message}
                  </p>
                ))}
              </div>
            )}
          </div>
        </section>
        <section
          aria-labelledby="valuation-heading"
          className="portfolio-section"
        >
          <h2 id="valuation-heading">Valuation</h2>
          <p>
            Server-selected price and FX evidence remains attached to each line.
          </p>
          <form onSubmit={createValuation} className="portfolio-form">
            <label>
              Snapshot{" "}
              <select
                required
                value={snapshotId}
                onChange={(event) => setSnapshotId(event.target.value)}
              >
                <option value="">Select snapshot</option>
                {snapshots.map((snapshot) => (
                  <option key={snapshot.id} value={snapshot.id}>
                    {snapshot.captured_at} · {snapshot.id.slice(0, 8)}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Knowledge mode{" "}
              <select
                value={knowledgeMode}
                onChange={(event) =>
                  setKnowledgeMode(
                    event.target.value as "system_as_of" | "source_as_of",
                  )
                }
              >
                <option value="system_as_of">System as-of</option>
                <option value="source_as_of">Source as-of</option>
              </select>
            </label>
            <label>
              Cutoff{" "}
              <input
                type="datetime-local"
                required
                value={cutoff}
                onChange={(event) => setCutoff(event.target.value)}
              />
            </label>
            <label>
              Known at{" "}
              <input
                type="datetime-local"
                required
                value={knownAt}
                onChange={(event) => setKnownAt(event.target.value)}
              />
            </label>
            <label>
              Maximum price age (seconds){" "}
              <input
                type="number"
                min="0"
                required
                value={priceMaxAge}
                onChange={(event) => setPriceMaxAge(event.target.value)}
              />
            </label>
            <label>
              Maximum FX age (seconds){" "}
              <input
                type="number"
                min="0"
                required
                value={fxMaxAge}
                onChange={(event) => setFxMaxAge(event.target.value)}
              />
            </label>
            <button disabled={!!busy || !snapshotId} type="submit">
              {busy === "valuation" ? "Valuing…" : "Record valuation"}
            </button>
          </form>
        </section>
      </div>
      {valuation && (
        <section
          aria-labelledby="result-heading"
          className="portfolio-section portfolio-result"
        >
          <div className="portfolio-result__head">
            <div>
              <h2 id="result-heading">Valuation result</h2>
              <p>
                Cutoff {valuation.cutoff} ·{" "}
                {valuation.knowledge_mode.replaceAll("_", " ")} · {valuation.id}
              </p>
            </div>
            <strong
              className={`portfolio-state portfolio-state--${valuation.state}`}
            >
              {valuation.state}
            </strong>
          </div>
          {valuation.state !== "valid" && (
            <p role="alert" className="portfolio-warning">
              Incomplete valuation. Totals below are not a complete portfolio
              NAV; review blocked and degraded lines.
            </p>
          )}
          <label>
            Display currency{" "}
            <select
              value={currency}
              onChange={(event) =>
                setCurrency(event.target.value as "native" | "TRY" | "USD")
              }
            >
              <option value="native">Native</option>
              <option value="TRY">TRY</option>
              <option value="USD">USD</option>
            </select>
          </label>
          {valuation.state === "valid" && currency !== "native" && (
            <p className="portfolio-total">
              Complete NAV:{" "}
              <strong>
                {valuation.totals[currency.toLowerCase() as "try" | "usd"] ??
                  "Unavailable"}{" "}
                {currency}
              </strong>
            </p>
          )}
          <div className="portfolio-lines">
            {valuation.lines.map((line) => (
              <article key={line.snapshot_line_id} className="portfolio-line">
                <div>
                  <strong>{line.instrument_id}</strong>
                  <span
                    className={`portfolio-state portfolio-state--${line.state}`}
                  >
                    {line.state}
                  </span>
                </div>
                <p>{displayLine(line, currency)}</p>
                <details>
                  <summary>Price and FX evidence</summary>
                  <p>
                    <strong>Selected price:</strong>{" "}
                    {line.price_method === "identity"
                      ? "Identity"
                      : line.price_revision_id || "Unavailable"}
                    {line.price_quote_unit ? ` · ${line.price_quote_unit}` : ""}
                  </p>
                  <FxPath label="TRY path" path={line.try_fx_path} />
                  <FxPath label="USD path" path={line.usd_fx_path} />
                  {line.reason_codes.map((reason) => (
                    <p key={reason.code}>
                      {reason.code}: {reason.message}
                    </p>
                  ))}
                </details>
              </article>
            ))}
          </div>
        </section>
      )}
      <section
        aria-labelledby="reconciliation-heading"
        className="portfolio-section portfolio-reconciliation"
      >
        <h2 id="reconciliation-heading">Reconciliation checkpoint</h2>
        <p>
          Compare a valid account valuation with a manually entered broker or
          statement total. No automatic correction is made.
        </p>
        <form
          onSubmit={createReconciliation}
          className="portfolio-form portfolio-form--wide"
        >
          <label>
            Account{" "}
            <select
              value={reconcileAccountId}
              onChange={(event) => setReconcileAccountId(event.target.value)}
            >
              <option value="">Select account</option>
              {accounts.map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name}
                </option>
              ))}
            </select>
          </label>
          <label>
            Source label{" "}
            <input
              required
              value={sourceLabel}
              onChange={(event) => setSourceLabel(event.target.value)}
              placeholder="Broker statement"
            />
          </label>
          <label>
            Reconciliation currency
            <select
              value={reconcileCurrency}
              onChange={(event) =>
                setReconcileCurrency(event.target.value as "TRY" | "USD")
              }
            >
              <option value="TRY">TRY</option>
              <option value="USD">USD</option>
            </select>
          </label>
          <label>
            External NAV{" "}
            <input
              required
              inputMode="decimal"
              value={externalNav}
              onChange={(event) => setExternalNav(event.target.value)}
            />
          </label>
          <button
            type="submit"
            disabled={!!busy || valuation?.state !== "valid"}
          >
            {busy === "reconciliation" ? "Recording…" : "Record checkpoint"}
          </button>
        </form>
        {reconciliation && (
          <dl className="portfolio-comparison">
            <div>
              <dt>State</dt>
              <dd>{reconciliation.state}</dd>
            </div>
            <div>
              <dt>Cutoff</dt>
              <dd>{reconciliation.cutoff}</dd>
            </div>
            <div>
              <dt>Valuation NAV</dt>
              <dd>
                {reconciliation.valuation_nav} {reconciliation.currency}
              </dd>
            </div>
            <div>
              <dt>External NAV</dt>
              <dd>
                {reconciliation.external_nav} {reconciliation.currency}
              </dd>
            </div>
            <div>
              <dt>Absolute difference</dt>
              <dd>
                {reconciliation.absolute_difference} {reconciliation.currency}
              </dd>
            </div>
            <div>
              <dt>Relative difference</dt>
              <dd>{reconciliation.relative_difference ?? "Undefined"}</dd>
            </div>
            <div>
              <dt>Effective tolerance</dt>
              <dd>
                {reconciliation.effective_tolerance} {reconciliation.currency} ·
                version {reconciliation.tolerance_version}
              </dd>
            </div>
            <div>
              <dt>Line checks</dt>
              <dd>{reconciliation.line_check_state}</dd>
            </div>
          </dl>
        )}
      </section>
    </div>
  );
}
