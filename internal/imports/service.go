package imports

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oplosy/atrisk/internal/archive"
)

var (
	ErrInvalidRequest = errors.New("invalid import request")
	ErrConflict       = errors.New("import request conflicts with existing state")
	ErrNotFound       = errors.New("import target not found")
	ErrUnavailable    = errors.New("import archive is unavailable")
)

type Service struct {
	Pool    *pgxpool.Pool
	Archive archive.Store
	Now     func() time.Time
}
type PreviewRequest struct {
	Kind, TargetID, SchemaVersion, CapturedAt string
	Body                                      []byte
}
type PreviewResponse struct {
	Token                string       `json:"token,omitempty"`
	ImportKind           string       `json:"import_kind"`
	TargetID             string       `json:"target_id"`
	SchemaVersion        string       `json:"schema_version"`
	ContentSHA256        string       `json:"content_sha256"`
	RowCount             int          `json:"row_count"`
	Valid                bool         `json:"valid"`
	Diagnostics          []Diagnostic `json:"diagnostics"`
	DiagnosticsTruncated bool         `json:"diagnostics_truncated"`
	ExpiresAt            time.Time    `json:"expires_at,omitempty"`
}
type CommitRequest struct {
	Kind, TargetID, SchemaVersion, CapturedAt, Token, IdempotencyKey string
	Body                                                             []byte
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s Service) Preview(ctx context.Context, req PreviewRequest) (PreviewResponse, error) {
	target := strings.ToLower(strings.TrimSpace(req.TargetID))
	if req.SchemaVersion != SchemaVersion || !validUUID(target) || (req.Kind != KindPositions && req.Kind != KindManualPrices) {
		return PreviewResponse{}, ErrInvalidRequest
	}
	if s.Pool == nil {
		return PreviewResponse{}, ErrUnavailable
	}
	if req.Kind == KindPositions {
		if _, err := parseRFC3339(req.CapturedAt); err != nil {
			return PreviewResponse{}, ErrInvalidRequest
		}
	}
	parsed := Parse(req.Body, req.Kind)
	response := PreviewResponse{ImportKind: req.Kind, TargetID: target, SchemaVersion: req.SchemaVersion, ContentSHA256: parsed.ContentSHA256, RowCount: parsed.RowCount, Valid: parsed.Valid, Diagnostics: parsed.Diagnostics, DiagnosticsTruncated: parsed.DiagnosticsTruncated}
	if !parsed.Valid {
		return response, nil
	}
	if req.Kind == KindPositions {
		var exists string
		if err := s.Pool.QueryRow(ctx, `SELECT id::text FROM portfolios WHERE id=$1::uuid`, target).Scan(&exists); err != nil {
			return PreviewResponse{}, ErrInvalidRequest
		}
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return PreviewResponse{}, fmt.Errorf("create preview token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)
	digest := sha256.Sum256([]byte(token))
	tokenDigest := hex.EncodeToString(digest[:])
	expires := s.now().Add(30 * time.Minute)
	var capturedAt any
	if req.Kind == KindPositions {
		capturedAt, _ = parseRFC3339(req.CapturedAt)
	}
	diagnostics, _ := json.Marshal(parsed.Diagnostics)
	if _, err := s.Pool.Exec(ctx, `INSERT INTO import_preview_tokens (token_digest, import_kind, target_id, captured_at, schema_version, content_sha256, row_count, diagnostics, diagnostics_truncated, expires_at) VALUES ($1,$2,$3::uuid,$4,$5,$6,$7,$8,$9,$10)`, tokenDigest, req.Kind, target, capturedAt, req.SchemaVersion, parsed.ContentSHA256, parsed.RowCount, diagnostics, parsed.DiagnosticsTruncated, expires); err != nil {
		return PreviewResponse{}, fmt.Errorf("persist preview token: %w", err)
	}
	response.Token, response.ExpiresAt = token, expires
	return response, nil
}

func (s Service) Commit(ctx context.Context, req CommitRequest) (map[string]any, error) {
	target := strings.ToLower(strings.TrimSpace(req.TargetID))
	if req.SchemaVersion != SchemaVersion || !validUUID(target) || req.Token == "" || req.IdempotencyKey == "" || len(req.IdempotencyKey) > 255 {
		return nil, ErrInvalidRequest
	}
	if s.Pool == nil {
		return nil, ErrUnavailable
	}
	capturedAt := s.now()
	if req.Kind == KindPositions {
		var parseErr error
		capturedAt, parseErr = parseRFC3339(req.CapturedAt)
		if parseErr != nil {
			return nil, ErrInvalidRequest
		}
	}
	parsed := Parse(req.Body, req.Kind)
	if !parsed.Valid {
		return nil, ErrInvalidRequest
	}
	digest := sha256.Sum256([]byte(req.Token))
	tokenDigest := hex.EncodeToString(digest[:])
	contentHash := parsed.ContentSHA256
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin import transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existing struct {
		Target, Schema, Hash string
		Rows                 int
		CapturedAt           *time.Time
		Response             []byte
	}
	err = tx.QueryRow(ctx, `SELECT target_id::text, schema_version, content_sha256, captured_at, row_count, response FROM import_results WHERE import_kind=$1 AND idempotency_key=$2`, req.Kind, req.IdempotencyKey).Scan(&existing.Target, &existing.Schema, &existing.Hash, &existing.CapturedAt, &existing.Rows, &existing.Response)
	if err == nil {
		if existing.Target != target || existing.Schema != req.SchemaVersion || existing.Hash != contentHash || !sameCapturedAt(existing.CapturedAt, capturedAtForKind(req.Kind, capturedAt)) || existing.Rows != parsed.RowCount {
			return nil, ErrConflict
		}
		var response map[string]any
		_ = json.Unmarshal(existing.Response, &response)
		return response, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("lookup import result: %w", err)
	}
	var storedKind, storedTarget, storedSchema, storedHash string
	var expiry time.Time
	var consumed, storedCapturedAt *time.Time
	err = tx.QueryRow(ctx, `SELECT import_kind,target_id::text,schema_version,content_sha256,captured_at,expires_at,consumed_at FROM import_preview_tokens WHERE token_digest=$1 FOR UPDATE`, tokenDigest).Scan(&storedKind, &storedTarget, &storedSchema, &storedHash, &storedCapturedAt, &expiry, &consumed)
	if errors.Is(err, pgx.ErrNoRows) || consumed != nil || !expiry.After(s.now()) {
		return nil, ErrConflict
	}
	if err != nil {
		return nil, fmt.Errorf("lookup preview token: %w", err)
	}
	if storedKind != req.Kind || storedTarget != target || storedSchema != req.SchemaVersion || storedHash != contentHash || !sameCapturedAt(storedCapturedAt, capturedAtForKind(req.Kind, capturedAt)) {
		return nil, ErrConflict
	}
	if err := validateDomainRows(ctx, tx, req.Kind, target, parsed); err != nil {
		return nil, err
	}
	if s.Archive == nil {
		return nil, ErrUnavailable
	}
	// Archive only after idempotency, token, target, and domain validation.
	ref, err := archive.ArchivePayload(ctx, s.Archive, req.Body, "text/csv; charset=utf-8", map[string]string{"import-kind": req.Kind})
	if err != nil {
		return nil, fmt.Errorf("archive CSV: %w", err)
	}
	var rawID string
	err = tx.QueryRow(ctx, `INSERT INTO raw_objects (content_sha256,object_key,media_type,byte_length,retrieved_at,request_metadata) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (content_sha256) DO NOTHING RETURNING id::text`, ref.ContentSHA256, ref.Key, ref.MediaType, ref.ByteLength, s.now(), `{"import":true}`).Scan(&rawID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `SELECT id::text FROM raw_objects WHERE content_sha256=$1`, ref.ContentSHA256).Scan(&rawID)
	}
	if err != nil {
		return nil, fmt.Errorf("register import raw object: %w", err)
	}
	var result map[string]any
	if req.Kind == KindPositions {
		result, err = s.commitPositions(ctx, tx, target, capturedAt, parsed, rawID, contentHash)
	} else {
		result, err = s.commitPrices(ctx, tx, parsed, rawID, contentHash)
	}
	if err != nil {
		return nil, err
	}
	result["import_kind"] = req.Kind
	result["target_id"] = target
	result["schema_version"] = req.SchemaVersion
	result["content_sha256"] = contentHash
	result["row_count"] = parsed.RowCount
	resultBytes, _ := json.Marshal(result)
	resultID, err := newUUID()
	if err != nil {
		return nil, err
	}
	result["import_result_id"] = resultID
	resultBytes, _ = json.Marshal(result)
	err = tx.QueryRow(ctx, `INSERT INTO import_results (id,import_kind,idempotency_key,target_id,captured_at,schema_version,content_sha256,row_count,raw_object_id,response) VALUES ($1::uuid,$2,$3,$4::uuid,$5,$6,$7,$8,$9::uuid,$10) ON CONFLICT (import_kind,idempotency_key) DO NOTHING RETURNING id::text`, resultID, req.Kind, req.IdempotencyKey, target, capturedAtForKind(req.Kind, capturedAt), req.SchemaVersion, contentHash, parsed.RowCount, rawID, resultBytes).Scan(&resultID)
	if errors.Is(err, pgx.ErrNoRows) {
		var target, schema, hash string
		var rows int
		var response []byte
		var storedResultCapturedAt *time.Time
		if lookupErr := tx.QueryRow(ctx, `SELECT target_id::text,schema_version,content_sha256,captured_at,row_count,response FROM import_results WHERE import_kind=$1 AND idempotency_key=$2`, req.Kind, req.IdempotencyKey).Scan(&target, &schema, &hash, &storedResultCapturedAt, &rows, &response); lookupErr != nil {
			return nil, ErrConflict
		}
		if target != strings.ToLower(req.TargetID) || schema != req.SchemaVersion || hash != contentHash || !sameCapturedAt(storedResultCapturedAt, capturedAtForKind(req.Kind, capturedAt)) || rows != parsed.RowCount {
			return nil, ErrConflict
		}
		var replay map[string]any
		_ = json.Unmarshal(response, &replay)
		return replay, nil
	}
	if err != nil {
		return nil, fmt.Errorf("persist import result: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE import_preview_tokens SET consumed_at=$2 WHERE token_digest=$1`, tokenDigest, s.now()); err != nil {
		return nil, fmt.Errorf("consume preview token: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit import transaction: %w", err)
	}
	return result, nil
}

func newUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

func capturedAtForKind(kind string, captured time.Time) *time.Time {
	if kind != KindPositions {
		return nil
	}
	value := captured.UTC()
	return &value
}

func sameCapturedAt(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.UTC().Equal(right.UTC())
}

func validateDomainRows(ctx context.Context, tx pgx.Tx, kind, target string, parsed Result) error {
	if kind == KindPositions {
		accountIDs := make([]pgtype.UUID, len(parsed.Positions))
		instrumentIDs := make([]pgtype.UUID, len(parsed.Positions))
		for i, row := range parsed.Positions {
			var err error
			accountIDs[i], err = parseUUID(row.AccountID)
			if err != nil {
				return ErrInvalidRequest
			}
			instrumentIDs[i], err = parseUUID(row.InstrumentID)
			if err != nil {
				return ErrInvalidRequest
			}
		}
		var invalid int
		err := tx.QueryRow(ctx, `
WITH input(account_id, instrument_id) AS (
    SELECT * FROM unnest($1::uuid[], $2::uuid[])
)
SELECT count(*)
FROM input
LEFT JOIN portfolios p ON p.id = $3::uuid
LEFT JOIN accounts a ON a.id = input.account_id AND a.portfolio_id = p.id
LEFT JOIN instruments i ON i.id = input.instrument_id
WHERE p.id IS NULL OR a.id IS NULL OR i.id IS NULL`, accountIDs, instrumentIDs, target).Scan(&invalid)
		if err != nil || invalid != 0 {
			return ErrInvalidRequest
		}
		return nil
	}
	instrumentIDs := make([]pgtype.UUID, len(parsed.Prices))
	for i, row := range parsed.Prices {
		var err error
		instrumentIDs[i], err = parseUUID(row.InstrumentID)
		if err != nil {
			return ErrInvalidRequest
		}
	}
	var invalid int
	if err := tx.QueryRow(ctx, `
WITH input(instrument_id) AS (SELECT * FROM unnest($1::uuid[]))
SELECT count(*)
FROM input
LEFT JOIN instruments i ON i.id = input.instrument_id
WHERE i.id IS NULL`, instrumentIDs).Scan(&invalid); err != nil || invalid != 0 {
		return ErrInvalidRequest
	}
	return nil
}

func (s Service) commitPositions(ctx context.Context, tx pgx.Tx, target string, capturedAt time.Time, parsed Result, rawID, hash string) (map[string]any, error) {
	var snapshotID string
	err := tx.QueryRow(ctx, `INSERT INTO portfolio_snapshots (portfolio_id,captured_at) VALUES ($1::uuid,$2) RETURNING id::text`, target, capturedAt).Scan(&snapshotID)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	portfolioID, err := parseUUID(target)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	snapshotUUID, err := parseUUID(snapshotID)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	copyRows := make([][]any, 0, len(parsed.Positions))
	for _, row := range parsed.Positions {
		accountID, accountErr := parseUUID(row.AccountID)
		instrumentID, instrumentErr := parseUUID(row.InstrumentID)
		quantity, quantityErr := parseNumeric(row.Quantity, false)
		cost, costErr := parseNumeric(row.TotalCostBasis, true)
		duration, durationErr := parseNumeric(row.ModifiedDurationYears, true)
		convexity, convexityErr := parseNumeric(row.ConvexityYearsSquared, true)
		if accountErr != nil || instrumentErr != nil || quantityErr != nil || costErr != nil || durationErr != nil || convexityErr != nil {
			return nil, ErrInvalidRequest
		}
		copyRows = append(copyRows, []any{portfolioID, snapshotUUID, accountID, instrumentID, quantity, nullableNumeric(cost), nullableNumeric(duration), nullableNumeric(convexity)})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"portfolio_snapshot_lines"}, []string{"portfolio_id", "snapshot_id", "account_id", "instrument_id", "quantity", "total_cost_basis", "modified_duration_years", "convexity_years_squared"}, pgx.CopyFromRows(copyRows)); err != nil {
		return nil, ErrInvalidRequest
	}
	return map[string]any{"snapshot_id": snapshotID, "raw_object_sha256": hash}, nil
}
func (s Service) commitPrices(ctx context.Context, tx pgx.Tx, parsed Result, rawID, hash string) (map[string]any, error) {
	rawObjectID, err := parseUUID(rawID)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	instrumentIDs := make([]pgtype.UUID, len(parsed.Prices))
	quoteCurrencies := make([]string, len(parsed.Prices))
	observationTimes := make([]pgtype.Timestamptz, len(parsed.Prices))
	prices := make([]pgtype.Numeric, len(parsed.Prices))
	sourceKnownAt := make([]pgtype.Timestamptz, len(parsed.Prices))
	bases := make([]string, len(parsed.Prices))
	for i, row := range parsed.Prices {
		instrumentIDs[i], err = parseUUID(row.InstrumentID)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		prices[i], err = parseNumeric(row.Price, false)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		quoteCurrencies[i] = row.QuoteCurrency
		observationTimes[i] = pgtype.Timestamptz{Time: row.ObservationTime, Valid: true}
		bases[i] = "first_observed_by_system"
		if row.SourceKnownAtTime != nil {
			sourceKnownAt[i] = pgtype.Timestamptz{Time: row.SourceKnownAtTime.UTC(), Valid: true}
			bases[i] = "source_effective_at"
		}
	}
	command, err := tx.Exec(ctx, `
WITH input(instrument_id, quote_currency, observation_time, price, source_known_at, knowledge_time_basis) AS (
    SELECT * FROM unnest($1::uuid[], $2::text[], $3::timestamptz[], $4::numeric[], $5::timestamptz[], $6::text[])
)
INSERT INTO price_revisions (instrument_id, quote_currency, observation_time, price, source_known_at, knowledge_time_basis, raw_object_id)
SELECT instrument_id, quote_currency, observation_time, price, source_known_at, knowledge_time_basis, $7::uuid
FROM input
`, instrumentIDs, quoteCurrencies, observationTimes, prices, sourceKnownAt, bases, rawObjectID)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	return map[string]any{"revision_count": command.RowsAffected(), "raw_object_sha256": hash}, nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(strings.TrimSpace(value)); err != nil || !id.Valid {
		return id, ErrInvalidRequest
	}
	return id, nil
}

func parseNumeric(value string, optional bool) (pgtype.Numeric, error) {
	if value == "" && optional {
		return pgtype.Numeric{}, nil
	}
	var numeric pgtype.Numeric
	if err := numeric.Scan(value); err != nil || !numeric.Valid {
		return numeric, ErrInvalidRequest
	}
	return numeric, nil
}

func nullableNumeric(value pgtype.Numeric) any {
	if !value.Valid {
		return nil
	}
	return value
}
