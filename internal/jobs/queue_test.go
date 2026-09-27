package jobs

import (
	"encoding/json"
	"testing"
)

func TestCanonicalJSONMatchesGoldenFixture(t *testing.T) {
	var job any
	if err := json.Unmarshal([]byte(`{"kind":"risk.run","schema_version":"1.0","idempotency_key":"golden-risk-001","input_snapshot_ids":["snapshot-001","snapshot-002"],"payload":{"scenario_version":"v1","values":{"alpha":1.25,"beta":"2.50"}}}`), &job); err != nil {
		t.Fatal(err)
	}
	data, err := CanonicalJSON(job)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"idempotency_key":"golden-risk-001","input_snapshot_ids":["snapshot-001","snapshot-002"],"kind":"risk.run","payload":{"scenario_version":"v1","values":{"alpha":1.25,"beta":"2.50"}},"schema_version":"1.0"}`
	if string(data) != want {
		t.Fatalf("canonical JSON mismatch: got %s want %s", data, want)
	}
	digest, err := HashCanonicalJSON(job)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "200a097de7bb446df1b0ccb5a7039aed73164f8272c04611ae24ad98d5bec1bb" {
		t.Fatalf("canonical hash mismatch: %s", digest)
	}
}

func TestValidateEnvelopeRejectsDuplicateSnapshots(t *testing.T) {
	err := validateEnvelope(EnqueueRequest{
		Kind: "risk.run", SchemaVersion: SchemaVersion, IdempotencyKey: "k",
		InputSnapshotIDs: []string{"same", "same"}, Payload: map[string]any{},
	})
	if err == nil {
		t.Fatal("duplicate snapshot IDs were accepted")
	}
}
