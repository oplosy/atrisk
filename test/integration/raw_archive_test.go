package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/ingestion"
)

func TestRawArchiveDatabaseStoreRegistration(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body := []byte(`{"integration":"database-store-registration","revision":1}`)
	digest := archive.SHA256Hex(body)
	item := ingestion.RawObjectRegistration{
		Reference: archive.Reference{
			Key:           archive.ObjectKey(digest),
			ContentSHA256: digest,
			ByteLength:    int64(len(body)),
			MediaType:     "application/json",
		},
		RetrievedAt: time.Date(2026, time.January, 2, 3, 4, 5, 123456000, time.UTC),
		RequestURI:  "https://example.test/data?api_key=source-secret&page=1",
		RequestHeaders: map[string]string{
			"Authorization": "Bearer source-secret",
			"X-Request-ID":  "corr-integration",
		},
	}
	store := ingestion.DatabaseStore{Pool: pool}

	id, err := store.RegisterRawObject(ctx, item)
	if err != nil {
		t.Fatalf("register raw object: %v", err)
	}
	duplicateID, err := store.RegisterRawObject(ctx, item)
	if err != nil || duplicateID != id {
		t.Fatalf("identical raw registration was not idempotent: id=%q duplicate=%q err=%v", id, duplicateID, err)
	}

	var (
		storedSHA       string
		storedKey       string
		storedMediaType string
		storedLength    int64
		storedURI       string
		storedMetadata  []byte
	)
	if err := pool.QueryRow(ctx, `
SELECT content_sha256, object_key, media_type, byte_length, request_uri, request_metadata
FROM raw_objects WHERE id = $1::uuid`, id).Scan(&storedSHA, &storedKey, &storedMediaType, &storedLength, &storedURI, &storedMetadata); err != nil {
		t.Fatalf("read registered raw object: %v", err)
	}
	if storedSHA != digest || storedKey != item.Reference.Key || storedMediaType != item.Reference.MediaType || storedLength != int64(len(body)) || storedURI != archive.RedactedURL(item.RequestURI) {
		t.Fatalf("raw object association changed: sha=%q key=%q media=%q length=%d uri=%q", storedSHA, storedKey, storedMediaType, storedLength, storedURI)
	}
	var metadata map[string]string
	if err := json.Unmarshal(storedMetadata, &metadata); err != nil {
		t.Fatalf("decode stored metadata: %v", err)
	}
	if _, leaked := metadata["authorization"]; leaked || metadata["x-request-id"] != "corr-integration" || storedURI == "" || strings.Contains(storedURI, "source-secret") {
		t.Fatalf("raw object metadata was not secret-safe: metadata=%v uri=%q", metadata, storedURI)
	}

	conflict := item
	conflict.Reference.MediaType = "text/plain"
	if _, err := store.RegisterRawObject(ctx, conflict); !errors.Is(err, ingestion.ErrRawObjectConflict) {
		t.Fatalf("conflicting immutable media type was accepted: %v", err)
	}
	conflict = item
	conflict.RequestURI = "https://example.test/other?page=1"
	if _, err := store.RegisterRawObject(ctx, conflict); !errors.Is(err, ingestion.ErrRawObjectConflict) {
		t.Fatalf("conflicting raw provenance was accepted: %v", err)
	}
}
