package binance

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

// Store writes Binance metadata and price revisions into the existing
// append-only core tables. It does not own migrations or source identity.
type Store struct{ Pool *pgxpool.Pool }

func (s Store) SaveCheckpoint(ctx context.Context, runID string, checkpoint KlineCheckpoint) error {
	if s.Pool == nil {
		return errors.New("Binance database pool is required")
	}
	if runID == "" || checkpoint.RequestFingerprint == "" {
		return ErrCheckpointMismatch
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return fmt.Errorf("encode Binance checkpoint: %w", err)
	}
	tag, err := s.Pool.Exec(ctx, `UPDATE ingestion_runs SET coverage = coverage || $2::jsonb WHERE id = $1::uuid AND status = 'running'`, runID, data)
	if err != nil {
		return fmt.Errorf("save Binance checkpoint: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("Binance ingestion run %q is not running", runID)
	}
	return nil
}

func (s Store) LoadCheckpoint(ctx context.Context, runID string) (KlineCheckpoint, error) {
	if s.Pool == nil {
		return KlineCheckpoint{}, errors.New("Binance database pool is required")
	}
	var data []byte
	if err := s.Pool.QueryRow(ctx, `SELECT coverage FROM ingestion_runs WHERE id = $1::uuid`, runID).Scan(&data); err != nil {
		return KlineCheckpoint{}, fmt.Errorf("load Binance checkpoint: %w", err)
	}
	if len(data) == 0 || string(data) == "{}" {
		return KlineCheckpoint{}, ErrNoCheckpoint
	}
	var checkpoint KlineCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return KlineCheckpoint{}, fmt.Errorf("decode Binance checkpoint: %w", err)
	}
	return checkpoint, nil
}

func (s Store) PersistInstrumentRecords(ctx context.Context, records []ingestion.NormalizedRecord) (int64, error) {
	if s.Pool == nil {
		return 0, errors.New("Binance database pool is required")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin Binance instrument transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var changed int64
	for index, normalized := range records {
		record, err := DecodeInstrumentRecord(normalized)
		if err != nil {
			return 0, fmt.Errorf("decode Binance instrument %d: %w", index, err)
		}
		if record.MissingFromCatalog {
			continue
		}
		external, _ := json.Marshal(map[string]any{"provider": "binance", "upstream_status": record.UpstreamStatus})
		var rows int64
		err = tx.QueryRow(ctx, `
INSERT INTO instruments (canonical_symbol, instrument_type, native_currency, external_ids, status)
VALUES ($1, 'crypto_spot', $2, $3, $4)
ON CONFLICT (canonical_symbol) DO UPDATE SET status = EXCLUDED.status
RETURNING 1`, record.Symbol, record.QuoteAsset, external, record.LifecycleStatus).Scan(&rows)
		if err != nil {
			return 0, fmt.Errorf("persist Binance instrument %d: %w", index, err)
		}
		changed += rows
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit Binance instruments: %w", err)
	}
	return changed, nil
}

// PersistPriceRecords requires the requested symbol explicitly. This prevents
// a response for one symbol from being written under another instrument.
func (s Store) PersistPriceRecords(ctx context.Context, symbol string, records []ingestion.NormalizedRecord) (int64, error) {
	if s.Pool == nil {
		return 0, errors.New("Binance database pool is required")
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if !validSymbol(symbol) {
		return 0, errors.New("Binance price symbol is required")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin Binance price transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var instrumentID, quoteCurrency string
	if err := tx.QueryRow(ctx, `SELECT id::text, native_currency FROM instruments WHERE canonical_symbol = $1`, symbol).Scan(&instrumentID, &quoteCurrency); err != nil {
		return 0, fmt.Errorf("find Binance instrument %q: %w", symbol, err)
	}
	var inserted int64
	for index, normalized := range records {
		record, err := DecodePriceRecord(normalized)
		if err != nil {
			return 0, fmt.Errorf("decode Binance price %d: %w", index, err)
		}
		if record.Symbol != symbol {
			return 0, fmt.Errorf("Binance price %d belongs to symbol %q, want %q", index, record.Symbol, symbol)
		}
		if record.QuoteAsset != "" && record.QuoteAsset != quoteCurrency {
			return 0, fmt.Errorf("Binance price %d quote %q does not match instrument quote %q", index, record.QuoteAsset, quoteCurrency)
		}
		var rawObjectID string
		if err := tx.QueryRow(ctx, `SELECT id::text FROM raw_objects WHERE content_sha256 = $1`, normalized.RawObjectSHA256).Scan(&rawObjectID); err != nil {
			return 0, fmt.Errorf("find Binance raw object %q: %w", normalized.RawObjectSHA256, err)
		}
		quality, err := json.Marshal(record.QualityFlags)
		if err != nil {
			return 0, fmt.Errorf("encode Binance quality flags: %w", err)
		}
		var rows int64
		err = tx.QueryRow(ctx, `
INSERT INTO price_revisions (instrument_id, quote_currency, observation_time, price, source_known_at, knowledge_time_basis, raw_object_id, quality_flags)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::uuid, $8)
ON CONFLICT (instrument_id, quote_currency, observation_time, price, source_known_at, raw_object_id) DO NOTHING
		RETURNING 1`, instrumentID, quoteCurrency, record.ObservationTime, record.Price, nil, record.KnowledgeTimeBasis, rawObjectID, quality).Scan(&rows)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return 0, fmt.Errorf("insert Binance price %d: %w", index, err)
		}
		inserted += rows
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit Binance prices: %w", err)
	}
	return inserted, nil
}
