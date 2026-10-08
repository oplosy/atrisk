package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
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
	if !validUUID("ABCDEFAB-CDEF-ABCD-EFAB-CDEFABCDEFAB") {
		t.Fatal("uppercase hexadecimal id rejected")
	}
	for _, id := range []string{"", "fixture", "00000000X0000-0000-0000-000000000001", "00000000-0000-0000-0000-00000000000g"} {
		if validUUID(id) {
			t.Fatalf("invalid id accepted: %q", id)
		}
	}
}

func TestCanonicalUUIDPreservesSnapshotBinding(t *testing.T) {
	upper := "ABCDEFAB-CDEF-ABCD-EFAB-CDEFABCDEFAB"
	canonical, ok := canonicalUUID(upper)
	if !ok {
		t.Fatal("uppercase hexadecimal id rejected")
	}
	lower, ok := canonicalUUID(strings.ToLower(upper))
	if !ok || canonical != lower {
		t.Fatalf("uppercase and lowercase UUIDs produced different bindings: %q vs %q", canonical, lower)
	}
}

func TestMetricUSDConversionRequiresMatchingFXPath(t *testing.T) {
	base := func(currency, price, usd string) map[string]any {
		return map[string]any{"price_quote_currency": currency, "price": price, "usd_price": usd}
	}
	if err := validateMetricUSDConversion(base("USD", "100", "100.000000000000000000"), nil); err != nil {
		t.Fatalf("USD price without FX path rejected: %v", err)
	}
	if err := validateMetricUSDConversion(base("USD", "100", "100"), []map[string]any{{"pair": "USD/JPY", "direction": "direct", "rate": "150"}}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("unrelated USD FX path accepted: %v", err)
	}
	if err := validateMetricUSDConversion(base("TRY", "100", "3.333333333333333333"), nil); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("missing TRY to USD FX path accepted: %v", err)
	}
	if err := validateMetricUSDConversion(base("TRY", "100", "3.333333333333333333"), []map[string]any{{"pair": "USD/JPY", "direction": "inverse", "rate": "30"}}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("unrelated TRY FX path accepted: %v", err)
	}
	if err := validateMetricUSDConversion(base("TRY", "100", "3.333333333333333333"), []map[string]any{{"pair": "USD/TRY", "direction": "inverse", "rate": "30"}}); err != nil {
		t.Fatalf("valid inverse TRY to USD path rejected: %v", err)
	}
	if err := validateMetricUSDConversion(base("TRY", "100", "3000.000000000000000000"), []map[string]any{{"pair": "TRY/USD", "direction": "direct", "rate": "30"}}); err != nil {
		t.Fatalf("valid direct TRY to USD path rejected: %v", err)
	}
}
