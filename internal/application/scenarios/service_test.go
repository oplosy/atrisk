package scenarios

import (
	"errors"
	"testing"
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
