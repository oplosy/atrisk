package ingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oplosy/atrisk/internal/archive"
)

type fixtureNormalizer struct{}

func (fixtureNormalizer) Normalize(_ context.Context, payload RawPayload) ([]NormalizedRecord, error) {
	var value map[string]any
	if err := json.Unmarshal(payload.Body, &value); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(value)
	return []NormalizedRecord{{Kind: "fixture", RawObjectKey: payload.Archive.Key, RawObjectSHA256: payload.Archive.ContentSHA256, Payload: encoded}}, nil
}

func testReplayPreservesRawProvenance(t *testing.T) {
	t.Helper()
	payload, records, err := ReplayBytes(context.Background(), []byte(`{"value":42}`), "application/json", fixtureNormalizer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RawObjectKey != payload.Archive.Key || records[0].RawObjectSHA256 != payload.Archive.ContentSHA256 {
		t.Fatalf("raw provenance was not preserved: payload=%+v records=%+v", payload.Archive, records)
	}
}

func TestIngestionFixtureReplayPreservesRawProvenance(t *testing.T) {
	testReplayPreservesRawProvenance(t)
}

func TestRawArchiveFixtureReplayPreservesRawProvenance(t *testing.T) {
	testReplayPreservesRawProvenance(t)
}

type recordingArchiveStore struct {
	mu     sync.Mutex
	object archive.Object
}

func (s *recordingArchiveStore) Put(_ context.Context, object archive.Object) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.object = archive.Object{Key: object.Key, ContentSHA256: object.ContentSHA256, MediaType: object.MediaType, Body: append([]byte(nil), object.Body...), Metadata: archive.SanitizedMetadata(object.Metadata)}
	return nil
}

func (s *recordingArchiveStore) Get(_ context.Context, _ string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return io.NopCloser(bytes.NewReader(s.object.Body)), nil
}

type recordingRunStore struct {
	started, completed, failed bool
}

func (s *recordingRunStore) StartRun(context.Context, RunSpec) (string, bool, error) {
	s.started = true
	return "run-1", false, nil
}

func (s *recordingRunStore) RegisterRawObject(context.Context, RawObjectRegistration) (string, error) {
	return "raw-1", nil
}

func (s *recordingRunStore) CompleteRun(context.Context, string, string, map[string]any) error {
	s.completed = true
	return nil
}

func (s *recordingRunStore) FailRun(context.Context, string, string) error {
	s.failed = true
	return nil
}

func TestPipelineArchivesBeforeNormalizationAndPreservesProvenance(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Authorization", "Bearer source-secret")
		_, _ = w.Write([]byte(`{"value":42}`))
	}))
	defer server.Close()

	archiveStore := &recordingArchiveStore{}
	runStore := &recordingRunStore{}
	pipeline := Pipeline{
		Fetcher:  &HTTPFetcher{Client: server.Client(), AllowedHosts: map[string]struct{}{"127.0.0.1": {}}, MaxBodyBytes: 1024},
		Archive:  archiveStore,
		Runs:     runStore,
		MaxBytes: 1024,
	}
	result, err := pipeline.Run(context.Background(), RunSpec{SourceID: "source-1", IdempotencyKey: "fixture-1", AdapterVersion: "test-1"}, FetchRequest{URL: server.URL + "?api_key=source-secret"}, fixtureNormalizer{})
	if err != nil {
		t.Fatal(err)
	}
	if runStore.failed || !runStore.started || !runStore.completed {
		t.Fatalf("unexpected run lifecycle: %+v", runStore)
	}
	if len(result.Records) != 1 || result.Records[0].RawObjectKey != result.Payload.Archive.Key || result.Records[0].RawObjectSHA256 != result.Payload.Archive.ContentSHA256 {
		t.Fatalf("normalized record lost raw provenance: result=%+v", result)
	}
	archiveStore.mu.Lock()
	defer archiveStore.mu.Unlock()
	if !bytes.Equal(archiveStore.object.Body, result.Payload.Body) || strings.Contains(archiveStore.object.Metadata["request-uri"], "source-secret") {
		t.Fatalf("archived object was not exact and secret-safe: %+v", archiveStore.object)
	}
}

func TestReplayFixtureUsesSavedBytesWithoutNetwork(t *testing.T) {
	path := filepath.Join("..", "..", "test", "fixtures", "http", "sample-response.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	payload, records, err := ReplayFixture(context.Background(), path, "application/json", 1024, fixtureNormalizer{})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Body) == 0 || len(records) != 1 || records[0].RawObjectSHA256 != archive.SHA256Hex(payload.Body) {
		t.Fatalf("saved fixture did not replay with exact raw provenance: payload=%+v records=%+v", payload.Archive, records)
	}
	if !payload.RetrievedAt.After(time.Time{}) {
		t.Fatal("fixture replay did not record a retrieval timestamp")
	}
}
