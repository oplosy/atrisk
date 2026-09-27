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

func TestCanonicalJSONMatchesPythonForAdversarialValues(t *testing.T) {
	var value any
	if err := json.Unmarshal([]byte(`{"text":"<é>","number":1.0,"nested":{"negative_zero":-0.0}}`), &value); err != nil {
		t.Fatal(err)
	}
	data, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"nested":{"negative_zero":0},"number":1,"text":"<é>"}`
	if string(data) != want {
		t.Fatalf("canonical JSON mismatch: got %s want %s", data, want)
	}
	digest, err := HashCanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "c1e614c0c8faf3d208e0f4b2cd06c4697473b60913c3374dfa2c6802d5dc098c" {
		t.Fatalf("canonical hash mismatch: %s", digest)
	}
}

func TestResultGoldenHashMatchesPython(t *testing.T) {
	result := map[string]any{
		"job_id":             "job-golden-001",
		"schema_version":     "1.0",
		"status":             "succeeded",
		"input_snapshot_ids": []string{"snapshot-001", "snapshot-002"},
		"data_quality":       "healthy",
		"engine_version":     "risk-engine-0.1.0",
		"output":             map[string]any{"metrics": map[string]any{"volatility": 0.1234}, "status": "ok"},
	}
	digest, err := HashCanonicalJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "bb078d46aef3aa371c2fbaa47f4253b6bf3e1532c55c2c10f1ac7355db06aee3" {
		t.Fatalf("result golden hash mismatch: %s", digest)
	}
}
