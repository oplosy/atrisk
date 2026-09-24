package archive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
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

type fakeS3Object struct {
	body      []byte
	mediaType string
	metadata  map[string]string
}

type fakeS3API struct {
	mu              sync.Mutex
	objects         map[string]fakeS3Object
	conditionalPuts int
}

func (s *fakeS3API) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "not found"}
	}
	return &s3.HeadObjectOutput{ContentLength: aws.Int64(int64(len(object.body))), ContentType: aws.String(object.mediaType), Metadata: object.metadata}, nil
}

func (s *fakeS3API) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	body, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if aws.ToString(input.IfNoneMatch) == "*" {
		s.conditionalPuts++
		if _, exists := s.objects[aws.ToString(input.Key)]; exists {
			return nil, &smithy.GenericAPIError{Code: "PreconditionFailed", Message: "already exists"}
		}
	}
	metadata := make(map[string]string, len(input.Metadata))
	for key, value := range input.Metadata {
		metadata[key] = value
	}
	s.objects[aws.ToString(input.Key)] = fakeS3Object{body: body, mediaType: aws.ToString(input.ContentType), metadata: metadata}
	return &s3.PutObjectOutput{}, nil
}

func (s *fakeS3API) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, ok := s.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, &smithy.GenericAPIError{Code: "NoSuchKey", Message: "not found"}
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(object.body))}, nil
}

func TestS3StoreConditionalConcurrentCreateAndContentValidation(t *testing.T) {
	api := &fakeS3API{objects: make(map[string]fakeS3Object)}
	store, err := NewS3Store(api, "raw")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("same response")
	object := Object{Key: ObjectKey(SHA256Hex(body)), ContentSHA256: SHA256Hex(body), MediaType: "text/plain", Body: body}
	errs := make(chan error, 16)
	for range 16 {
		go func() { errs <- store.Put(context.Background(), object) }()
	}
	for range 16 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	api.mu.Lock()
	if len(api.objects) != 1 || api.conditionalPuts == 0 {
		t.Fatalf("conditional S3 create was not exercised: objects=%d conditional_puts=%d", len(api.objects), api.conditionalPuts)
	}
	api.objects[object.Key] = fakeS3Object{body: []byte("same respoNse"), mediaType: object.MediaType, metadata: map[string]string{"sha256": object.ContentSHA256}}
	api.mu.Unlock()
	if err := store.Put(context.Background(), object); !errors.Is(err, ErrContentMismatch) {
		t.Fatalf("tampered existing S3 object was accepted: %v", err)
	}
}
