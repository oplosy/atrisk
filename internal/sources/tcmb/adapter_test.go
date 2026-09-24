package tcmb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/ingestion"
)

func TestTCMBNormalizesLocaleMissingAndProvenance(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "tcmb", "evds-observations.json"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Config{BaseURL: "https://evds2.tcmb.gov.tr", APIKey: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(client, SeriesRequest{Series: []string{"TP.DK.USD.A", "TP.FAIZ.O1"}, StartDate: "02-01-2024", EndDate: "08-01-2024", DecimalSeparator: ",", Frequency: "1", AggregationTypes: "avg"})
	if err != nil {
		t.Fatal(err)
	}
	digest := strings.Repeat("a", 64)
	records, err := adapter.Normalize(context.Background(), ingestion.RawPayload{Body: body, MediaType: "application/json", Archive: referenceForTest(digest)})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 10 {
		t.Fatalf("normalized %d observations, want 10", len(records))
	}
	first, err := DecodeObservationRecord(records[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.Value == nil || *first.Value != "29.4381" || first.SourceKnownAt != nil || first.KnowledgeTimeBasis != "first_observed_by_system" {
		t.Fatalf("locale or source-clock handling was incorrect: %+v", first)
	}
	if first.QualityFlags["source_publication_time"] != "unavailable_in_evds2_response" || first.QualityFlags["retrieval_time_not_publication"] != true {
		t.Fatalf("publication-time basis was not explicit: %+v", first.QualityFlags)
	}
	missing, err := DecodeObservationRecord(records[2])
	if err != nil {
		t.Fatal(err)
	}
	if missing.Value != nil || missing.ValueText == nil || *missing.ValueText != "null" || missing.QualityFlags["missing"] != true {
		t.Fatalf("missing EVDS value was coerced or lost: %+v", missing)
	}
	for _, record := range records {
		if record.RawObjectKey != referenceForTest(digest).Key || record.RawObjectSHA256 != digest {
			t.Fatalf("record provenance mismatch: %+v", record)
		}
	}
}

func TestTCMBBuildRequestUsesHeaderKeyAndOfficialMetadata(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://evds2.tcmb.gov.tr", APIKey: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(client, SeriesRequest{Series: []string{"TP.DK.USD.A", "TP.DK.EUR.A"}, StartDate: "02-01-2024", EndDate: "31-12-2024", Frequency: "5", AggregationTypes: "avg-avg", Formulas: "0-0", DecimalSeparator: ","})
	if err != nil {
		t.Fatal(err)
	}
	request, err := adapter.BuildRequest(context.Background(), ingestion.RunSpec{})
	if err != nil {
		t.Fatal(err)
	}
	if request.Headers.Get("key") != "fixture-secret" || strings.Contains(request.URL, "fixture-secret") || strings.Contains(request.URL, "api_key") {
		t.Fatalf("EVDS key was not confined to request header: url=%s headers=%v", request.URL, request.Headers)
	}
	for _, wanted := range []string{"/service/evds/series=TP.DK.USD.A-TP.DK.EUR.A&startDate=02-01-2024&endDate=31-12-2024&type=json", "frequency=5", "aggregationTypes=avg-avg", "formulas=0-0", "decimalSeperator=%2C"} {
		if !strings.Contains(request.URL, wanted) {
			t.Fatalf("request missing %q: %s", wanted, request.URL)
		}
	}
	if redacted := archive.RedactedURL(request.URL); strings.Contains(redacted, "fixture-secret") || strings.Contains(redacted, "key") {
		t.Fatalf("redacted EVDS request leaked credential: %s", redacted)
	}
}

func TestTCMBFetchesJSONResponseAndCheckpoint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/service/evds/series=TP.DK.USD.A&startDate=02-01-2024&endDate=03-01-2024&type=json&decimalSeperator=." || r.Header.Get("key") != "fixture-secret" {
			t.Fatalf("unexpected EVDS request: %s headers=%v", r.URL.String(), r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalCount":"2","items":[{"Tarih":"02-01-2024","TP_DK_USD_A":"29.4"},{"Tarih":"03-01-2024","TP_DK_USD_A":"29.5"}]}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{BaseURL: server.URL, APIKey: "fixture-secret", HTTPClient: server.Client(), AllowedHosts: map[string]struct{}{"127.0.0.1": {}}})
	if err != nil {
		t.Fatal(err)
	}
	request := SeriesRequest{Series: []string{"TP.DK.USD.A"}, StartDate: "02-01-2024", EndDate: "03-01-2024"}
	response, fetched, err := client.FetchSeries(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.TotalCount != 2 || len(response.Items) != 2 || strings.Contains(fetched.RequestURI, "fixture-secret") {
		t.Fatalf("unexpected EVDS response/provenance: response=%+v fetched=%+v", response, fetched)
	}
	checkpoint, err := NewObservationCheckpoint(request, client.pageSize)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint = checkpoint.Advance(SeriesPage{Response: response})
	if checkpoint.NextStartDate != "04-01-2024" || !checkpoint.Completed || checkpoint.Observations != 2 {
		t.Fatalf("checkpoint did not advance deterministically: %+v", checkpoint)
	}
	if _, err := checkpoint.NextRequest(SeriesRequest{Series: []string{"TP.DK.USD.A"}, StartDate: "02-01-2024", EndDate: "03-01-2024"}, client.pageSize); err != nil {
		t.Fatalf("completed checkpoint rejected original request: %v", err)
	}
	if _, err := checkpoint.NextRequest(SeriesRequest{Series: []string{"TP.DK.USD.A"}, StartDate: "02-01-2024", EndDate: "03-01-2024", Frequency: "5"}, client.pageSize); err != ErrCheckpointRequestMismatch {
		t.Fatalf("changed EVDS request was accepted: %v", err)
	}
}

func TestTCMBCheckpointAdvancesSourceFrequencyBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, frequency, sourceDate, want string
	}{
		{name: "monthly", frequency: "5", sourceDate: "2024-12", want: "01-01-2025"},
		{name: "quarterly", frequency: "6", sourceDate: "2024-Q4", want: "01-01-2025"},
		{name: "yearly", frequency: "8", sourceDate: "2024", want: "01-01-2025"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := SeriesRequest{Series: []string{"TP.TEST"}, StartDate: "01-01-2024", EndDate: "31-12-2025", Frequency: test.frequency}
			checkpoint, err := NewObservationCheckpoint(request, 1000)
			if err != nil {
				t.Fatal(err)
			}
			checkpoint = checkpoint.Advance(SeriesPage{Response: SeriesResponse{Items: []map[string]json.RawMessage{{"Tarih": json.RawMessage(`"` + test.sourceDate + `"`)}}}})
			if checkpoint.NextStartDate != test.want {
				t.Fatalf("next source period = %q, want %q", checkpoint.NextStartDate, test.want)
			}
		})
	}
}

func TestTCMBRejectsAmbiguousNumbersAndDates(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://evds2.tcmb.gov.tr", APIKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(client, SeriesRequest{Series: []string{"TP.DK.USD.A"}, StartDate: "02-01-2024", EndDate: "02-01-2024"})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"items": []map[string]any{{"Tarih": "02-01-2024", "TP_DK_USD_A": "1,234.5"}}})
	if _, err := adapter.Normalize(context.Background(), ingestion.RawPayload{Body: body, MediaType: "application/json", Archive: referenceForTest(strings.Repeat("b", 64))}); err == nil {
		t.Fatal("ambiguous comma number unexpectedly normalized")
	}
	badRequest := SeriesRequest{Series: []string{"TP.DK.USD.A"}, StartDate: "2024-01-02", EndDate: "02-01-2024"}
	if _, err := NewAdapter(client, badRequest); err == nil {
		t.Fatal("non-EVDS date format unexpectedly accepted")
	}
}

func TestTCMBMarksLateCoverageWithoutInventingValues(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://evds2.tcmb.gov.tr", APIKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(client, SeriesRequest{Series: []string{"TP.DK.USD.A"}, StartDate: "02-01-2024", EndDate: "05-01-2024"})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"totalCount":1,"items":[{"Tarih":"02-01-2024","TP_DK_USD_A":"29.4"}]}`)
	records, err := adapter.Normalize(context.Background(), ingestion.RawPayload{Body: body, MediaType: "application/json", Archive: referenceForTest(strings.Repeat("c", 64))})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeObservationRecord(records[0])
	if err != nil {
		t.Fatal(err)
	}
	if decoded.QualityFlags["late"] != true || decoded.Value == nil || *decoded.Value == "0" {
		t.Fatalf("late coverage was not explicit or value was fabricated: %+v", decoded)
	}
}

func referenceForTest(digest string) archive.Reference {
	return archive.Reference{Key: archive.ObjectKey(digest), ContentSHA256: digest, MediaType: "application/json"}
}
