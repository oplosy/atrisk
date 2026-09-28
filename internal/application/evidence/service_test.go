package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/oplosy/atrisk/internal/archive"
)

type testArchive struct{ content []byte }

func (a testArchive) Put(context.Context, archive.Object) error { return nil }
func (a testArchive) Get(context.Context, string) (io.ReadCloser, error) {
	if a.content == nil {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(a.content)), nil
}

func TestDecisionEvidenceArchiveIntegrity(t *testing.T) {
	content := []byte("immutable source")
	sum := sha256.Sum256(content)
	ref := RawRef{ID: "00000000-0000-0000-0000-000000000001", Key: "source", SHA256: hex.EncodeToString(sum[:])}
	if err := (Service{Archive: testArchive{content}}).verifyRaw(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if err := (Service{Archive: testArchive{[]byte("changed")}}).verifyRaw(context.Background(), ref); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("changed content: %v", err)
	}
	if err := (Service{Archive: testArchive{}}).verifyRaw(context.Background(), ref); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("missing content: %v", err)
	}
}

func TestDecisionEvidenceUUIDValidation(t *testing.T) {
	if !validUUID("00000000-0000-0000-0000-000000000001") {
		t.Fatal("valid id rejected")
	}
	for _, id := range []string{"", "fixture", "00000000X0000-0000-0000-000000000001", "00000000-0000-0000-0000-00000000000g"} {
		if validUUID(id) {
			t.Fatalf("invalid id accepted: %q", id)
		}
	}
}
