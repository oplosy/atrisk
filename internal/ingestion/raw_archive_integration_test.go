package ingestion

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/platform/database"
)

func TestRawArchive(t *testing.T) {
	dsn := os.Getenv("ATLASRISK_TEST_DATABASE_URL")
	if err := database.ValidateIsolatedTestDatabaseURL(dsn); err != nil {
		if os.Getenv("ATLASRISK_REQUIRE_TEST_DATABASE") == "1" {
			t.Fatalf("isolated database validation failed: %v", err)
		}
		t.Skipf("isolated database unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrationsDir, err := filepath.Abs(filepath.Join("..", "..", "db", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, dsn, migrationsDir); err != nil {
		t.Fatalf("migrate isolated database: %v", err)
	}
	pool, err := database.OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open isolated database: %v", err)
	}
	defer pool.Close()

	body, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "http", "sample-response.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest := archive.SHA256Hex(body)
	retrievedAt := time.Date(2026, time.January, 2, 3, 4, 5, 123456000, time.UTC)
	item := RawObjectRegistration{
		Reference:   archive.Reference{Key: archive.ObjectKey(digest), ContentSHA256: digest, ByteLength: int64(len(body)), MediaType: "application/json"},
		RetrievedAt: retrievedAt, RequestURI: "https://example.test/data?api_key=source-secret&page=1",
		RequestHeaders: map[string]string{"Authorization": "Bearer source-secret", "X-Request-ID": "corr-1"},
	}
	store := DatabaseStore{Pool: pool}
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
	if metadata["authorization"] != "" || metadata["x-request-id"] != "corr-1" || storedURI == "" {
		t.Fatalf("raw object metadata was not secret-safe: metadata=%v uri=%q", metadata, storedURI)
	}

	conflict := item
	conflict.Reference.MediaType = "text/plain"
	if _, err := store.RegisterRawObject(ctx, conflict); !errors.Is(err, ErrRawObjectConflict) {
		t.Fatalf("conflicting immutable raw object registration was accepted: %v", err)
	}
	conflict = item
	conflict.RequestURI = "https://example.test/other?page=1"
	if _, err := store.RegisterRawObject(ctx, conflict); !errors.Is(err, ErrRawObjectConflict) {
		t.Fatalf("conflicting raw provenance was accepted: %v", err)
	}
}
