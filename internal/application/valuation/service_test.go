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

func TestValuationTotalsSumPersistedHalfScaleLines(t *testing.T) {
	halfScale := mustDecimal("0.0000000000000000005")
	first := roundedDecimal(halfScale)
	second := roundedDecimal(halfScale)
	total := new(big.Rat).Add(first, second)
	if got := formatDecimal(first); got != "0.000000000000000001" {
		t.Fatalf("first persisted value=%s", got)
	}
	if got := formatDecimal(second); got != "0.000000000000000001" {
		t.Fatalf("second persisted value=%s", got)
	}
	if got := formatDecimal(total); got != "0.000000000000000002" {
		t.Fatalf("persisted total=%s", got)
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
	if _, err := pathFor("BTC", "USD", nil); err == nil {
		t.Fatal("expected asset ticker to be rejected as FX currency")
	}
}

func TestValuationPathRejectsNonPositiveRate(t *testing.T) {
	if _, err := pathFor("EUR", "USD", []fxRevision{{id: "bad", base: "EUR", quote: "USD", rate: mustDecimal("0")}}); err == nil {
		t.Fatal("expected non-positive FX rate to be rejected")
	}
}

func TestValuationValidRequestRequiresBothClocksAndFreshness(t *testing.T) {
	base := domain.Request{SnapshotID: "00000000-0000-0000-0000-000000000001", Cutoff: time.Now(), KnowledgeMode: domain.KnowledgeSystem, KnownAt: time.Now(), PriceMaxAgeSeconds: 1, FXMaxAgeSeconds: 1}
	if !validRequest(base) {
		t.Fatal("expected request to be valid")
	}
	base.KnownAt = time.Time{}
	if validRequest(base) {
		t.Fatal("expected missing knowledge clock to be rejected")
	}
	base.KnownAt = time.Now()
	base.PriceMaxAgeSeconds = maxAgeSeconds + 1
	if validRequest(base) {
		t.Fatal("expected price freshness overflow to be rejected")
	}
	base.PriceMaxAgeSeconds = 1
	base.FXMaxAgeSeconds = maxAgeSeconds + 1
	if validRequest(base) {
		t.Fatal("expected FX freshness overflow to be rejected")
	}
}

func TestValuationPointInTimeSelectorRejectsFutureStaleAndUnknown(t *testing.T) {
	cutoff := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	knownAt := cutoff.Add(time.Hour)
	knownSystem := cutoff.Add(-time.Minute)
	if eligibleRevision(cutoff.Add(time.Minute), nil, knownSystem, domain.KnowledgeSystem, cutoff, knownAt, 3600) {
		t.Fatal("future revision was eligible")
	}
	if eligibleRevision(cutoff.Add(-2*time.Hour), nil, knownSystem, domain.KnowledgeSystem, cutoff, knownAt, 3600) {
		t.Fatal("stale revision was eligible")
	}
	if eligibleRevision(cutoff, nil, knownAt.Add(time.Minute), domain.KnowledgeSystem, cutoff, knownAt, 3600) {
		t.Fatal("not-yet-known system revision was eligible")
	}
	if eligibleRevision(cutoff, nil, knownSystem, domain.KnowledgeSource, cutoff, knownAt, 3600) {
		t.Fatal("source-as-of selector fell back to system time")
	}
}

func TestValuationPointInTimeSelectorRequiresSourceClock(t *testing.T) {
	cutoff := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	sourceKnown := cutoff.Add(-time.Minute)
	if !eligibleRevision(cutoff, &sourceKnown, cutoff, domain.KnowledgeSource, cutoff, cutoff.Add(time.Hour), 3600) {
		t.Fatal("eligible source-known revision was rejected")
	}
}

func mustDecimal(value string) *big.Rat {
	result, err := parseDecimal(value)
	if err != nil {
		panic(err)
	}
	return result
}
