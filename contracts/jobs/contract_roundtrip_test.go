package contractstest

import (
	"encoding/json"
	"os"
	"testing"

	contracts "github.com/oplosy/atrisk/contracts/generated/go"
)

func TestGeneratedRiskRunRequestRoundTrip(t *testing.T) {
	data, err := os.ReadFile("risk-request.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var request contracts.RiskRunRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatalf("generated RiskRunRequest rejected golden payload: %v", err)
	}
	if request.ValuationID != "00000000-0000-0000-0000-000000000005" {
		t.Fatalf("generated valuation binding was lost: %#v", request.ValuationID)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip contracts.RiskRunRequest
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("generated RiskRunRequest round trip failed: %v", err)
	}
	if roundTrip.ValuationID != request.ValuationID {
		t.Fatalf("generated round trip changed valuation binding: %#v", roundTrip.ValuationID)
	}
}
