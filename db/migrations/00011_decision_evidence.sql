-- +goose Up
-- +goose StatementBegin
CREATE TABLE decision_evidence (
    decision_id UUID PRIMARY KEY REFERENCES decisions (id),
    manifest_bytes BYTEA NOT NULL,
    manifest_sha256 CHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT decision_evidence_hash CHECK (manifest_sha256 ~ '^[0-9a-f]{64}$'),
    CONSTRAINT decision_evidence_nonempty CHECK (octet_length(manifest_bytes) > 0)
);

CREATE TRIGGER decision_evidence_immutable
    BEFORE UPDATE OR DELETE ON decision_evidence
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE OR REPLACE FUNCTION guard_completed_risk_job()
RETURNS trigger LANGUAGE plpgsql AS $function$
BEGIN
    IF TG_OP = 'DELETE' AND OLD.state = 'succeeded' THEN
        RAISE EXCEPTION 'completed risk results are immutable' USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'UPDATE' AND OLD.state = 'succeeded' AND (
        NEW.state IS DISTINCT FROM OLD.state
        OR NEW.result IS DISTINCT FROM OLD.result
        OR NEW.result_hash IS DISTINCT FROM OLD.result_hash
        OR NEW.completed_at IS DISTINCT FROM OLD.completed_at
    ) THEN
        RAISE EXCEPTION 'completed risk results are immutable' USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$function$;

CREATE TRIGGER risk_jobs_completed_immutable
    BEFORE UPDATE OR DELETE ON risk_jobs
    FOR EACH ROW EXECUTE FUNCTION guard_completed_risk_job();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS decision_evidence_immutable ON decision_evidence;
DROP TABLE IF EXISTS decision_evidence;
DROP TRIGGER IF EXISTS risk_jobs_completed_immutable ON risk_jobs;
DROP FUNCTION IF EXISTS guard_completed_risk_job();
-- +goose StatementEnd
