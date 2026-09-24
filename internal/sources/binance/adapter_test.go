package binance

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/ingestion"
)

func TestBinanceBuildsOnlySpotMetadataAndDailyKlineRequests(t *testing.T) {
	client, err := NewClient(Config{BaseURL: "https://data-api.binance.vision"})
	if err != nil {
		t.Fatal(err)
	}
	exchange, err := client.BuildExchangeInfoRequest()
	if err != nil {
		t.Fatal(err)
	}
	assertRequest(t, exchange.URL, "/api/v3/exchangeInfo", map[string]string{"permissions": "SPOT"})
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	kline, err := client.BuildKlineRequest(KlineRequest{Symbol: "btcusdt", StartTime: &start, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	assertRequest(t, kline.URL, "/api/v3/klines", map[string]string{"interval": "1d", "symbol": "BTCUSDT", "limit": "10", "startTime": "1704067200000"})
	if strings.Contains(kline.URL, "key=") || strings.Contains(kline.URL, "api") && strings.Contains(kline.URL, "auth") {
		t.Fatalf("request contains unsupported credentials: %s", kline.URL)
	}
}

func TestBinanceNormalizesExactDailyPricesAndExcludesCurrentCandle(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "binance", "daily-klines.json"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Config{BaseURL: "https://data-api.binance.vision", Now: func() time.Time { return time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(client, nil, KlineRequest{Symbol: "BTCUSDT"})
	if err != nil {
		t.Fatal(err)
	}
	digest := archive.SHA256Hex(body)
	records, err := adapter.NormalizeKlines(context.Background(), "BTCUSDT", ingestion.RawPayload{Body: body, MediaType: "application/json", Archive: archive.Reference{Key: archive.ObjectKey(digest), ContentSHA256: digest, MediaType: "application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("normalized %d complete candles, want 2", len(records))
	}
	first, err := DecodePriceRecord(records[0])
	if err != nil {
		t.Fatal(err)
	}
	if first.Price != "42500.123456789012345678" || !first.ObservationTime.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) || first.SourceKnownAt != nil || first.KnowledgeTimeBasis != firstObservedBasis {
		t.Fatalf("exact price or three-clock semantics lost: %+v", first)
	}
	if first.QualityFlags["source_publication_time_unknown"] != true {
		t.Fatalf("missing publication evidence was not explicit: %+v", first.QualityFlags)
	}
	if records[0].RawObjectSHA256 != digest {
		t.Fatal("raw provenance was not preserved")
	}
}

func TestBinanceExchangeInfoMakesMissingAndStatusChangesObservable(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "fixtures", "binance", "exchange-info.json"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(Config{BaseURL: "https://data-api.binance.vision"})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewAdapter(client, []string{"BTCUSDT", "DOGEUSDT"}, KlineRequest{})
	if err != nil {
		t.Fatal(err)
	}
	digest := archive.SHA256Hex(body)
	records, err := adapter.Normalize(context.Background(), ingestion.RawPayload{Body: body, MediaType: "application/json", Archive: archive.Reference{Key: archive.ObjectKey(digest), ContentSHA256: digest, MediaType: "application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("normalized %d catalog records, want 3", len(records))
	}
	var foundMissing, foundBreak bool
	for _, normalized := range records {
		record, decodeErr := DecodeInstrumentRecord(normalized)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if record.MissingFromCatalog {
			foundMissing = record.Symbol == "DOGEUSDT" && record.QualityFlags["requested_symbol_missing"] == true
		}
		if record.Symbol == "ETHUSDT" {
			foundBreak = record.UpstreamStatus == "BREAK" && record.LifecycleStatus == "inactive" && record.QualityFlags["upstream_status_changed"] == true
		}
	}
	if !foundMissing || !foundBreak {
		t.Fatalf("missing/status evidence not preserved: missing=%v break=%v", foundMissing, foundBreak)
	}
}

func TestBinanceCheckpointAdvancesAtDailyBoundary(t *testing.T) {
	start := time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC)
	request := KlineRequest{Symbol: "BTCUSDT", StartTime: &start, Limit: 2}
	checkpoint := NewKlineCheckpoint(request)
	page := KlinePage{Request: request, Klines: []Kline{{OpenTime: start}, {OpenTime: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)}}}
	checkpoint = checkpoint.Advance(page)
	if checkpoint.NextStartTime == nil || !checkpoint.NextStartTime.Equal(time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily checkpoint did not advance by one UTC period: %+v", checkpoint)
	}
	resumed, err := checkpoint.NextRequest(request, 2)
	if err != nil || resumed.StartTime == nil || !resumed.StartTime.Equal(*checkpoint.NextStartTime) {
		t.Fatalf("checkpoint did not resume exact daily boundary: %+v err=%v", resumed, err)
	}
}

func TestBinanceCheckpointLeavesIncompleteCurrentCandleAsRestartBoundary(t *testing.T) {
	cutoff := time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC)
	request := KlineRequest{Symbol: "BTCUSDT", Limit: 3}
	page := KlinePage{Request: request, Klines: []Kline{
		{OpenTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), CloseTime: time.Date(2024, 1, 1, 23, 59, 59, 999000000, time.UTC)},
		{OpenTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), CloseTime: time.Date(2024, 1, 2, 23, 59, 59, 999000000, time.UTC)},
		{OpenTime: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), CloseTime: time.Date(2024, 1, 3, 23, 59, 59, 999000000, time.UTC)},
	}}
	checkpoint := NewKlineCheckpoint(request).AdvanceComplete(page, cutoff)
	if checkpoint.Completed || checkpoint.Candles != 2 || checkpoint.NextStartTime == nil || !checkpoint.NextStartTime.Equal(time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("incomplete current candle advanced incorrectly: %+v", checkpoint)
	}
	if err := checkpoint.ValidateForSymbol("BTCUSDT"); err != nil {
		t.Fatalf("valid checkpoint rejected: %v", err)
	}
	if err := checkpoint.ValidateForSymbol("ETHUSDT"); !errors.Is(err, ErrCheckpointMismatch) {
		t.Fatalf("unrelated symbol accepted checkpoint: %v", err)
	}
}

func TestBinanceFetchesOnlyAllowlistedPaths(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/v3/exchangeInfo" || r.URL.Query().Get("permissions") != "SPOT" || len(r.URL.Query()) != 1 {
			t.Fatalf("unexpected Binance request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"symbols":[]}`))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	client, err := NewClient(Config{BaseURL: server.URL, HTTPClient: server.Client(), AllowedHosts: map[string]struct{}{parsed.Hostname(): {}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.FetchExchangeInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one exchange info request, got %d", calls.Load())
	}
}

func assertRequest(t *testing.T, raw, path string, expected map[string]string) {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != path || len(parsed.Query()) != len(expected) {
		t.Fatalf("request path/query mismatch: %s", raw)
	}
	for key, value := range expected {
		if parsed.Query().Get(key) != value {
			t.Fatalf("request %s=%q, want %q: %s", key, parsed.Query().Get(key), value, raw)
		}
	}
}
