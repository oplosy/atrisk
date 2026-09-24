package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/ingestion"
	"github.com/oplosy/atrisk/internal/sources/binance"
)

func TestBinanceMarketData(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	body, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "binance", "daily-klines.json"))
	if err != nil {
		t.Fatal(err)
	}
	fixtureName := "binance-" + time.Now().UTC().Format("20060102150405.000000000")
	digest := archive.SHA256Hex(body)
	if _, err := pool.Exec(ctx, `INSERT INTO data_sources (code, name, adapter_version) VALUES ($1, 'Binance Spot', $2)`, fixtureName, binance.AdapterVersion); err != nil {
		t.Fatal(err)
	}
	var sourceID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM data_sources WHERE code=$1`, fixtureName).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO instruments (canonical_symbol, instrument_type, native_currency, external_ids) VALUES ('BTCUSDT', 'crypto_spot', 'USDT', '{"provider":"binance"}')`); err != nil {
		t.Fatal(err)
	}
	runStore := ingestion.DatabaseStore{Pool: pool}
	runID, duplicate, err := runStore.StartRun(ctx, ingestion.RunSpec{SourceID: sourceID, IdempotencyKey: fixtureName, AdapterVersion: binance.AdapterVersion})
	if err != nil || duplicate {
		t.Fatalf("start Binance ingestion run: id=%q duplicate=%v err=%v", runID, duplicate, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO raw_objects (content_sha256, object_key, media_type, byte_length, retrieved_at, request_metadata, ingestion_run_id) VALUES ($1, $2, 'application/json', $3, $4, '{"fixture":"binance"}', $5::uuid)`, digest, archive.ObjectKey(digest), len(body), time.Now().UTC(), runID); err != nil {
		t.Fatal(err)
	}
	client, err := binance.NewClient(binance.Config{BaseURL: binance.DataAPIBaseURL, Now: func() time.Time { return time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	request := binance.KlineRequest{Symbol: "BTCUSDT"}
	adapter, err := binance.NewAdapter(client, nil, request)
	if err != nil {
		t.Fatal(err)
	}
	payload := ingestion.RawPayload{Body: body, MediaType: "application/json", Archive: archive.Reference{Key: archive.ObjectKey(digest), ContentSHA256: digest, ByteLength: int64(len(body)), MediaType: "application/json"}}
	records, err := adapter.NormalizeKlines(ctx, "BTCUSDT", payload)
	if err != nil {
		t.Fatal(err)
	}
	store := binance.Store{Pool: pool}
	checkpoint := binance.NewKlineCheckpoint(request).Advance(binance.KlinePage{Request: request, Klines: []binance.Kline{{OpenTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, {OpenTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)}}})
	inserted, err := store.PersistPriceRecordsAndCheckpoint(ctx, runID, "BTCUSDT", records, checkpoint)
	if err != nil || inserted != 2 {
		t.Fatalf("persisted %d complete Binance prices, want 2: %v", inserted, err)
	}
	if inserted, err = store.PersistPriceRecordsAndCheckpoint(ctx, runID, "BTCUSDT", records, checkpoint); err != nil || inserted != 0 {
		t.Fatalf("duplicate Binance page was not idempotent: inserted=%d err=%v", inserted, err)
	}
	var count int
	var missingPublication, qualityEvidence, provenance bool
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int,
       bool_and(source_known_at IS NULL),
       bool_and(quality_flags->>'source_publication_time_unknown' = 'true'),
       bool_and(raw_object_id IN (SELECT id FROM raw_objects WHERE content_sha256 = $1))
FROM price_revisions WHERE instrument_id=(SELECT id FROM instruments WHERE canonical_symbol='BTCUSDT')`, digest).Scan(&count, &missingPublication, &qualityEvidence, &provenance); err != nil {
		t.Fatal(err)
	}
	if count != 2 || !missingPublication || !qualityEvidence || !provenance {
		t.Fatalf("Binance price evidence incomplete: count=%d publication_unknown=%v quality=%v provenance=%v", count, missingPublication, qualityEvidence, provenance)
	}
	var nativeCurrency, quoteCurrency string
	if err := pool.QueryRow(ctx, `
SELECT i.native_currency, p.quote_currency
FROM instruments i JOIN price_revisions p ON p.instrument_id=i.id
WHERE i.canonical_symbol='BTCUSDT' ORDER BY p.observation_time LIMIT 1`).Scan(&nativeCurrency, &quoteCurrency); err != nil {
		t.Fatal(err)
	}
	if nativeCurrency != "USDT" || quoteCurrency != "USDT" {
		t.Fatalf("Binance asset unit was normalized or remapped: native=%q quote=%q", nativeCurrency, quoteCurrency)
	}
	var rawSHA string
	if err := pool.QueryRow(ctx, `
SELECT r.content_sha256 FROM price_revisions p JOIN raw_objects r ON r.id=p.raw_object_id
WHERE p.instrument_id=(SELECT id FROM instruments WHERE canonical_symbol='BTCUSDT') ORDER BY p.observation_time LIMIT 1`).Scan(&rawSHA); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(rawSHA, digest) {
		t.Fatalf("price revision raw provenance SHA=%s, want %s", rawSHA, digest)
	}
	var storedPages int
	if err := pool.QueryRow(ctx, `SELECT (coverage->'pages')::int FROM ingestion_runs WHERE id=$1::uuid`, runID).Scan(&storedPages); err != nil {
		t.Fatal(err)
	}
	if storedPages != 1 {
		t.Fatalf("checkpoint was not stored after accepted prices: pages=%d", storedPages)
	}
	wrongRecords, err := adapter.NormalizeKlines(ctx, "ETHUSDT", payload)
	if err != nil {
		t.Fatal(err)
	}
	failedCheckpoint := checkpoint
	failedCheckpoint.Pages = 99
	if _, err := store.PersistPriceRecordsAndCheckpoint(ctx, runID, "BTCUSDT", wrongRecords, failedCheckpoint); err == nil {
		t.Fatal("mismatched symbol unexpectedly advanced checkpoint")
	}
	if err := pool.QueryRow(ctx, `SELECT (coverage->'pages')::int FROM ingestion_runs WHERE id=$1::uuid`, runID).Scan(&storedPages); err != nil {
		t.Fatal(err)
	}
	if storedPages != 1 {
		t.Fatalf("failed price persistence advanced checkpoint: pages=%d", storedPages)
	}
	unrelatedCheckpoint := binance.NewKlineCheckpoint(binance.KlineRequest{Symbol: "ETHUSDT"})
	unrelatedCheckpoint.Pages = 99
	if _, err := store.PersistPriceRecordsAndCheckpoint(ctx, runID, "BTCUSDT", records, unrelatedCheckpoint); err == nil {
		t.Fatal("unrelated checkpoint was accepted with valid BTCUSDT prices")
	}
	if err := pool.QueryRow(ctx, `SELECT (coverage->'pages')::int FROM ingestion_runs WHERE id=$1::uuid`, runID).Scan(&storedPages); err != nil {
		t.Fatal(err)
	}
	if storedPages != 1 {
		t.Fatalf("unrelated checkpoint changed durable cursor: pages=%d", storedPages)
	}

	statusBody := []byte(`{"symbols":[{"symbol":"BTCUSDT","status":"TRADING","baseAsset":"BTC","quoteAsset":"USDT","permissions":["SPOT"],"isSpotTradingAllowed":true},{"symbol":"ETHUSDT","status":"BREAK","baseAsset":"ETH","quoteAsset":"USDT","permissions":["SPOT"],"isSpotTradingAllowed":false}]}`)
	statusDigest := archive.SHA256Hex(statusBody)
	if _, err := pool.Exec(ctx, `INSERT INTO raw_objects (content_sha256, object_key, media_type, byte_length, retrieved_at, request_metadata, ingestion_run_id) VALUES ($1, $2, 'application/json', $3, $4, '{"fixture":"binance-status"}', $5::uuid)`, statusDigest, archive.ObjectKey(statusDigest), len(statusBody), time.Now().UTC(), runID); err != nil {
		t.Fatal(err)
	}
	statusAdapter, err := binance.NewAdapter(client, []string{"BTCUSDT", "DOGEUSDT"}, binance.KlineRequest{})
	if err != nil {
		t.Fatal(err)
	}
	statusRecords, err := statusAdapter.Normalize(ctx, ingestion.RawPayload{Body: statusBody, MediaType: "application/json", Archive: archive.Reference{Key: archive.ObjectKey(statusDigest), ContentSHA256: statusDigest, ByteLength: int64(len(statusBody)), MediaType: "application/json"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := runStore.CompleteRun(ctx, runID, "succeeded", map[string]any{"records": len(records)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PersistInstrumentRecords(ctx, statusRecords); err != nil {
		t.Fatal(err)
	}
	var evidenceJSON []byte
	if err := pool.QueryRow(ctx, `SELECT coverage->'binance_status_evidence' FROM ingestion_runs WHERE id=$1::uuid`, runID).Scan(&evidenceJSON); err != nil {
		t.Fatal(err)
	}
	var evidence []map[string]any
	if err := json.Unmarshal(evidenceJSON, &evidence); err != nil || len(evidence) != 3 {
		t.Fatalf("status evidence is not append-only/queryable: entries=%d err=%v", len(evidence), err)
	}
	var foundMissing, foundBreak bool
	for _, item := range evidence {
		if item["symbol"] == "DOGEUSDT" && item["missing_from_catalog"] == true {
			foundMissing = true
		}
		if item["symbol"] == "ETHUSDT" && item["upstream_status"] == "BREAK" {
			foundBreak = true
		}
	}
	if !foundMissing || !foundBreak {
		t.Fatalf("missing/status evidence not durable: missing=%v break=%v", foundMissing, foundBreak)
	}
	var completedEvidence []byte
	if err := pool.QueryRow(ctx, `SELECT coverage->'binance_status_evidence' FROM ingestion_runs WHERE id=$1::uuid`, runID).Scan(&completedEvidence); err != nil {
		t.Fatal(err)
	}
	var completedItems []map[string]any
	if err := json.Unmarshal(completedEvidence, &completedItems); err != nil || len(completedItems) != 3 {
		t.Fatalf("completed run discarded status evidence: entries=%d err=%v", len(completedItems), err)
	}
}
