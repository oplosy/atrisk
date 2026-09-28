-- AR-401 journal queries are kept here as the reviewable SQL boundary. The
-- application service uses the same statements with pgx transactions so the
-- draft-to-finalized transition and append-only event writes remain atomic.

-- name: GetDecision :one
SELECT id, account_id, thesis, alternatives, evidence_references,
       invalidation_conditions, horizon_start, horizon_end,
       risk_budget_amount, risk_budget_currency, risk_budget_measure,
       risk_budget_horizon, intended_action, tags, status, author,
       source_metadata, created_at, finalized_at
FROM decisions
WHERE id = $1;

-- name: ListDecisionTimeline :many
SELECT id, decision_id, kind, payload, author, source_metadata, created_at
FROM (
    SELECT id, id AS decision_id, 'decision' AS kind,
           jsonb_build_object('status', status, 'thesis', thesis) AS payload,
           author, source_metadata, created_at
    FROM decisions WHERE id = $1
    UNION ALL
    SELECT id, decision_id, 'review',
           jsonb_build_object('review', review, 'outcome', outcome),
           author, source_metadata, created_at
    FROM decision_reviews WHERE decision_id = $1
    UNION ALL
    SELECT id, decision_id, 'amendment',
           jsonb_build_object('summary', summary, 'changes', changes),
           author, source_metadata, created_at
    FROM decision_amendments WHERE decision_id = $1
) events
ORDER BY created_at, id;
