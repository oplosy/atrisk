package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oplosy/atrisk/internal/archive"
)

var ErrRawObjectConflict = errors.New("raw object conflicts with existing content address")

type NormalizedRecord struct {
	Kind            string
	RawObjectKey    string
	RawObjectSHA256 string
	Payload         json.RawMessage
}

type Normalizer interface {
	Normalize(context.Context, RawPayload) ([]NormalizedRecord, error)
}

// Adapter combines source-specific request construction and normalization.
// Provider packages implement this interface without owning fetching, archive,
// or run-state persistence.
type Adapter interface {
	BuildRequest(context.Context, RunSpec) (FetchRequest, error)
	Normalize(context.Context, RawPayload) ([]NormalizedRecord, error)
}

type RawPayload struct {
	Body          []byte
	MediaType     string
	RetrievedAt   time.Time
	CorrelationID string
	RequestURI    string
	Headers       map[string]string
	Archive       archive.Reference
}

type RunSpec struct {
	SourceID       string
	DatasetID      *string
	IdempotencyKey string
	AdapterVersion string
	RequestedFrom  *time.Time
	RequestedTo    *time.Time
}

type RunResult struct {
	RunID     string
	Payload   RawPayload
	Records   []NormalizedRecord
	Duplicate bool
}

type RawObjectRegistration struct {
	Reference      archive.Reference
	RetrievedAt    time.Time
	RequestURI     string
	RequestHeaders map[string]string
	IngestionRunID *string
	// OccurrenceKey identifies one request/page within an ingestion run. It is
	// separate from the content address so a refetch can reuse the raw object
	// while retaining retrieval provenance.
	OccurrenceKey string
}

type RunStore interface {
	StartRun(context.Context, RunSpec) (runID string, duplicate bool, err error)
	RegisterRawObject(context.Context, RawObjectRegistration) (rawObjectID string, err error)
	CompleteRun(context.Context, string, string, map[string]any) error
	FailRun(context.Context, string, string) error
}

type RunStatusStore interface {
	LoadRunStatus(context.Context, string) (string, error)
}

// PersistFunc is the durable normalized-record boundary. A pipeline cannot
// report success unless this callback accepts every normalized record.
type PersistFunc func(context.Context, string, []NormalizedRecord) error

// CoverageFunc rejects a response that cannot represent a complete bounded
// request. It runs after persistence so a caller can checkpoint only after
// accepted records are durable.
type CoverageFunc func(RawPayload) error

type Pipeline struct {
	Fetcher  *HTTPFetcher
	Archive  archive.Store
	Runs     RunStore
	Persist  PersistFunc
	Coverage CoverageFunc
	MaxBytes int64
}

func (p Pipeline) Run(ctx context.Context, spec RunSpec, request FetchRequest, normalizer Normalizer) (RunResult, error) {
	if p.Fetcher == nil || p.Archive == nil || p.Runs == nil || p.Persist == nil || normalizer == nil {
		return RunResult{}, errors.New("fetcher, archive, run store, persistence, and normalizer are required")
	}
	runID, duplicate, err := p.Runs.StartRun(ctx, spec)
	if err != nil {
		return RunResult{}, err
	}
	if duplicate {
		if statusStore, ok := p.Runs.(RunStatusStore); ok {
			status, statusErr := statusStore.LoadRunStatus(ctx, runID)
			if statusErr != nil {
				return RunResult{}, statusErr
			}
			if status == "succeeded" {
				return RunResult{RunID: runID, Duplicate: true}, nil
			}
		}
	}
	fetcher := *p.Fetcher
	if p.MaxBytes > 0 {
		fetcher.MaxBodyBytes = p.MaxBytes
	}
	response, err := fetcher.Fetch(ctx, request)
	if err != nil {
		_ = p.Runs.FailRun(ctx, runID, "fetch_failed")
		return RunResult{}, err
	}
	ref, err := archive.ArchivePayload(ctx, p.Archive, response.Body, response.MediaType, map[string]string{
		"request-uri": response.RequestURI, "correlation-id": response.CorrelationID,
	})
	if err != nil {
		_ = p.Runs.FailRun(ctx, runID, "archive_failed")
		return RunResult{}, fmt.Errorf("archive fetched response: %w", err)
	}
	rawID, err := p.Runs.RegisterRawObject(ctx, RawObjectRegistration{
		Reference: ref, RetrievedAt: response.RetrievedAt, RequestURI: response.RequestURI,
		RequestHeaders: response.Headers, IngestionRunID: &runID, OccurrenceKey: response.RequestURI,
	})
	if err != nil {
		_ = p.Runs.FailRun(ctx, runID, "registration_failed")
		return RunResult{}, fmt.Errorf("register raw object: %w", err)
	}
	records, err := normalizer.Normalize(ctx, RawPayload{
		Body: response.Body, MediaType: response.MediaType, RetrievedAt: response.RetrievedAt,
		CorrelationID: response.CorrelationID, RequestURI: response.RequestURI, Headers: response.Headers,
		Archive: ref,
	})
	if err != nil {
		_ = p.Runs.FailRun(ctx, runID, "normalize_failed")
		return RunResult{}, fmt.Errorf("normalize response %s: %w", rawID, err)
	}
	for index := range records {
		if records[index].RawObjectKey != ref.Key || records[index].RawObjectSHA256 != ref.ContentSHA256 {
			_ = p.Runs.FailRun(ctx, runID, "provenance_failed")
			return RunResult{}, fmt.Errorf("normalized record %d does not reference archived response", index)
		}
	}
	if err := p.Persist(ctx, runID, records); err != nil {
		_ = p.Runs.FailRun(ctx, runID, "persistence_failed")
		return RunResult{}, fmt.Errorf("persist normalized records %s: %w", rawID, err)
	}
	if p.Coverage != nil {
		if err := p.Coverage(RawPayload{Body: response.Body, MediaType: response.MediaType, RetrievedAt: response.RetrievedAt, CorrelationID: response.CorrelationID, RequestURI: response.RequestURI, Headers: response.Headers, Archive: ref}); err != nil {
			_ = p.Runs.FailRun(ctx, runID, "incomplete_coverage")
			return RunResult{}, err
		}
	}
	if err := p.Runs.CompleteRun(ctx, runID, "succeeded", map[string]any{"records": len(records), "raw_object_id": rawID}); err != nil {
		return RunResult{}, fmt.Errorf("complete ingestion run: %w", err)
	}
	return RunResult{RunID: runID, Payload: RawPayload{Body: response.Body, MediaType: response.MediaType, RetrievedAt: response.RetrievedAt, CorrelationID: response.CorrelationID, RequestURI: response.RequestURI, Headers: response.Headers, Archive: ref}, Records: records}, nil
}

// RunAdapter executes an adapter through the common bounded fetch/archive
// pipeline so provider implementations cannot bypass provenance checks.
func (p Pipeline) RunAdapter(ctx context.Context, spec RunSpec, adapter Adapter) (RunResult, error) {
	if adapter == nil {
		return RunResult{}, errors.New("adapter is required")
	}
	request, err := adapter.BuildRequest(ctx, spec)
	if err != nil {
		return RunResult{}, fmt.Errorf("build adapter request: %w", err)
	}
	return p.Run(ctx, spec, request, adapter)
}

// ReplayFixture runs the same normalization contract from a saved fixture,
// without opening a network connection. It is intentionally bounded.
func ReplayFixture(ctx context.Context, path, mediaType string, maxBytes int64, normalizer Normalizer) (RawPayload, []NormalizedRecord, error) {
	if normalizer == nil {
		return RawPayload{}, nil, errors.New("normalizer is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return RawPayload{}, nil, fmt.Errorf("open fixture: %w", err)
	}
	defer file.Close()
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	body, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return RawPayload{}, nil, fmt.Errorf("read fixture: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return RawPayload{}, nil, fmt.Errorf("fixture exceeds %d bytes", maxBytes)
	}
	return replayBytes(ctx, body, mediaType, normalizer)
}

func ReplayBytes(ctx context.Context, body []byte, mediaType string, normalizer Normalizer) (RawPayload, []NormalizedRecord, error) {
	return replayBytes(ctx, body, mediaType, normalizer)
}

func replayBytes(ctx context.Context, body []byte, mediaType string, normalizer Normalizer) (RawPayload, []NormalizedRecord, error) {
	if normalizer == nil {
		return RawPayload{}, nil, errors.New("normalizer is required")
	}
	payload := RawPayload{Body: append([]byte(nil), body...), MediaType: mediaType, RetrievedAt: time.Now().UTC(), CorrelationID: "fixture-replay", RequestURI: "fixture://replay", Archive: archive.Reference{ContentSHA256: archive.SHA256Hex(body), Key: archive.ObjectKey(archive.SHA256Hex(body)), ByteLength: int64(len(body)), MediaType: mediaType}}
	records, err := normalizer.Normalize(ctx, payload)
	if err != nil {
		return RawPayload{}, nil, err
	}
	for index := range records {
		if records[index].RawObjectKey != payload.Archive.Key || records[index].RawObjectSHA256 != payload.Archive.ContentSHA256 {
			return RawPayload{}, nil, fmt.Errorf("fixture normalized record %d has no exact raw provenance", index)
		}
	}
	return payload, records, nil
}

// DatabaseStore persists the run and raw-object registration in PostgreSQL.
// It uses the existing append-only core tables and keeps credentials outside
// this package; callers provide an already authenticated pool.
type DatabaseStore struct{ Pool *pgxpool.Pool }

func (s DatabaseStore) StartRun(ctx context.Context, spec RunSpec) (string, bool, error) {
	if s.Pool == nil {
		return "", false, errors.New("database pool is required")
	}
	var id string
	var dataset any
	if spec.DatasetID != nil {
		dataset = *spec.DatasetID
	}
	err := s.Pool.QueryRow(ctx, `
INSERT INTO ingestion_runs (source_id, dataset_id, idempotency_key, adapter_version, requested_from, requested_to, status)
VALUES ($1, $2, $3, $4, $5, $6, 'running')
ON CONFLICT (source_id, idempotency_key) DO NOTHING
RETURNING id::text`, spec.SourceID, dataset, spec.IdempotencyKey, spec.AdapterVersion, spec.RequestedFrom, spec.RequestedTo).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("start ingestion run: %w", err)
	}
	err = s.Pool.QueryRow(ctx, `SELECT id::text FROM ingestion_runs WHERE source_id=$1 AND idempotency_key=$2`, spec.SourceID, spec.IdempotencyKey).Scan(&id)
	if err != nil {
		return "", false, err
	}
	if _, resetErr := s.Pool.Exec(ctx, `UPDATE ingestion_runs SET status='running', error_code=NULL, completed_at=NULL WHERE id=$1::uuid AND status='failed'`, id); resetErr != nil {
		return "", false, fmt.Errorf("resume failed ingestion run: %w", resetErr)
	}
	return id, true, nil
}

func (s DatabaseStore) LoadRunStatus(ctx context.Context, runID string) (string, error) {
	if s.Pool == nil {
		return "", errors.New("database pool is required")
	}
	var status string
	if err := s.Pool.QueryRow(ctx, `SELECT status FROM ingestion_runs WHERE id = $1::uuid`, runID).Scan(&status); err != nil {
		return "", fmt.Errorf("load ingestion run status: %w", err)
	}
	return status, nil
}

func (s DatabaseStore) RegisterRawObject(ctx context.Context, item RawObjectRegistration) (string, error) {
	if s.Pool == nil {
		return "", errors.New("database pool is required")
	}
	// The body is intentionally not part of this registration object. The
	// archive store validates the actual bytes; here validate the address
	// shape and the object-key/length invariants before touching PostgreSQL.
	if item.Reference.ByteLength < 0 || len(item.Reference.ContentSHA256) != 64 || archive.ObjectKey(item.Reference.ContentSHA256) != item.Reference.Key {
		return "", archive.ErrInvalidDigest
	}
	metadata, err := json.Marshal(archive.SanitizedMetadata(item.RequestHeaders))
	if err != nil {
		return "", err
	}
	var id string
	err = s.Pool.QueryRow(ctx, `
INSERT INTO raw_objects (content_sha256, object_key, media_type, byte_length, retrieved_at, request_uri, request_metadata, ingestion_run_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (content_sha256) DO NOTHING
		RETURNING id::text`, item.Reference.ContentSHA256, item.Reference.Key, item.Reference.MediaType, item.Reference.ByteLength, item.RetrievedAt, archive.RedactedURL(item.RequestURI), metadata, item.IngestionRunID).Scan(&id)
	if err == nil {
		if err := s.registerOccurrence(ctx, id, item, metadata); err != nil {
			return "", err
		}
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("register raw object: %w", err)
	}
	err = s.Pool.QueryRow(ctx, `SELECT id::text FROM raw_objects WHERE content_sha256=$1`, item.Reference.ContentSHA256).Scan(&id)
	if err != nil {
		return "", err
	}
	if err := s.validateRawObject(ctx, id, item, metadata); err != nil {
		return "", err
	}
	if err := s.registerOccurrence(ctx, id, item, metadata); err != nil {
		return "", err
	}
	return id, nil
}

func (s DatabaseStore) validateRawObject(ctx context.Context, id string, item RawObjectRegistration, expectedMetadata []byte) error {
	var (
		objectKey  string
		mediaType  string
		byteLength int64
	)
	err := s.Pool.QueryRow(ctx, `
SELECT object_key, media_type, byte_length
FROM raw_objects
WHERE id = $1::uuid AND content_sha256 = $2`, id, item.Reference.ContentSHA256).
		Scan(&objectKey, &mediaType, &byteLength)
	if err != nil {
		return fmt.Errorf("validate raw object registration: %w", err)
	}
	if objectKey != item.Reference.Key || mediaType != item.Reference.MediaType || byteLength != item.Reference.ByteLength {
		return ErrRawObjectConflict
	}
	if item.IngestionRunID == nil {
		var retrievedAt time.Time
		var requestURI string
		var metadataMatches bool
		if err := s.Pool.QueryRow(ctx, `
SELECT retrieved_at, COALESCE(request_uri, ''), request_metadata = $2::jsonb
FROM raw_objects WHERE id = $1::uuid`, id, string(expectedMetadata)).Scan(&retrievedAt, &requestURI, &metadataMatches); err != nil {
			return fmt.Errorf("validate raw object metadata: %w", err)
		}
		if !retrievedAt.Equal(item.RetrievedAt.UTC().Truncate(time.Microsecond)) || requestURI != archive.RedactedURL(item.RequestURI) || !metadataMatches {
			return ErrRawObjectConflict
		}
	}
	return nil
}

func (s DatabaseStore) registerOccurrence(ctx context.Context, rawObjectID string, item RawObjectRegistration, metadata []byte) error {
	if item.IngestionRunID == nil {
		return nil
	}
	if *item.IngestionRunID == "" {
		return errors.New("ingestion run ID is required for a raw occurrence")
	}
	key := item.OccurrenceKey
	if key == "" {
		key = archive.RedactedURL(item.RequestURI)
	}
	if key == "" {
		return errors.New("raw occurrence key is required")
	}
	requestURI := archive.RedactedURL(item.RequestURI)
	var occurrenceID string
	err := s.Pool.QueryRow(ctx, `
INSERT INTO raw_object_occurrences (raw_object_id, ingestion_run_id, occurrence_key, retrieved_at, request_uri, request_metadata)
VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6::jsonb)
ON CONFLICT (ingestion_run_id, occurrence_key) DO NOTHING
RETURNING id::text`, rawObjectID, *item.IngestionRunID, key, item.RetrievedAt.UTC(), requestURI, metadata).Scan(&occurrenceID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("register raw object occurrence: %w", err)
	}
	var storedRaw, storedURI string
	var metadataMatches bool
	if err := s.Pool.QueryRow(ctx, `
SELECT raw_object_id::text, COALESCE(request_uri, ''), request_metadata = $3::jsonb
FROM raw_object_occurrences
WHERE ingestion_run_id = $1::uuid AND occurrence_key = $2`, *item.IngestionRunID, key, string(metadata)).Scan(&storedRaw, &storedURI, &metadataMatches); err != nil {
		return fmt.Errorf("validate raw object occurrence: %w", err)
	}
	if storedRaw != rawObjectID || storedURI != requestURI || !metadataMatches {
		return ErrRawObjectConflict
	}
	return nil
}

func (s DatabaseStore) CompleteRun(ctx context.Context, runID, status string, coverage map[string]any) error {
	data, err := json.Marshal(coverage)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx, `UPDATE ingestion_runs SET status=$2, coverage=coverage || $3::jsonb, completed_at=clock_timestamp() WHERE id=$1::uuid`, runID, status, data)
	return err
}

func (s DatabaseStore) FailRun(ctx context.Context, runID, code string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE ingestion_runs SET status='failed', error_code=$2, completed_at=clock_timestamp() WHERE id=$1::uuid`, runID, code)
	return err
}
