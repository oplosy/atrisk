package journal

import (
	"testing"
	"time"

	domain "github.com/oplosy/atrisk/internal/domain/journal"
)

func TestDecisionJournalValidation(t *testing.T) {
	request := domain.CreateRequest{
		AccountID:              "00000000-0000-0000-0000-000000000001",
		Thesis:                 "Rates normalize",
		Horizon:                domain.Horizon{Start: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
		RiskBudget:             domain.RiskBudget{Amount: "1000", Currency: "USD", Measure: "absolute_loss", Horizon: "12_months"},
		IntendedAction:         "Hold and review monthly",
		Author:                 "user",
		InvalidationConditions: []domain.Invalidation{{Condition: "Policy rate exceeds 15%", Metric: "policy_rate", Threshold: "15%"}},
	}
	if !validCreate(request) {
		t.Fatal("expected complete journal request to be valid")
	}
	request.InvalidationConditions = nil
	if validCreate(request) {
		t.Fatal("expected missing invalidation conditions to be rejected")
	}
	request.InvalidationConditions = []domain.Invalidation{{Condition: " "}}
	if validCreate(request) {
		t.Fatal("expected blank invalidation condition to be rejected")
	}
}

func TestDecisionJournalDecimalValidation(t *testing.T) {
	for _, value := range []string{"0", "100.50", "-1"} {
		if _, err := parseDecimal(value); err != nil {
			t.Fatalf("parseDecimal(%q): %v", value, err)
		}
	}
	for _, value := range []string{"", "1e3", "NaN", "Infinity", ".5"} {
		if _, err := parseDecimal(value); err == nil {
			t.Fatalf("parseDecimal(%q) unexpectedly succeeded", value)
		}
	}
}
