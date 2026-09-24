-- +goose Up
-- +goose StatementBegin

-- A dataset belongs to exactly one source. This composite key lets ingestion
-- runs enforce that relationship when both identifiers are present.
ALTER TABLE datasets
    ADD CONSTRAINT datasets_source_id_id_key UNIQUE (source_id, id);

ALTER TABLE ingestion_runs
    ADD CONSTRAINT ingestion_runs_source_dataset_fk
    FOREIGN KEY (source_id, dataset_id)
    REFERENCES datasets (source_id, id);

-- Source metadata is historical input to every revision. Corrections create a
-- new source/dataset/series identity instead of rewriting the old description.
CREATE TRIGGER data_sources_immutable
    BEFORE UPDATE OR DELETE ON data_sources
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE TRIGGER datasets_immutable
    BEFORE UPDATE OR DELETE ON datasets
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

CREATE TRIGGER series_immutable
    BEFORE UPDATE OR DELETE ON series
    FOR EACH ROW EXECUTE FUNCTION prevent_immutable_row_mutation();

-- Instrument identity and descriptive metadata are immutable, while the
-- lifecycle status may transition between active/inactive/delisted.
CREATE OR REPLACE FUNCTION prevent_instrument_identity_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    IF TG_OP = 'DELETE'
       OR NEW.canonical_symbol IS DISTINCT FROM OLD.canonical_symbol
       OR NEW.instrument_type IS DISTINCT FROM OLD.instrument_type
       OR NEW.native_currency IS DISTINCT FROM OLD.native_currency
       OR NEW.external_ids IS DISTINCT FROM OLD.external_ids
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'instrument identity metadata is immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$function$;

CREATE TRIGGER instruments_identity_immutable
    BEFORE UPDATE OR DELETE ON instruments
    FOR EACH ROW EXECUTE FUNCTION prevent_instrument_identity_mutation();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS series_immutable ON series;
DROP TRIGGER IF EXISTS datasets_immutable ON datasets;
DROP TRIGGER IF EXISTS data_sources_immutable ON data_sources;
DROP TRIGGER IF EXISTS instruments_identity_immutable ON instruments;
DROP FUNCTION IF EXISTS prevent_instrument_identity_mutation();
ALTER TABLE ingestion_runs DROP CONSTRAINT IF EXISTS ingestion_runs_source_dataset_fk;
ALTER TABLE datasets DROP CONSTRAINT IF EXISTS datasets_source_id_id_key;
-- +goose StatementEnd
