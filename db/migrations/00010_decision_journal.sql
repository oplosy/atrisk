-- +goose Up
-- +goose StatementBegin

CREATE TABLE decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL REFERENCES accounts (id),
    thesis TEXT NOT NULL,
    alternatives JSONB NOT NULL DEFAULT '[]'::jsonb,
    evidence_references JSONB NOT NULL DEFAULT '[]'::jsonb,
    invalidation_conditions JSONB NOT NULL,
    horizon_start TIMESTAMPTZ NOT NULL,
    horizon_end TIMESTAMPTZ NOT NULL,
    risk_budget_amount NUMERIC(38,18) NOT NULL,
    risk_budget_currency TEXT NOT NULL,
    risk_budget_measure TEXT NOT NULL,
    risk_budget_horizon TEXT NOT NULL,
    intended_action TEXT NOT NULL,
    tags TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'draft',
    author TEXT NOT NULL,
    source_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    finalized_at TIMESTAMPTZ,
    CONSTRAINT decisions_thesis_not_blank CHECK (length(btrim(thesis)) > 0),
    CONSTRAINT decisions_alternatives_array CHECK (jsonb_typeof(alternatives) = 'array'),
    CONSTRAINT decisions_evidence_references_array CHECK (jsonb_typeof(evidence_references) = 'array'),
    CONSTRAINT decisions_invalidation_conditions_required CHECK (
        jsonb_typeof(invalidation_conditions) = 'array'
        AND jsonb_array_length(invalidation_conditions) > 0
    ),
    CONSTRAINT decisions_horizon_order CHECK (horizon_end > horizon_start),
    CONSTRAINT decisions_risk_budget_finite CHECK (
        risk_budget_amount >= 0
        AND risk_budget_amount NOT IN ('NaN'::numeric, 'Infinity'::numeric, '-Infinity'::numeric)
    ),
    CONSTRAINT decisions_currency_not_blank CHECK (length(btrim(risk_budget_currency)) > 0),
    CONSTRAINT decisions_measure_not_blank CHECK (length(btrim(risk_budget_measure)) > 0),
    CONSTRAINT decisions_budget_horizon_not_blank CHECK (length(btrim(risk_budget_horizon)) > 0),
    CONSTRAINT decisions_action_not_blank CHECK (length(btrim(intended_action)) > 0),
    CONSTRAINT decisions_author_not_blank CHECK (length(btrim(author)) > 0),
    CONSTRAINT decisions_status CHECK (status IN ('draft', 'finalized')),
    CONSTRAINT decisions_finalized_at CHECK ((status = 'draft' AND finalized_at IS NULL) OR (status = 'finalized' AND finalized_at IS NOT NULL))
);

CREATE TABLE decision_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    decision_id UUID NOT NULL REFERENCES decisions (id),
    review TEXT NOT NULL,
    outcome TEXT NOT NULL,
    author TEXT NOT NULL,
    source_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT decision_reviews_review_not_blank CHECK (length(btrim(review)) > 0),
    CONSTRAINT decision_reviews_outcome_not_blank CHECK (length(btrim(outcome)) > 0),
    CONSTRAINT decision_reviews_author_not_blank CHECK (length(btrim(author)) > 0)
);

CREATE TABLE decision_amendments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    decision_id UUID NOT NULL REFERENCES decisions (id),
    summary TEXT NOT NULL,
    changes JSONB NOT NULL,
    author TEXT NOT NULL,
    source_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT decision_amendments_summary_not_blank CHECK (length(btrim(summary)) > 0),
    CONSTRAINT decision_amendments_changes_object CHECK (jsonb_typeof(changes) = 'object'),
    CONSTRAINT decision_amendments_author_not_blank CHECK (length(btrim(author)) > 0)
);

CREATE OR REPLACE FUNCTION guard_finalized_decision()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    IF OLD.status = 'finalized' THEN
        RAISE EXCEPTION 'finalized decisions are immutable' USING ERRCODE = '55000';
    END IF;
    IF NEW.id IS DISTINCT FROM OLD.id
       OR NEW.account_id IS DISTINCT FROM OLD.account_id
       OR NEW.thesis IS DISTINCT FROM OLD.thesis
       OR NEW.alternatives IS DISTINCT FROM OLD.alternatives
       OR NEW.evidence_references IS DISTINCT FROM OLD.evidence_references
       OR NEW.invalidation_conditions IS DISTINCT FROM OLD.invalidation_conditions
       OR NEW.horizon_start IS DISTINCT FROM OLD.horizon_start
       OR NEW.horizon_end IS DISTINCT FROM OLD.horizon_end
       OR NEW.risk_budget_amount IS DISTINCT FROM OLD.risk_budget_amount
       OR NEW.risk_budget_currency IS DISTINCT FROM OLD.risk_budget_currency
       OR NEW.risk_budget_measure IS DISTINCT FROM OLD.risk_budget_measure
       OR NEW.risk_budget_horizon IS DISTINCT FROM OLD.risk_budget_horizon
       OR NEW.intended_action IS DISTINCT FROM OLD.intended_action
       OR NEW.tags IS DISTINCT FROM OLD.tags
       OR NEW.author IS DISTINCT FROM OLD.author
       OR NEW.source_metadata IS DISTINCT FROM OLD.source_metadata
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'decision content is immutable during finalization' USING ERRCODE = '55000';
    END IF;
    IF NEW.status = 'draft' AND NEW.finalized_at IS NOT NULL THEN
        RAISE EXCEPTION 'draft decisions cannot have a finalized timestamp' USING ERRCODE = '23514';
    END IF;
    IF NEW.status = 'finalized' AND NEW.finalized_at IS NULL THEN
        RAISE EXCEPTION 'finalized decisions require a finalized timestamp' USING ERRCODE = '23514';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE TRIGGER decisions_finalized_guard
    BEFORE UPDATE OR DELETE ON decisions
    FOR EACH ROW EXECUTE FUNCTION guard_finalized_decision();

CREATE TRIGGER decision_reviews_immutable
    BEFORE UPDATE OR DELETE ON decision_reviews
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE TRIGGER decision_amendments_immutable
    BEFORE UPDATE OR DELETE ON decision_amendments
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE INDEX decisions_account_created_idx ON decisions (account_id, created_at DESC, id DESC);
CREATE INDEX decision_reviews_decision_created_idx ON decision_reviews (decision_id, created_at, id);
CREATE INDEX decision_amendments_decision_created_idx ON decision_amendments (decision_id, created_at, id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS decision_amendments_immutable ON decision_amendments;
DROP TRIGGER IF EXISTS decision_reviews_immutable ON decision_reviews;
DROP TRIGGER IF EXISTS decisions_finalized_guard ON decisions;
DROP FUNCTION IF EXISTS guard_finalized_decision();
DROP TABLE IF EXISTS decision_amendments;
DROP TABLE IF EXISTS decision_reviews;
DROP TABLE IF EXISTS decisions;
-- +goose StatementEnd
