package valuation

import (
	"math/big"
	"testing"
	"time"

	domain "github.com/oplosy/atrisk/internal/domain/valuation"
)

func TestValuationFormatDecimalPreservesEighteenDigits(t *testing.T) {
	value, err := parseDecimal("1.000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if got := formatDecimal(value); got != "1.000000000000000001" {
		t.Fatalf("formatDecimal() = %s", got)
	}
}

func TestValuationPathPrefersDirectAndRecordsReverse(t *testing.T) {
	observation := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	quotes := []fxRevision{
		{id: "bridge-a", base: "EUR", quote: "USD", observation: observation, rate: mustDecimal("1.1")},
		{id: "bridge-b", base: "USD", quote: "TRY", observation: observation, rate: mustDecimal("40")},
		{id: "direct", base: "TRY", quote: "EUR", observation: observation, rate: mustDecimal("0.025")},
	}
	path, err := pathFor("EUR", "TRY", quotes)
	if err != nil || path == nil || len(path.edges) != 1 || path.edges[0].QuoteRevisionID != "direct" || path.edges[0].Direction != "reverse" {
		t.Fatalf("direct reverse path = %#v, err=%v", path, err)
	}
}

func TestValuationPathBlocksAssetTicker(t *testing.T) {
	if _, err := pathFor("USDT", "USD", nil); err == nil {
		t.Fatal("expected asset ticker to be rejected as FX currency")
	}
}

func TestValuationValidRequestRequiresBothClocksAndFreshness(t *testing.T) {
	base := domain.Request{SnapshotID: "snapshot", Cutoff: time.Now(), KnowledgeMode: domain.KnowledgeSystem, KnownAt: time.Now(), PriceMaxAgeSeconds: 1, FXMaxAgeSeconds: 1}
	if !validRequest(base) {
		t.Fatal("expected request to be valid")
	}
	base.KnownAt = time.Time{}
	if validRequest(base) {
		t.Fatal("expected missing knowledge clock to be rejected")
	}
}

func mustDecimal(value string) *big.Rat {
	result, err := parseDecimal(value)
	if err != nil {
		panic(err)
	}
	return result
}
