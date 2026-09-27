package scenarios

import (
	"errors"
	"testing"
)

func TestScenariosRequestValidation(t *testing.T) {
	validInput := VersionInput{
		AccountID: "account", SnapshotID: "snapshot", Name: "Risk off", TemplateKey: "risk_off",
		IdempotencyKey: "request-1", Units: map[string]any{"yield": "basis_points"},
		Shocks:      map[string]any{"asset_class_returns": map[string]any{"crypto": "-0.40"}},
		Mappings:    map[string]any{},
		Assumptions: map[string]any{"coverage_policy": "block"},
		Positions:   []map[string]any{},
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
