import { useEffect, useMemo, useState, type FormEvent } from "react";

import {
  appendDecisionAmendment,
  appendDecisionReview,
  createDecision,
  finalizeDecision,
  getDecision,
  getDecisionEvidence,
  getDecisionTimeline,
  JournalApiError,
  type Decision,
  type DecisionEvidence,
  type DecisionRequest,
  type DecisionTimeline,
  type EvidenceReference,
  type InvalidationCondition,
} from "../../features/journal/journalApi";
import "./decisions.css";

const DRAFT_KEY = "atlasrisk.decision-journal.draft.v1";
const DECISION_KEY = "atlasrisk.decision-journal.current-id";
const REQUIRED_EVIDENCE = ["portfolio_snapshot", "valuation_run", "risk_run"];

interface JournalDraft {
  accountId: string;
  thesis: string;
  alternatives: string;
  evidence: EvidenceReference[];
  invalidation: InvalidationCondition[];
  horizonStart: string;
  horizonEnd: string;
  riskAmount: string;
  riskCurrency: string;
  riskMeasure: string;
  riskHorizon: string;
  intendedAction: string;
  tags: string;
  author: string;
}

interface ReviewDraft {
  review: string;
  outcome: string;
  author: string;
}
interface AmendmentDraft {
  summary: string;
  changes: string;
  author: string;
}

const localDateTime = (offsetDays = 0) => {
  const value = new Date(Date.now() + offsetDays * 86_400_000);
  value.setMinutes(value.getMinutes() - value.getTimezoneOffset());
  return value.toISOString().slice(0, 16);
};

const localDateTimeFromIso = (value: string) => {
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "";
  date.setMinutes(date.getMinutes() - date.getTimezoneOffset());
  return date.toISOString().slice(0, 16);
};

const initialDraft = (): JournalDraft => ({
  accountId: "",
  thesis: "",
  alternatives: "",
  evidence: [
    { kind: "portfolio_snapshot", reference: "", description: "" },
    { kind: "valuation_run", reference: "", description: "" },
    { kind: "risk_run", reference: "", description: "" },
  ],
  invalidation: [{ condition: "", metric: "", threshold: "" }],
  horizonStart: localDateTime(),
  horizonEnd: localDateTime(30),
  riskAmount: "",
  riskCurrency: "TRY",
  riskMeasure: "absolute loss",
  riskHorizon: "30 days",
  intendedAction: "",
  tags: "",
  author: "",
});

function readDraft(): { draft: JournalDraft; recovered: boolean } {
  const fallback = initialDraft();
  try {
    const raw = window.localStorage.getItem(DRAFT_KEY);
    if (!raw) return { draft: fallback, recovered: false };
    const parsed = JSON.parse(raw) as Partial<JournalDraft>;
    return {
      recovered: true,
      draft: {
        ...fallback,
        ...parsed,
        evidence: parsed.evidence?.length ? parsed.evidence : fallback.evidence,
        invalidation: parsed.invalidation?.length
          ? parsed.invalidation
          : fallback.invalidation,
      },
    };
  } catch {
    return { draft: fallback, recovered: false };
  }
}

function toRequest(draft: JournalDraft): DecisionRequest {
  return {
    account_id: draft.accountId.trim(),
    thesis: draft.thesis.trim(),
    alternatives: draft.alternatives
      .split("\n")
      .map((item) => item.trim())
      .filter(Boolean),
    evidence_references: draft.evidence
      .map((item) => ({
        kind: item.kind.trim(),
        reference: item.reference.trim(),
        ...(item.description?.trim()
          ? { description: item.description.trim() }
          : {}),
      }))
      .filter(
        (item) =>
          item.reference.length > 0 || REQUIRED_EVIDENCE.includes(item.kind),
      ),
    invalidation_conditions: draft.invalidation
      .map((item) => ({
        condition: item.condition.trim(),
        ...(item.metric?.trim() ? { metric: item.metric.trim() } : {}),
        ...(item.threshold?.trim() ? { threshold: item.threshold.trim() } : {}),
      }))
      .filter((item) => item.condition),
    horizon: {
      start: new Date(draft.horizonStart).toISOString(),
      end: new Date(draft.horizonEnd).toISOString(),
    },
    risk_budget: {
      amount: draft.riskAmount.trim(),
      currency: draft.riskCurrency.trim().toUpperCase(),
      measure: draft.riskMeasure.trim(),
      horizon: draft.riskHorizon.trim(),
    },
    intended_action: draft.intendedAction.trim(),
    tags: draft.tags
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean),
    author: draft.author.trim(),
    source_metadata: { client: "atlasrisk-web", capture: "decision-journal" },
  };
}

function validateDraft(draft: JournalDraft) {
  const request = toRequest(draft);
  if (!request.account_id) return "Account ID is required.";
  if (!request.thesis) return "Thesis is required.";
  if (!request.invalidation_conditions.length)
    return "Add at least one invalidation condition.";
  const missingEvidence = REQUIRED_EVIDENCE.filter(
    (kind) =>
      !request.evidence_references.some(
        (reference) => reference.kind === kind && reference.reference,
      ),
  );
  if (missingEvidence.length) {
    return `Add required evidence references before saving: ${missingEvidence
      .map(labelEvidenceKind)
      .join(", ")}.`;
  }
  if (!request.risk_budget.amount) return "Risk budget amount is required.";
  if (!request.intended_action) return "Intended action statement is required.";
  if (!request.author) return "Author is required.";
  if (
    !Number.isFinite(new Date(request.horizon.start).getTime()) ||
    !Number.isFinite(new Date(request.horizon.end).getTime())
  )
    return "Choose a valid decision horizon.";
  if (new Date(request.horizon.end) <= new Date(request.horizon.start))
    return "Decision horizon end must be after its start.";
  return "";
}

function labelEvidenceKind(kind: string) {
  return kind
    .replaceAll("_", " ")
    .replace(/\b\w/g, (letter) => letter.toUpperCase());
}
function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause);
}

function HistoricalEvent({
  kind,
  payload,
  author,
  createdAt,
}: {
  kind: string;
  payload: unknown;
  author: string;
  createdAt: string;
}) {
  return (
    <article className={`journal-event journal-event--${kind}`}>
      <div className="journal-event__marker" aria-hidden="true">
        {kind === "decision" ? "D" : kind === "review" ? "R" : "A"}
      </div>
      <div>
        <p className="journal-kicker">
          {kind === "decision"
            ? "Original decision"
            : kind === "review"
              ? "Later review"
              : "Linked amendment"}
        </p>
        <h3>
          {kind === "decision" ? "Historical context" : labelEvidenceKind(kind)}
        </h3>
        <p className="journal-event__meta">
          {author || "Unknown author"} · {new Date(createdAt).toLocaleString()}
        </p>
        <pre>{JSON.stringify(payload, null, 2)}</pre>
      </div>
    </article>
  );
}

function manifestEntries(manifest: Record<string, unknown>) {
  const entries = Object.entries(manifest);
  return entries.length
    ? entries.map(([key, value]) => (
        <li key={key}>
          <strong>{key.replaceAll("_", " ")}</strong>
          <code>
            {typeof value === "string" ? value : JSON.stringify(value)}
          </code>
        </li>
      ))
    : [<li key="empty">No manifest entries were returned.</li>];
}

export function DecisionJournalRoute() {
  const [draft, setDraft] = useState<JournalDraft>(() => readDraft().draft);
  const [recovered, setRecovered] = useState(() => readDraft().recovered);
  const [decisionID, setDecisionID] = useState(
    () => window.localStorage.getItem(DECISION_KEY) || "",
  );
  const [decision, setDecision] = useState<Decision | null>(null);
  const [evidence, setEvidence] = useState<DecisionEvidence | null>(null);
  const [timeline, setTimeline] = useState<DecisionTimeline | null>(null);
  const [lookupID, setLookupID] = useState(
    () => window.localStorage.getItem(DECISION_KEY) || "",
  );
  const [reviewDraft, setReviewDraft] = useState<ReviewDraft>({
    review: "",
    outcome: "",
    author: "",
  });
  const [amendmentDraft, setAmendmentDraft] = useState<AmendmentDraft>({
    summary: "",
    changes: "",
    author: "",
  });
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [integrityBlocked, setIntegrityBlocked] = useState(false);

  useEffect(() => {
    try {
      window.localStorage.setItem(DRAFT_KEY, JSON.stringify(draft));
    } catch {
      /* optional recovery */
    }
  }, [draft]);

  useEffect(() => {
    if (!decisionID) return;
    let active = true;
    setBusy("load");
    setError("");
    Promise.all([
      getDecision(decisionID),
      getDecisionTimeline(decisionID).catch(() => null),
    ])
      .then(([loadedDecision, loadedTimeline]) => {
        if (!active) return;
        setDecision(loadedDecision);
        setTimeline(loadedTimeline);
        setDraft((current) => ({
          ...current,
          accountId: loadedDecision.account_id,
          thesis: loadedDecision.thesis,
          alternatives: loadedDecision.alternatives.join("\n"),
          evidence: loadedDecision.evidence_references,
          invalidation: loadedDecision.invalidation_conditions,
          horizonStart: localDateTimeFromIso(loadedDecision.horizon.start),
          horizonEnd: localDateTimeFromIso(loadedDecision.horizon.end),
          riskAmount: loadedDecision.risk_budget.amount,
          riskCurrency: loadedDecision.risk_budget.currency,
          riskMeasure: loadedDecision.risk_budget.measure,
          riskHorizon: loadedDecision.risk_budget.horizon,
          intendedAction: loadedDecision.intended_action,
          tags: loadedDecision.tags.join(", "),
          author: loadedDecision.author,
        }));
        setRecovered(false);
        if (loadedDecision.status === "finalized")
          void getDecisionEvidence(decisionID)
            .then((loadedEvidence) => {
              if (active) setEvidence(loadedEvidence);
            })
            .catch((cause) => {
              if (active) setError(errorMessage(cause));
            });
      })
      .catch((cause) => {
        if (active) setError(errorMessage(cause));
      })
      .finally(() => {
        if (active) setBusy("");
      });
    return () => {
      active = false;
    };
  }, [decisionID]);

  const requiredEvidence = useMemo(
    () =>
      REQUIRED_EVIDENCE.map((kind) => ({
        kind,
        item: draft.evidence.find((reference) => reference.kind === kind),
      })),
    [draft.evidence],
  );
  const updateDraft = <K extends keyof JournalDraft>(
    key: K,
    value: JournalDraft[K],
  ) => setDraft((current) => ({ ...current, [key]: value }));
  const updateEvidence = (
    index: number,
    key: keyof EvidenceReference,
    value: string,
  ) =>
    setDraft((current) => ({
      ...current,
      evidence: current.evidence.map((item, itemIndex) =>
        itemIndex === index ? { ...item, [key]: value } : item,
      ),
    }));
  const updateInvalidation = (
    index: number,
    key: keyof InvalidationCondition,
    value: string,
  ) =>
    setDraft((current) => ({
      ...current,
      invalidation: current.invalidation.map((item, itemIndex) =>
        itemIndex === index ? { ...item, [key]: value } : item,
      ),
    }));

  const submitDraft = (event: FormEvent) => {
    event.preventDefault();
    if (decision) return;
    const validation = validateDraft(draft);
    if (validation) {
      setError(validation);
      return;
    }
    setBusy("create");
    setError("");
    setNotice("");
    void createDecision(toRequest(draft))
      .then((created) => {
        setDecision(created);
        setDecisionID(created.id);
        setLookupID(created.id);
        window.localStorage.setItem(DECISION_KEY, created.id);
        setNotice(
          `Draft ${created.id} saved. Review the seal preview before finalization.`,
        );
      })
      .catch((cause) => setError(errorMessage(cause)))
      .finally(() => setBusy(""));
  };

  const loadDecision = (event: FormEvent) => {
    event.preventDefault();
    const id = lookupID.trim();
    if (!id) {
      setError("Enter a decision ID to reconstruct its historical record.");
      return;
    }
    setDecisionID(id);
    window.localStorage.setItem(DECISION_KEY, id);
  };

  const finalize = () => {
    if (!decision || decision.status !== "draft" || integrityBlocked) return;
    const missing = requiredEvidence
      .filter(({ item }) => !item?.reference.trim())
      .map(({ kind }) => labelEvidenceKind(kind));
    if (missing.length) {
      setError(`Seal preview is incomplete. Add: ${missing.join(", ")}.`);
      return;
    }
    setBusy("finalize");
    setError("");
    setNotice("");
    void finalizeDecision(decision.id)
      .then(async (finalized) => {
        setDecision(finalized);
        const [sealed, reconstructed] = await Promise.all([
          getDecisionEvidence(finalized.id),
          getDecisionTimeline(finalized.id).catch(() => null),
        ]);
        setEvidence(sealed);
        setTimeline(reconstructed);
        setNotice(
          "Decision finalized. Its evidence is now immutable and reconstructable.",
        );
      })
      .catch((cause) => {
        const apiError = cause instanceof JournalApiError ? cause : undefined;
        if (
          apiError?.code === "EVIDENCE_INTEGRITY_FAILURE" ||
          apiError?.code === "EVIDENCE_INCOMPLETE"
        ) {
          setIntegrityBlocked(true);
          setError(
            "Finalization blocked: sealed evidence failed integrity validation. Latest data fallback is not permitted.",
          );
        } else setError(errorMessage(cause));
      })
      .finally(() => setBusy(""));
  };

  const appendReview = (event: FormEvent) => {
    event.preventDefault();
    if (!decision || decision.status !== "finalized") return;
    if (
      !reviewDraft.review.trim() ||
      !reviewDraft.outcome.trim() ||
      !reviewDraft.author.trim()
    ) {
      setError("Review, outcome, and author are required.");
      return;
    }
    setBusy("review");
    setError("");
    void appendDecisionReview(decision.id, {
      review: reviewDraft.review.trim(),
      outcome: reviewDraft.outcome.trim(),
      author: reviewDraft.author.trim(),
    })
      .then(async () => {
        setReviewDraft({ review: "", outcome: "", author: "" });
        setTimeline(await getDecisionTimeline(decision.id));
        setNotice("Review appended. The original decision remains unchanged.");
      })
      .catch((cause) => setError(errorMessage(cause)))
      .finally(() => setBusy(""));
  };

  const appendAmendment = (event: FormEvent) => {
    event.preventDefault();
    if (!decision || decision.status !== "finalized") return;
    let changes: Record<string, unknown>;
    try {
      changes = JSON.parse(amendmentDraft.changes) as Record<string, unknown>;
    } catch {
      setError("Amendment changes must be a valid JSON object.");
      return;
    }
    if (!amendmentDraft.summary.trim() || !amendmentDraft.author.trim()) {
      setError("Amendment summary and author are required.");
      return;
    }
    setBusy("amendment");
    setError("");
    void appendDecisionAmendment(decision.id, {
      summary: amendmentDraft.summary.trim(),
      changes,
      author: amendmentDraft.author.trim(),
    })
      .then(async () => {
        setAmendmentDraft({ summary: "", changes: "", author: "" });
        setTimeline(await getDecisionTimeline(decision.id));
        setNotice("Amendment appended as a linked historical event.");
      })
      .catch((cause) => setError(errorMessage(cause)))
      .finally(() => setBusy(""));
  };

  const clearDraft = () => {
    setDraft(initialDraft());
    setRecovered(false);
    window.localStorage.removeItem(DRAFT_KEY);
  };

  const draftLocked = decision !== null;

  return (
    <div className="decision-journal" data-testid="decision-journal">
      <div className="decision-journal__status" aria-live="polite">
        {error && (
          <p className="journal-alert journal-alert--error" role="alert">
            {error}
          </p>
        )}
        {notice && (
          <p className="journal-alert journal-alert--notice">{notice}</p>
        )}
      </div>
      {recovered && !decision && (
        <div className="journal-recovery" role="status">
          <strong>Recovered local draft.</strong>
          <span>Your unfinished decision is restored in this form.</span>
          <button
            className="journal-button journal-button--quiet"
            type="button"
            onClick={clearDraft}
          >
            Discard recovered draft
          </button>
        </div>
      )}

      <section
        className="journal-section journal-section--intro"
        aria-labelledby="journal-title"
      >
        <div>
          <p className="journal-kicker">Decision record</p>
          <h2 id="journal-title">Write down what was knowable.</h2>
          <p>
            Build a decision statement, inspect the immutable evidence seal,
            then append later reviews without rewriting history.
          </p>
        </div>
        <span className="journal-boundary">
          Journal statement · no execution
        </span>
      </section>

      <section className="journal-section" aria-labelledby="draft-heading">
        <div className="journal-section__heading">
          <div>
            <p className="journal-kicker">01 / Draft</p>
            <h2 id="draft-heading">Decision context</h2>
          </div>
          <span className="journal-status">
            {decision?.status ?? "local draft"}
          </span>
        </div>
        <form className="journal-form" onSubmit={submitDraft}>
          <label>
            Account ID
            <input
              value={draft.accountId}
              disabled={draftLocked}
              onChange={(event) => updateDraft("accountId", event.target.value)}
              placeholder="UUID"
              required
            />
          </label>
          <label>
            Author
            <input
              value={draft.author}
              disabled={draftLocked}
              onChange={(event) => updateDraft("author", event.target.value)}
              required
            />
          </label>
          <label className="journal-form__wide">
            Thesis
            <textarea
              value={draft.thesis}
              disabled={draftLocked}
              onChange={(event) => updateDraft("thesis", event.target.value)}
              rows={4}
              required
              placeholder="What is the decision thesis?"
            />
          </label>
          <label className="journal-form__wide">
            Alternatives (one per line)
            <textarea
              value={draft.alternatives}
              disabled={draftLocked}
              onChange={(event) =>
                updateDraft("alternatives", event.target.value)
              }
              rows={3}
              placeholder="What alternatives were considered?"
            />
          </label>
          <fieldset className="journal-fieldset journal-form__wide">
            <legend>Invalidation conditions</legend>
            {draft.invalidation.map((condition, index) => (
              <div className="journal-row" key={index}>
                <label>
                  Condition
                  <input
                    value={condition.condition}
                    disabled={draftLocked}
                    onChange={(event) =>
                      updateInvalidation(index, "condition", event.target.value)
                    }
                    required
                  />
                </label>
                <label>
                  Metric
                  <input
                    value={condition.metric}
                    disabled={draftLocked}
                    onChange={(event) =>
                      updateInvalidation(index, "metric", event.target.value)
                    }
                  />
                </label>
                <label>
                  Threshold
                  <input
                    value={condition.threshold}
                    disabled={draftLocked}
                    onChange={(event) =>
                      updateInvalidation(index, "threshold", event.target.value)
                    }
                  />
                </label>
                {draft.invalidation.length > 1 && (
                  <button
                    className="journal-button journal-button--quiet"
                    type="button"
                    disabled={draftLocked}
                    onClick={() =>
                      updateDraft(
                        "invalidation",
                        draft.invalidation.filter(
                          (_, itemIndex) => itemIndex !== index,
                        ),
                      )
                    }
                  >
                    Remove
                  </button>
                )}
              </div>
            ))}
            <button
              className="journal-button journal-button--quiet"
              type="button"
              disabled={draftLocked}
              onClick={() =>
                updateDraft("invalidation", [
                  ...draft.invalidation,
                  { condition: "", metric: "", threshold: "" },
                ])
              }
            >
              Add condition
            </button>
          </fieldset>
          <div className="journal-row journal-form__wide">
            <label>
              Horizon start
              <input
                type="datetime-local"
                value={draft.horizonStart}
                disabled={draftLocked}
                onChange={(event) =>
                  updateDraft("horizonStart", event.target.value)
                }
                required
              />
            </label>
            <label>
              Horizon end
              <input
                type="datetime-local"
                value={draft.horizonEnd}
                disabled={draftLocked}
                onChange={(event) =>
                  updateDraft("horizonEnd", event.target.value)
                }
                required
              />
            </label>
          </div>
          <div className="journal-row journal-form__wide">
            <label>
              Risk budget amount
              <input
                inputMode="decimal"
                value={draft.riskAmount}
                disabled={draftLocked}
                onChange={(event) =>
                  updateDraft("riskAmount", event.target.value)
                }
                required
              />
            </label>
            <label>
              Currency
              <input
                value={draft.riskCurrency}
                disabled={draftLocked}
                onChange={(event) =>
                  updateDraft("riskCurrency", event.target.value)
                }
                required
              />
            </label>
            <label>
              Measure
              <input
                value={draft.riskMeasure}
                disabled={draftLocked}
                onChange={(event) =>
                  updateDraft("riskMeasure", event.target.value)
                }
                required
              />
            </label>
            <label>
              Budget horizon
              <input
                value={draft.riskHorizon}
                disabled={draftLocked}
                onChange={(event) =>
                  updateDraft("riskHorizon", event.target.value)
                }
                required
              />
            </label>
          </div>
          <label className="journal-form__wide">
            Intended action statement
            <textarea
              aria-label="Intended action statement"
              value={draft.intendedAction}
              disabled={draftLocked}
              onChange={(event) =>
                updateDraft("intendedAction", event.target.value)
              }
              rows={3}
              required
              placeholder="Describe the intended action as a journal statement; it will never execute."
            />
            <span className="journal-help">
              This is a journal statement, never an executable control.
            </span>
          </label>
          <label>
            Tags (comma separated)
            <input
              value={draft.tags}
              disabled={draftLocked}
              onChange={(event) => updateDraft("tags", event.target.value)}
            />
          </label>
          <div className="journal-form__actions journal-form__wide">
            <button
              className="journal-button"
              disabled={busy !== "" || !!decision}
              type="submit"
            >
              {busy === "create"
                ? "Saving…"
                : decision
                  ? "Draft saved"
                  : "Save draft"}
            </button>
            {decision && (
              <span className="journal-help">
                Draft is saved as {decision.id}. Evidence sealing is a separate
                explicit step.
              </span>
            )}
          </div>
        </form>
      </section>

      <section className="journal-section" aria-labelledby="evidence-heading">
        <div className="journal-section__heading">
          <div>
            <p className="journal-kicker">02 / Evidence seal</p>
            <h2 id="evidence-heading">Preview immutable references</h2>
          </div>
          <span className="journal-status">
            {decision?.status === "finalized" ? "sealed" : "pending"}
          </span>
        </div>
        <p className="journal-copy">
          Finalization binds the exact portfolio snapshot, valuation result, and
          completed risk result. A missing or broken reference blocks the
          operation.
        </p>
        <div className="journal-evidence-list">
          {draft.evidence.map((item, index) => (
            <div className="journal-evidence-item" key={index}>
              <label>
                Evidence type
                <select
                  value={item.kind}
                  onChange={(event) =>
                    updateEvidence(index, "kind", event.target.value)
                  }
                  disabled={draftLocked}
                >
                  <option value="portfolio_snapshot">Portfolio snapshot</option>
                  <option value="valuation_run">Valuation result</option>
                  <option value="risk_run">Completed risk result</option>
                  <option value="observation_revision">
                    Observation revision
                  </option>
                  <option value="price_revision">Price revision</option>
                  <option value="fx_quote_revision">FX quote revision</option>
                  <option value="raw_object">Raw source object</option>
                </select>
              </label>
              <label>
                Reference ID
                <input
                  value={item.reference}
                  onChange={(event) =>
                    updateEvidence(index, "reference", event.target.value)
                  }
                  disabled={draftLocked}
                  required={REQUIRED_EVIDENCE.includes(item.kind)}
                />
              </label>
              <label>
                Description
                <input
                  value={item.description ?? ""}
                  onChange={(event) =>
                    updateEvidence(index, "description", event.target.value)
                  }
                  disabled={draftLocked}
                />
              </label>
              {draft.evidence.length > 3 && (
                <button
                  className="journal-button journal-button--quiet"
                  type="button"
                  disabled={draftLocked}
                  onClick={() =>
                    updateDraft(
                      "evidence",
                      draft.evidence.filter(
                        (_, itemIndex) => itemIndex !== index,
                      ),
                    )
                  }
                >
                  Remove
                </button>
              )}
            </div>
          ))}
        </div>
        <div className="journal-form__actions">
          <button
            className="journal-button journal-button--quiet"
            type="button"
            disabled={draftLocked}
            onClick={() =>
              updateDraft("evidence", [
                ...draft.evidence,
                { kind: "raw_object", reference: "", description: "" },
              ])
            }
          >
            Add evidence reference
          </button>
        </div>
        <div className="journal-seal-preview" aria-label="Finalization preview">
          <h3>Finalization preview</h3>
          <ul>
            {requiredEvidence.map(({ kind, item }) => (
              <li key={kind}>
                <strong>{labelEvidenceKind(kind)}</strong>
                <code>
                  {item?.reference || "Missing — finalization blocked"}
                </code>
              </li>
            ))}
          </ul>
          <p>
            Every listed ID is sent to the server for atomic validation. The UI
            does not replace broken historical evidence with latest data.
          </p>
          {integrityBlocked && (
            <p className="journal-alert journal-alert--error" role="alert">
              Evidence integrity failure is blocking finalization.
            </p>
          )}
          <button
            className="journal-button"
            type="button"
            disabled={
              !decision ||
              decision.status !== "draft" ||
              busy !== "" ||
              integrityBlocked
            }
            onClick={finalize}
          >
            {busy === "finalize" ? "Sealing…" : "Finalize decision"}
          </button>
        </div>
        {evidence && (
          <div className="journal-sealed" aria-label="Sealed evidence manifest">
            <h3>Sealed evidence reconstruction</h3>
            <p>
              Manifest SHA-256 <code>{evidence.sha256}</code>
            </p>
            <ul>{manifestEntries(evidence.manifest)}</ul>
          </div>
        )}
      </section>

      <section
        className="journal-section"
        aria-labelledby="reconstruct-heading"
      >
        <div className="journal-section__heading">
          <div>
            <p className="journal-kicker">03 / Historical reconstruction</p>
            <h2 id="reconstruct-heading">Open a recorded decision</h2>
          </div>
        </div>
        <form className="journal-lookup" onSubmit={loadDecision}>
          <label>
            Decision ID
            <input
              value={lookupID}
              onChange={(event) => setLookupID(event.target.value)}
              placeholder="UUID"
            />
          </label>
          <button
            className="journal-button"
            disabled={busy !== ""}
            type="submit"
          >
            {busy === "load" ? "Loading…" : "Reconstruct"}
          </button>
        </form>
        {timeline && (
          <div className="journal-timeline" aria-label="Decision history">
            {timeline.events.map((event) => (
              <HistoricalEvent
                key={event.id}
                kind={event.kind}
                payload={event.payload}
                author={event.author}
                createdAt={event.created_at}
              />
            ))}
          </div>
        )}
        {decision && (
          <div
            className="journal-original"
            aria-label="Original decision context"
          >
            <p className="journal-kicker">Original decision</p>
            <h3>{decision.thesis}</h3>
            <p>{decision.intended_action}</p>
            <dl>
              <div>
                <dt>Status</dt>
                <dd>{decision.status}</dd>
              </div>
              <div>
                <dt>Created</dt>
                <dd>{new Date(decision.created_at).toLocaleString()}</dd>
              </div>
              <div>
                <dt>Evidence references</dt>
                <dd>{decision.evidence_references.length}</dd>
              </div>
            </dl>
          </div>
        )}
      </section>

      {decision?.status === "finalized" && (
        <section className="journal-section" aria-labelledby="review-heading">
          <div className="journal-section__heading">
            <div>
              <p className="journal-kicker">04 / Append-only review</p>
              <h2 id="review-heading">Review without rewriting</h2>
            </div>
            <span className="journal-status">historical append</span>
          </div>
          <form className="journal-form" onSubmit={appendReview}>
            <label className="journal-form__wide">
              Review
              <textarea
                value={reviewDraft.review}
                onChange={(event) =>
                  setReviewDraft((current) => ({
                    ...current,
                    review: event.target.value,
                  }))
                }
                rows={4}
                required
              />
            </label>
            <label>
              Outcome
              <input
                value={reviewDraft.outcome}
                onChange={(event) =>
                  setReviewDraft((current) => ({
                    ...current,
                    outcome: event.target.value,
                  }))
                }
                required
              />
            </label>
            <label>
              Author
              <input
                value={reviewDraft.author}
                onChange={(event) =>
                  setReviewDraft((current) => ({
                    ...current,
                    author: event.target.value,
                  }))
                }
                required
              />
            </label>
            <button
              className="journal-button journal-form__wide"
              disabled={busy !== ""}
              type="submit"
            >
              {busy === "review" ? "Appending…" : "Append review"}
            </button>
          </form>
          <form
            className="journal-form journal-amendment"
            onSubmit={appendAmendment}
          >
            <h3 className="journal-form__wide">Linked amendment</h3>
            <label className="journal-form__wide">
              Summary
              <input
                value={amendmentDraft.summary}
                onChange={(event) =>
                  setAmendmentDraft((current) => ({
                    ...current,
                    summary: event.target.value,
                  }))
                }
                required
              />
            </label>
            <label className="journal-form__wide">
              Changes (JSON object)
              <textarea
                value={amendmentDraft.changes}
                onChange={(event) =>
                  setAmendmentDraft((current) => ({
                    ...current,
                    changes: event.target.value,
                  }))
                }
                rows={4}
                placeholder='{"field":"new context"}'
                required
              />
            </label>
            <label>
              Author
              <input
                value={amendmentDraft.author}
                onChange={(event) =>
                  setAmendmentDraft((current) => ({
                    ...current,
                    author: event.target.value,
                  }))
                }
                required
              />
            </label>
            <button
              className="journal-button journal-form__wide"
              disabled={busy !== ""}
              type="submit"
            >
              {busy === "amendment" ? "Appending…" : "Append amendment"}
            </button>
          </form>
        </section>
      )}
    </div>
  );
}

export const DecisionsRoute = DecisionJournalRoute;
