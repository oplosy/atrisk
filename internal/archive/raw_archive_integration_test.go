package archive

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestRawArchive(t *testing.T) {
	endpoint := os.Getenv("ATLASRISK_S3_ENDPOINT")
	if endpoint == "" {
		if os.Getenv("ATLASRISK_REQUIRE_TEST_DATABASE") == "1" {
			t.Fatal("ATLASRISK_S3_ENDPOINT is required for RawArchive integration")
		}
		t.Skip("Garage endpoint is not configured")
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "http", "sample-response.json"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewS3StoreFromConfig(context.Background(), ClientConfig{
		Endpoint: endpoint, Region: getenvOr("ATLASRISK_S3_REGION", "garage"),
		AccessKeyID: os.Getenv("AWS_ACCESS_KEY_ID"), SecretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
		Bucket: os.Getenv("ATLASRISK_S3_BUCKET"),
	})
	if err != nil {
		t.Fatal(err)
	}
	refs := make(chan Reference, 8)
	errs := make(chan error, 8)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			ref, archiveErr := ArchivePayload(context.Background(), store, body, "application/json", map[string]string{"fixture": "sample-response"})
			if archiveErr != nil {
				errs <- archiveErr
				return
			}
			refs <- ref
		}()
	}
	group.Wait()
	close(refs)
	close(errs)
	var ref Reference
	for candidate := range refs {
		ref = candidate
	}
	for archiveErr := range errs {
		t.Fatal(archiveErr)
	}
	if ref.ContentSHA256 != SHA256Hex(body) || ref.ByteLength != int64(len(body)) {
		t.Fatalf("unexpected concurrent archive reference: %+v", ref)
	}
	reader, err := store.Get(context.Background(), ref.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("archived bytes changed: got %q want %q", got, body)
	}

	wanted := []byte("garage expected bytes")
	preseeded := []byte("garage altered bytes!")
	conflictKey := ObjectKey(SHA256Hex(wanted))
	if _, err := store.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(store.bucket), Key: aws.String(conflictKey), Body: bytes.NewReader(preseeded),
		ContentType: aws.String("text/plain"),
	}); err != nil {
		t.Fatalf("pre-seed Garage conflict object: %v", err)
	}
	if _, err := ArchivePayload(context.Background(), store, wanted, "text/plain", nil); !errors.Is(err, ErrContentMismatch) {
		t.Fatalf("Garage accepted conflicting immutable object: %v", err)
	}
	reader, err = store.Get(context.Background(), conflictKey)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	got, err = io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, preseeded) {
		t.Fatalf("Garage conflict object changed: got %q want %q", got, preseeded)
	}
}

func getenvOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
