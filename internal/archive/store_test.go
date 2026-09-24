package archive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

type memoryStore struct {
	mu       sync.Mutex
	objects  map[string][]byte
	metadata map[string]map[string]string
	puts     int
}

func (s *memoryStore) Put(_ context.Context, object Object) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.puts++
	if existing, ok := s.objects[object.Key]; ok {
		if !bytes.Equal(existing, object.Body) {
			return ErrContentMismatch
		}
		return nil
	}
	s.objects[object.Key] = append([]byte(nil), object.Body...)
	if s.metadata == nil {
		s.metadata = make(map[string]map[string]string)
	}
	s.metadata[object.Key] = object.Metadata
	return nil
}

func (s *memoryStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return io.NopCloser(bytes.NewReader(s.objects[key])), nil
}

func TestArchivePayloadIsContentAddressedAndMetadataIsRedacted(t *testing.T) {
	store := &memoryStore{objects: make(map[string][]byte)}
	body := []byte(`{"observations":[1,2,3]}`)
	ref, err := ArchivePayload(context.Background(), store, body, "application/json", map[string]string{
		"request-uri":   "https://example.test/data?api_key=secret&limit=3",
		"authorization": "Bearer secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ContentSHA256 != SHA256Hex(body) || ref.Key != ObjectKey(ref.ContentSHA256) || ref.ByteLength != int64(len(body)) {
		t.Fatalf("unexpected reference: %+v", ref)
	}
	if got := RedactedURL("https://example.test/data?api_key=secret&limit=3"); got != "https://example.test/data?limit=3" {
		t.Fatalf("secret URL was not redacted: %q", got)
	}
	metadata := SanitizedMetadata(map[string]string{"Authorization": "secret", "adapter-version": "fixture-1"})
	if _, ok := metadata["authorization"]; ok || metadata["adapter-version"] != "fixture-1" {
		t.Fatalf("unsafe metadata survived sanitization: %#v", metadata)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if got := store.metadata[ref.Key]["request-uri"]; strings.Contains(got, "secret") {
		t.Fatalf("secret URL survived archive metadata sanitization: %q", got)
	}
}

func TestArchivePayloadConcurrentSameBytesIsIdempotent(t *testing.T) {
	store := &memoryStore{objects: make(map[string][]byte)}
	body := []byte("same response")
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		go func() {
			_, err := ArchivePayload(context.Background(), store, body, "text/plain", nil)
			errs <- err
		}()
	}
	for i := 0; i < 16; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.objects) != 1 {
		t.Fatalf("expected one content-addressed object, got %d", len(store.objects))
	}
}

func TestArchiveRejectsAddressMismatch(t *testing.T) {
	store := &memoryStore{objects: make(map[string][]byte)}
	_, err := ArchivePayload(context.Background(), store, []byte("body"), "text/plain", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateObject(Object{Key: ObjectKey(SHA256Hex([]byte("other"))), ContentSHA256: SHA256Hex([]byte("other")), MediaType: "text/plain", Body: []byte("body")}); err != ErrContentMismatch {
		t.Fatalf("expected content mismatch, got %v", err)
	}
}

type failingStore struct{ err error }

func (s failingStore) Put(context.Context, Object) error { return s.err }

func (s failingStore) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, s.err
}

func TestArchivePropagatesStorageFailure(t *testing.T) {
	want := errors.New("storage unavailable")
	_, err := ArchivePayload(context.Background(), failingStore{err: want}, []byte("body"), "text/plain", nil)
	if !errors.Is(err, want) {
		t.Fatalf("storage error was not preserved: %v", err)
	}
}
