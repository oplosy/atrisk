package fred

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/ingestion"
)

func TestFREDNormalizesVintagesMissingValuesAndProvenance(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "fred", "vintage-observations.json"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Config{BaseURL: "https://api.stlouisfed.org", APIKey: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(client, "CPIAUCSL", ObservationRequest{OutputType: 2})
	if err != nil {
		t.Fatal(err)
	}
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	payload := ingestion.RawPayload{
		Body: body, MediaType: "application/json",
		Archive: referenceForTest(digest),
	}
	records, err := adapter.Normalize(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 4 {
		t.Fatalf("normalized %d observations, want 4", len(records))
	}
	first, err := DecodeObservationRecord(records[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.Value == nil || *first.Value != "100.123456789012345678" || first.SourceKnownAt == nil || first.KnowledgeTimeBasis != "source_published_at" {
		t.Fatalf("initial vintage lost exact value or source clock: %+v", first)
	}
	revised, err := DecodeObservationRecord(records[1])
	if err != nil {
		t.Fatal(err)
	}
	if revised.Value == nil || *revised.Value != "101.987654321098765432" || revised.ObservationTime != first.ObservationTime {
		t.Fatalf("revised vintage was not preserved: %+v", revised)
	}
	missing, err := DecodeObservationRecord(records[2])
	if err != nil {
		t.Fatal(err)
	}
	if missing.Value != nil || missing.ValueText == nil || *missing.ValueText != MissingValueMarker || missing.QualityFlags["missing"] != true {
		t.Fatalf("missing FRED marker was coerced or lost: %+v", missing)
	}
	for _, record := range records {
		if record.RawObjectKey != payload.Archive.Key || record.RawObjectSHA256 != payload.Archive.ContentSHA256 {
			t.Fatalf("record provenance mismatch: %+v", record)
		}
	}
}

func TestFREDBuildRequestRedactsAPIKeyAndPreservesVintageBounds(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://api.stlouisfed.org", APIKey: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(client, "GDP", ObservationRequest{RealtimeStart: "2024-01-01", RealtimeEnd: "2024-12-31", VintageDates: "2024-06-01", OutputType: 3, Limit: 500, Offset: 100})
	if err != nil {
		t.Fatal(err)
	}
	request, err := adapter.BuildRequest(context.Background(), ingestion.RunSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(request.URL, "output_type=3") || !strings.Contains(request.URL, "vintage_dates=2024-06-01") || !strings.Contains(request.URL, "offset=100") {
		t.Fatalf("FRED vintage bounds missing from request: %s", request.URL)
	}
	if !strings.Contains(request.URL, "api_key=fixture-secret") {
		t.Fatalf("FRED API key was not supplied to upstream request: %s", request.URL)
	}
	if redacted := archive.RedactedURL(request.URL); strings.Contains(redacted, "fixture-secret") || strings.Contains(redacted, "api_key") {
		t.Fatalf("redacted request leaked API key: %s", redacted)
	}
}

func TestFREDFetchesSeriesMetadata(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fred/series" || r.URL.Query().Get("series_id") != "GDP" || r.URL.Query().Get("file_type") != "json" {
			t.Fatalf("unexpected metadata request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"seriess":[{"id":"GDP","title":"Gross Domestic Product","units":"dollars","frequency":"Quarterly","seasonal_adjustment":"SA","observation_start":"1947-01-01","observation_end":"2025-01-01","realtime_start":"2025-01-01","realtime_end":"2025-12-31","last_updated":"2025-02-01","notes":"fixture"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, APIKey: "fixture-secret", HTTPClient: server.Client(), AllowedHosts: map[string]struct{}{"127.0.0.1": {}}})
	if err != nil {
		t.Fatal(err)
	}
	metadata, response, err := client.FetchSeriesMetadata(context.Background(), "GDP")
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ID != "GDP" || metadata.Title != "Gross Domestic Product" || response.MediaType != "application/json" || strings.Contains(response.RequestURI, "fixture-secret") {
		t.Fatalf("unexpected FRED metadata/provenance: metadata=%+v response=%+v", metadata, response)
	}
}

func TestFREDFetchObservationPagesUsesBoundedOffset(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.URL.Query().Get("api_key") != "fixture-secret" {
			t.Errorf("missing API key")
		}
		w.Header().Set("Content-Type", "application/json")
		if call == 1 {
			_, _ = w.Write([]byte(`{"count":3,"limit":2,"offset":0,"output_type":2,"observations":[{"date":"2024-01-01","value":"1","realtime_start":"2024-01-01"},{"date":"2024-02-01","value":"2","realtime_start":"2024-01-01"}]}`))
			return
		}
		if r.URL.Query().Get("offset") != "2" {
			t.Errorf("second page offset = %q, want 2", r.URL.Query().Get("offset"))
		}
		_, _ = w.Write([]byte(`{"count":3,"limit":2,"offset":2,"output_type":2,"observations":[{"date":"2024-03-01","value":"3","realtime_start":"2024-01-01"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, APIKey: "fixture-secret", HTTPClient: server.Client(), AllowedHosts: map[string]struct{}{"127.0.0.1": {}}, PageSize: 2, MaxPages: 3})
	if err != nil {
		t.Fatal(err)
	}
	pages, err := client.FetchObservationPages(context.Background(), ObservationRequest{SeriesID: "GDP", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || calls.Load() != 2 {
		t.Fatalf("unexpected pages/calls: pages=%d calls=%d", len(pages), calls.Load())
	}
	checkpoint := (ObservationCheckpoint{SeriesID: "GDP"}).Advance(pages[0])
	if checkpoint.NextOffset != 2 || checkpoint.Pages != 1 || checkpoint.Observations != 2 || checkpoint.Completed {
		t.Fatalf("unexpected checkpoint: %+v", checkpoint)
	}
	resumeRequest := checkpoint.NextRequest(ObservationRequest{SeriesID: "wrong", Limit: 2})
	if resumeRequest.SeriesID != "GDP" || resumeRequest.Offset != 2 {
		t.Fatalf("checkpoint did not resume request: %+v", resumeRequest)
	}
	checkpoint = checkpoint.Advance(pages[1])
	if !checkpoint.Completed || checkpoint.NextOffset != 3 {
		t.Fatalf("final checkpoint did not complete: %+v", checkpoint)
	}
}

func TestFREDRejectsInvalidAndMissingValues(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://api.stlouisfed.org", APIKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(client, "GDP", ObservationRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"empty": "", "invalid": "NaN"} {
		t.Run(name, func(t *testing.T) {
			body, _ := json.Marshal(ObservationResponse{Observations: []Observation{{Date: "2024-01-01", Value: value}}})
			if _, err := adapter.Normalize(context.Background(), ingestion.RawPayload{Body: body, MediaType: "application/json", Archive: referenceForTest("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")}); err == nil {
				t.Fatal("invalid FRED value unexpectedly normalized")
			}
		})
	}
}

func referenceForTest(digest string) archive.Reference {
	return archive.Reference{Key: archive.ObjectKey(digest), ContentSHA256: digest, MediaType: "application/json"}
}
