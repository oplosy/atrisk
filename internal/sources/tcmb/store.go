package tcmb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oplosy/atrisk/internal/ingestion"
)

// Store persists TCMB records into the existing append-only core tables. It
// does not own source identity, migrations, or credentials.
type Store struct{ Pool *pgxpool.Pool }

func (s Store) SaveCheckpoint(ctx context.Context, runID string, checkpoint ObservationCheckpoint) error {
	if s.Pool == nil {
		return errors.New("TCMB EVDS database pool is required")
	}
	if runID == "" {
		return errors.New("TCMB EVDS ingestion run ID is required")
	}
	if checkpoint.RequestFingerprint == "" {
		return ErrCheckpointRequestMismatch
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("encode TCMB EVDS checkpoint: %w", err)
	}
	commandTag, err := s.Pool.Exec(ctx, `UPDATE ingestion_runs SET coverage = coverage || $2::jsonb WHERE id = $1::uuid AND status = 'running'`, runID, data)
	if err != nil {
		return fmt.Errorf("save TCMB EVDS checkpoint: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("TCMB EVDS ingestion run %q is not running", runID)
	}
	return nil
}

func (s Store) LoadCheckpoint(ctx context.Context, runID string) (ObservationCheckpoint, error) {
	if s.Pool == nil {
		return ObservationCheckpoint{}, errors.New("TCMB EVDS database pool is required")
	}
	if runID == "" {
		return ObservationCheckpoint{}, errors.New("TCMB EVDS ingestion run ID is required")
	}
	var data []byte
	if err := s.Pool.QueryRow(ctx, `SELECT coverage FROM ingestion_runs WHERE id = $1::uuid`, runID).Scan(&data); err != nil {
		return ObservationCheckpoint{}, fmt.Errorf("load TCMB EVDS checkpoint: %w", err)
	}
	var checkpoint ObservationCheckpoint
	if len(data) == 0 || string(data) == "{}" {
		return checkpoint, ErrNoCheckpoint
	}
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return ObservationCheckpoint{}, fmt.Errorf("decode TCMB EVDS checkpoint: %w", err)
	}
	if checkpoint.RequestFingerprint == "" {
		return ObservationCheckpoint{}, ErrCheckpointRequestMismatch
	}
	return checkpoint, nil
}

func (s Store) PersistRecords(ctx context.Context, seriesID string, records []ingestion.NormalizedRecord) (int64, error) {
	if s.Pool == nil {
		return 0, errors.New("TCMB EVDS database pool is required")
	}
	if seriesID == "" {
		return 0, errors.New("TCMB EVDS series ID is required")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin TCMB EVDS observation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sourceCode, err := seriesSourceCode(ctx, tx, seriesID)
	if err != nil {
		return 0, err
	}
	var inserted int64
	for index, normalized := range records {
		decoded, decodeErr := DecodeObservationRecord(normalized)
		if decodeErr != nil {
			return 0, fmt.Errorf("decode TCMB EVDS observation %d: %w", index, decodeErr)
		}
		if decoded.SeriesCode != sourceCode {
			return 0, fmt.Errorf("TCMB EVDS observation %d belongs to series %q, want %q", index, decoded.SeriesCode, sourceCode)
		}
		rows, err := insertObservation(ctx, tx, seriesID, decoded)
		if err != nil {
			return 0, fmt.Errorf("insert TCMB EVDS observation %d: %w", index, err)
		}
		inserted += rows
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit TCMB EVDS observations: %w", err)
	}
	return inserted, nil
}

func insertObservation(ctx context.Context, tx pgx.Tx, seriesID string, record ObservationRecord) (int64, error) {
	rawObjectID, err := rawObjectID(ctx, tx, record.RawObject.ContentSHA256)
	if err != nil {
		return 0, err
	}
	quality, err := json.Marshal(record.QualityFlags)
	if err != nil {
		return 0, fmt.Errorf("encode quality flags: %w", err)
	}
	var value, valueText any
	if record.Value != nil {
		value = *record.Value
	}
	if record.ValueText != nil {
		valueText = *record.ValueText
	}
	var sourceKnownAt any
	if record.SourceKnownAt != nil {
		sourceKnownAt = *record.SourceKnownAt
	}
	var rows int64
	err = tx.QueryRow(ctx, `
INSERT INTO observation_revisions (series_id, observation_time, value, value_text, source_known_at, knowledge_time_basis, raw_object_id, quality_flags)
VALUES ($1, $2, $3, $4, $5, $6, $7::uuid, $8)
ON CONFLICT (series_id, observation_time, value, value_text, source_known_at, raw_object_id) DO NOTHING
RETURNING 1`, seriesID, record.ObservationTime, value, valueText, sourceKnownAt, record.KnowledgeTimeBasis, rawObjectID, quality).Scan(&rows)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return rows, err
}

// PersistFXRecords writes a TCMB series as an explicit currency pair. The
// caller supplies the pair because EVDS series codes do not provide a
// canonical base/quote contract for every FX series. Missing source periods
// are retained in observation_revisions under seriesID; fx_quote_revisions
// remains numeric-only because its rate is NOT NULL.
func (s Store) PersistFXRecords(ctx context.Context, baseCurrency, quoteCurrency, seriesID string, records []ingestion.NormalizedRecord) (int64, error) {
	if s.Pool == nil {
		return 0, errors.New("TCMB EVDS database pool is required")
	}
	if len(baseCurrency) != 3 || len(quoteCurrency) != 3 || baseCurrency == quoteCurrency {
		return 0, errors.New("TCMB EVDS FX currencies must be distinct ISO 4217 codes")
	}
	if seriesID == "" {
		return 0, errors.New("TCMB EVDS FX series ID is required")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin TCMB EVDS FX transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	sourceCode, err := seriesSourceCode(ctx, tx, seriesID)
	if err != nil {
		return 0, err
	}
	for index, normalized := range records {
		decoded, decodeErr := DecodeObservationRecord(normalized)
		if decodeErr != nil {
			return 0, fmt.Errorf("decode TCMB EVDS FX observation %d: %w", index, decodeErr)
		}
		if decoded.SeriesCode != sourceCode {
			return 0, fmt.Errorf("TCMB EVDS FX observation %d belongs to series %q, want %q", index, decoded.SeriesCode, sourceCode)
		}
	}
	var inserted int64
	for index, normalized := range records {
		decoded, decodeErr := DecodeObservationRecord(normalized)
		if decodeErr != nil {
			return 0, fmt.Errorf("decode TCMB EVDS FX observation %d: %w", index, decodeErr)
		}
		if decoded.Value == nil {
			if decoded.QualityFlags == nil {
				decoded.QualityFlags = map[string]any{}
			}
			decoded.QualityFlags["fx_missing_persisted"] = true
			decoded.QualityFlags["fx_persistence_target"] = "observation_revisions"
			rows, err := insertObservation(ctx, tx, seriesID, decoded)
			if err != nil {
				return 0, fmt.Errorf("insert TCMB EVDS missing FX observation %d: %w", index, err)
			}
			inserted += rows
			continue
		}
		rawID, err := rawObjectID(ctx, tx, decoded.RawObject.ContentSHA256)
		if err != nil {
			return 0, err
		}
		quality, err := json.Marshal(decoded.QualityFlags)
		if err != nil {
			return 0, err
		}
		var sourceKnownAt any
		if decoded.SourceKnownAt != nil {
			sourceKnownAt = *decoded.SourceKnownAt
		}
		var rows int64
		err = tx.QueryRow(ctx, `
INSERT INTO fx_quote_revisions (base_currency, quote_currency, observation_time, rate, source_known_at, knowledge_time_basis, raw_object_id, quality_flags)
VALUES ($1, $2, $3, $4, $5, $6, $7::uuid, $8)
ON CONFLICT (base_currency, quote_currency, observation_time, rate, source_known_at, raw_object_id) DO NOTHING
RETURNING 1`, strings.ToUpper(baseCurrency), strings.ToUpper(quoteCurrency), decoded.ObservationTime, *decoded.Value, sourceKnownAt, decoded.KnowledgeTimeBasis, rawID, quality).Scan(&rows)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("insert TCMB EVDS FX observation %d: %w", index, err)
		}
		inserted += rows
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit TCMB EVDS FX observations: %w", err)
	}
	return inserted, nil
}

func rawObjectID(ctx context.Context, tx pgx.Tx, digest string) (string, error) {
	var id string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM raw_objects WHERE content_sha256 = $1`, digest).Scan(&id); err != nil {
		return "", fmt.Errorf("find TCMB EVDS raw object %q: %w", digest, err)
	}
	return id, nil
}

func seriesSourceCode(ctx context.Context, tx pgx.Tx, seriesID string) (string, error) {
	var sourceCode string
	if err := tx.QueryRow(ctx, `SELECT source_code FROM series WHERE id = $1::uuid`, seriesID).Scan(&sourceCode); err != nil {
		return "", fmt.Errorf("find TCMB EVDS series %q: %w", seriesID, err)
	}
	return sourceCode, nil
}
