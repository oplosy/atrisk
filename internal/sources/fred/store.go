package fred

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oplosy/atrisk/internal/ingestion"
)

// Store writes FRED normalized records into the existing append-only core
// table. It intentionally does not own migrations or source identity setup.
type Store struct {
	Pool *pgxpool.Pool
}

func (s Store) SaveCheckpoint(ctx context.Context, runID string, checkpoint ObservationCheckpoint) error {
	if s.Pool == nil {
		return errors.New("FRED database pool is required")
	}
	if runID == "" {
		return errors.New("FRED ingestion run ID is required")
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("encode FRED checkpoint: %w", err)
	}
	commandTag, err := s.Pool.Exec(ctx, `
UPDATE ingestion_runs
SET coverage = coverage || $2::jsonb
WHERE id = $1::uuid AND status = 'running'`, runID, data)
	if err != nil {
		return fmt.Errorf("save FRED checkpoint: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return fmt.Errorf("FRED ingestion run %q is not running", runID)
	}
	return nil
}

func (s Store) LoadCheckpoint(ctx context.Context, runID string) (ObservationCheckpoint, error) {
	if s.Pool == nil {
		return ObservationCheckpoint{}, errors.New("FRED database pool is required")
	}
	if runID == "" {
		return ObservationCheckpoint{}, errors.New("FRED ingestion run ID is required")
	}
	var data []byte
	if err := s.Pool.QueryRow(ctx, `SELECT coverage FROM ingestion_runs WHERE id = $1::uuid`, runID).Scan(&data); err != nil {
		return ObservationCheckpoint{}, fmt.Errorf("load FRED checkpoint: %w", err)
	}
	var checkpoint ObservationCheckpoint
	if len(data) == 0 || string(data) == "{}" {
		return checkpoint, ErrNoCheckpoint
	}
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return ObservationCheckpoint{}, fmt.Errorf("decode FRED checkpoint: %w", err)
	}
	return checkpoint, nil
}

// PersistRecords inserts every normalized observation in one transaction.
// PostgreSQL's identity constraint makes retries and duplicate fixture fetches
// idempotent while allowing changed values to coexist as revisions.
func (s Store) PersistRecords(ctx context.Context, seriesID string, records []ingestion.NormalizedRecord) (int64, error) {
	if s.Pool == nil {
		return 0, errors.New("FRED database pool is required")
	}
	if seriesID == "" {
		return 0, errors.New("FRED series ID is required")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin FRED observation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var inserted int64
	for index, normalized := range records {
		decoded, decodeErr := DecodeObservationRecord(normalized)
		if decodeErr != nil {
			return 0, fmt.Errorf("decode FRED observation %d: %w", index, decodeErr)
		}
		if decoded.SeriesID != seriesID {
			return 0, fmt.Errorf("FRED observation %d belongs to series %q, want %q", index, decoded.SeriesID, seriesID)
		}
		var rawObjectID string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM raw_objects WHERE content_sha256 = $1`, normalized.RawObjectSHA256).Scan(&rawObjectID); err != nil {
			return 0, fmt.Errorf("find FRED raw object %q: %w", normalized.RawObjectSHA256, err)
		}
		quality, err := json.Marshal(decoded.QualityFlags)
		if err != nil {
			return 0, fmt.Errorf("encode FRED quality flags: %w", err)
		}
		var value any
		if decoded.Value != nil {
			value = *decoded.Value
		}
		var valueText any
		if decoded.ValueText != nil {
			valueText = *decoded.ValueText
		}
		var sourceKnownAt any
		if decoded.SourceKnownAt != nil {
			sourceKnownAt = *decoded.SourceKnownAt
		}
		var rows int64
		if err := tx.QueryRow(ctx, `
INSERT INTO observation_revisions (
    series_id, observation_time, value, value_text, source_known_at,
    knowledge_time_basis, raw_object_id, quality_flags
)
VALUES ($1, $2, $3, $4, $5, $6, $7::uuid, $8)
ON CONFLICT (series_id, observation_time, value, value_text, source_known_at, raw_object_id)
DO NOTHING
RETURNING 1`, seriesID, decoded.ObservationTime, value, valueText, sourceKnownAt,
			decoded.KnowledgeTimeBasis, rawObjectID, quality).Scan(&rows); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return 0, fmt.Errorf("insert FRED observation %d: %w", index, err)
		}
		inserted += rows
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit FRED observations: %w", err)
	}
	return inserted, nil
}
