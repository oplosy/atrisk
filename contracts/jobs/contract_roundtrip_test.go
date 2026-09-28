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
	if len(request.Positions) != 1 || request.Positions[0]["instrument_id"] != "00000000-0000-0000-0000-000000000007" {
		t.Fatalf("generated position model lost object fields: %#v", request.Positions)
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip contracts.RiskRunRequest
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("generated RiskRunRequest round trip failed: %v", err)
	}
	if len(roundTrip.Positions) != len(request.Positions) || roundTrip.Positions[0]["snapshot_line_id"] != request.Positions[0]["snapshot_line_id"] {
		t.Fatalf("generated round trip changed positions: %#v", roundTrip.Positions)
	}
}
