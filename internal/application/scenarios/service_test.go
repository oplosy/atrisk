package scenarios

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestScenariosRequestValidation(t *testing.T) {
	validInput := VersionInput{
		AccountID: "account", SnapshotID: "snapshot", ValuationID: "valuation", Name: "Risk off", TemplateKey: "risk_off",
		IdempotencyKey: "request-1", Units: map[string]any{"yield": "basis_points"},
		Shocks:      map[string]any{"asset_class_returns": map[string]any{"crypto": "-0.40"}},
		Mappings:    map[string]any{},
		Assumptions: map[string]any{"coverage_policy": "block"},
	}
	if !valid(validInput) {
		t.Fatal("expected supported, fully specified template request to validate")
	}
	validInput.TemplateKey = "user_code"
	if valid(validInput) {
		t.Fatal("user-defined scenario template must be rejected")
	}
	if _, err := (Service{}).CreateVersionAndRun(t.Context(), validInput); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected invalid request error, got %v", err)
	}
}

func TestShockValidationRequiresBoundedDecimalStrings(t *testing.T) {
	base := VersionInput{Shocks: map[string]any{
		"asset_class_returns": map[string]any{"crypto": "-0.40"},
		"correlation_target":  "0.75",
	}}
	if !validateShocks(base.Shocks) {
		t.Fatal("expected decimal-string shocks to validate")
	}
	for name, value := range map[string]any{
		"numeric shock":           map[string]any{"asset_class_returns": map[string]any{"crypto": 0.4}},
		"exponent shock":          map[string]any{"correlation_target": "1e-1"},
		"oversized shock":         map[string]any{"correlation_target": "123456789012345678901.0"},
		"out of range target":     map[string]any{"correlation_target": "1.1"},
		"unsupported shock field": map[string]any{"unexpected": "0.1"},
	} {
		if validateShocks(value.(map[string]any)) {
			t.Fatalf("%s unexpectedly validated", name)
		}
	}
}

func TestPreShockMetricsFailClosedWhenAR302HistoryInputsAreUnavailable(t *testing.T) {
	metrics := preShockMetricsUnavailable()
	if metrics["data_quality"] != "blocked" {
		t.Fatalf("expected blocked pre-shock metrics, got %#v", metrics)
	}
	if metrics["reason"] != "PRE_SHOCK_METRICS_INPUT_HISTORY_UNAVAILABLE" {
		t.Fatalf("unexpected pre-shock reason: %#v", metrics["reason"])
	}
	if metrics["metric_engine"] != "AR-302" {
		t.Fatalf("unexpected metric engine: %#v", metrics["metric_engine"])
	}
	missing, ok := metrics["missing_inputs"].([]string)
	if !ok || len(missing) != 3 {
		t.Fatalf("expected explicit missing inputs, got %#v", metrics["missing_inputs"])
	}
}

func TestMetricHistoryPreservesRevisionsAndDeterministicallyAggregatesDays(t *testing.T) {
	firstAt := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	secondAt := firstAt.Add(3 * time.Hour)
	thirdAt := firstAt.Add(24 * time.Hour)
	revisions := []metricPriceRevision{
		{ID: "revision-2", ObservationTime: secondAt, Price: "102", SystemKnownAt: secondAt, KnowledgeTimeBasis: "first_observed_by_system"},
		{ID: "revision-4", ObservationTime: secondAt, Price: "104", SystemKnownAt: secondAt, KnowledgeTimeBasis: "first_observed_by_system"},
		{ID: "revision-1", ObservationTime: firstAt, Price: "100", SystemKnownAt: firstAt, KnowledgeTimeBasis: "first_observed_by_system"},
		{ID: "revision-3", ObservationTime: thirdAt, Price: "103", SystemKnownAt: thirdAt, KnowledgeTimeBasis: "first_observed_by_system"},
	}
	daily := aggregateMetricPriceHistory(revisions)
	if daily["2026-01-02"] != "102" || daily["2026-01-03"] != "103" {
		t.Fatalf("unexpected deterministic day aggregation: %#v", daily)
	}
	provenance := metricPriceRevisionMaps(revisions)
	if len(provenance) != 3 || provenance[0]["id"] != "revision-1" || provenance[2]["id"] != "revision-3" {
		t.Fatalf("revision provenance was not preserved in observation order: %#v", provenance)
	}
	for _, field := range []string{"id", "observation_time", "price", "source_known_at", "system_known_at", "knowledge_time_basis"} {
		if _, ok := provenance[0][field]; !ok {
			t.Fatalf("revision provenance omitted %q: %#v", field, provenance[0])
		}
	}
}

func TestCanonicalSealedInputHashIncludesMetricBundle(t *testing.T) {
	metricInputs := map[string]any{"price_history": map[string]any{"instrument": map[string]any{"2026-01-02": "100"}}}
	first := canonicalSealedInputHash("account", "snapshot", "valuation", "scenario", 1, "content", map[string]any{"state": "valid"}, nil, metricInputs)
	metricInputs["price_history"].(map[string]any)["instrument"].(map[string]any)["2026-01-02"] = "101"
	second := canonicalSealedInputHash("account", "snapshot", "valuation", "scenario", 1, "content", map[string]any{"state": "valid"}, nil, metricInputs)
	if first == second {
		t.Fatal("sealed input hash ignored metric bundle")
	}
}

func TestFXPathMapsRejectMissingRevisionOrDirection(t *testing.T) {
	id := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	if _, err := fxPathMaps([]pgtype.UUID{id}, []string{"forward"}, map[string]string{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected missing FX revision to fail closed, got %v", err)
	}
	if _, err := fxPathMaps([]pgtype.UUID{id}, nil, map[string]string{id.String(): "USD/TRY"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("expected missing FX direction to fail closed, got %v", err)
	}
}

func TestTRYMetricHistoryUsesExplicitDeterministicFXAndFailsClosedWhenMissing(t *testing.T) {
	priceAt := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	systemKnown := priceAt.Add(-time.Hour)
	fxHistory := []metricFXRevision{
		{ID: "fx-later", Pair: "TRY/USD", Direction: "direct", ObservationTime: priceAt.Add(-2 * time.Hour), Rate: "0.031", SystemKnownAt: systemKnown, KnowledgeTimeBasis: "first_observed_by_system"},
		{ID: "fx-earlier", Pair: "TRY/USD", Direction: "direct", ObservationTime: priceAt.Add(-2 * time.Hour), Rate: "0.03", SystemKnownAt: systemKnown, KnowledgeTimeBasis: "first_observed_by_system"},
		{ID: "fx-reverse", Pair: "USD/TRY", Direction: "inverse", ObservationTime: priceAt.Add(-time.Hour), Rate: "32", SystemKnownAt: systemKnown, KnowledgeTimeBasis: "first_observed_by_system"},
	}
	selected := selectMetricFXRevision(fxHistory, priceAt, 7200)
	if selected == nil || selected.ID != "fx-earlier" || selected.Direction != "direct" {
		t.Fatalf("direct FX tie-break or orientation was not deterministic: %#v", selected)
	}
	converted, err := convertMetricPriceToUSD("100", selected.Rate, selected.Direction)
	if err != nil || converted != "3.000000000000000000" {
		t.Fatalf("TRY metric conversion=%q err=%v", converted, err)
	}
	price := metricPriceRevision{ID: "price-1", ObservationTime: priceAt, Price: "100", QuoteCurrency: "TRY", USDPrice: converted, FXPath: []metricFXRevision{*selected}, SourceKnownAt: nil, SystemKnownAt: systemKnown, KnowledgeTimeBasis: "first_observed_by_system"}
	provenance := metricPriceRevisionMaps([]metricPriceRevision{price})
	fxPath, ok := provenance[0]["fx_path"].([]map[string]any)
	if !ok || len(fxPath) != 1 || fxPath[0]["id"] != "fx-earlier" || fxPath[0]["pair"] != "TRY/USD" {
		t.Fatalf("TRY FX provenance missing deterministic path: %#v", provenance)
	}
	for _, field := range []string{"id", "pair", "direction", "observation_time", "rate", "source_known_at", "system_known_at", "knowledge_time_basis"} {
		if _, ok := fxPath[0][field]; !ok {
			t.Fatalf("TRY FX provenance omitted %q: %#v", field, fxPath[0])
		}
	}
	if selectMetricFXRevision(fxHistory, priceAt.Add(-150*time.Minute), 3600) != nil {
		t.Fatal("future-only FX revision was accepted instead of failing closed")
	}
	if selectMetricFXRevision(fxHistory, priceAt, 1800) != nil {
		t.Fatal("stale FX revision was accepted instead of failing closed")
	}
}

func TestCanonicalUUIDNormalizesUppercaseInput(t *testing.T) {
	const lower = "11111111-1111-4111-8111-111111111111"
	canonical, err := canonicalUUID(strings.ToUpper(lower))
	if err != nil || canonical != lower {
		t.Fatalf("uppercase UUID was not canonicalized: %q err=%v", canonical, err)
	}
}

func TestConstantCashHistoryUsesDatedFXWithoutForwardFill(t *testing.T) {
	firstAt := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	history, err := buildCashMetricHistory("TRY", []metricFXRevision{
		{ID: "fx-1", ObservationTime: firstAt, Rate: "30", Direction: "inverse", SystemKnownAt: firstAt},
		{ID: "fx-2", ObservationTime: firstAt.Add(72 * time.Hour), Rate: "32", Direction: "inverse", SystemKnownAt: firstAt},
	}, 3600)
	if err != nil {
		t.Fatal(err)
	}
	prices := aggregateMetricPriceHistory(history)
	if len(prices) != 2 || prices["2026-01-02"] != "0.033333333333333333" || prices["2026-01-05"] != "0.031250000000000000" {
		t.Fatalf("unexpected dated FX cash prices: %#v", prices)
	}
	if _, ok := prices["2026-01-03"]; ok {
		t.Fatal("cash FX history was forward-filled")
	}
}

func TestConstantCashDatesBuildBusinessHistory(t *testing.T) {
	dates := constantCashDates("2026-01-05T00:00:00Z", 64)
	if len(dates) != 64 {
		t.Fatalf("expected 64 business dates, got %d", len(dates))
	}
	if _, ok := dates["2026-01-03"]; ok {
		t.Fatal("constant cash history included a weekend")
	}
}
