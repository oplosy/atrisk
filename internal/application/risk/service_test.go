package risk

import (
	"encoding/json"
	"testing"
)

func TestMapStatusPreservesWorkerLifecycle(t *testing.T) {
	for job, want := range map[string]string{"queued": "queued", "running": "running", "retryable_failed": "retryable", "failed": "permanent", "cancelled": "cancelled", "succeeded": "completed"} {
		if got := mapStatus(job, "queued"); got != want {
			t.Fatalf("mapStatus(%q)=%q, want %q", job, got, want)
		}
	}
}

func TestMapQualityDoesNotPromoteBlockedOutput(t *testing.T) {
	blocked := []byte(`{"data_quality":"blocked","output":{"state":"blocked"}}`)
	if got := mapQuality("blocked", blocked, nil); got != "blocked" {
		t.Fatalf("quality=%q", got)
	}
	if got := mapQuality("valid", blocked, nil); got != "blocked" {
		t.Fatalf("quality=%q", got)
	}
}

func TestReasonCodesExtractFromImmutableResult(t *testing.T) {
	raw := json.RawMessage(`{"output":{"positions":[{"reason_codes":["PRICE_STALE","FX_MISSING"]},{"reason_codes":["PRICE_STALE"]}]}}`)
	got := reasonCodes(raw, "")
	if len(got) != 2 {
		t.Fatalf("reason codes=%v", got)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	const value = "00000000-0000-0000-0000-000000000001"
	cursor := encodeCursor(value)
	decoded, err := decodeCursor(cursor)
	if err != nil || decoded != value {
		t.Fatalf("cursor=%q decoded=%q err=%v", cursor, decoded, err)
	}
	if _, err := decodeCursor("!"); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}
