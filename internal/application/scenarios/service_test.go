package scenarios

import (
	"errors"
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
	if daily["2026-01-02"] != "104" || daily["2026-01-03"] != "103" {
		t.Fatalf("unexpected deterministic day aggregation: %#v", daily)
	}
	provenance := metricPriceRevisionMaps(revisions)
	if len(provenance) != 4 || provenance[0]["id"] != "revision-1" || provenance[3]["id"] != "revision-3" {
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
