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
	if _, err := pool.Exec(ctx, `INSERT INTO instruments (canonical_symbol, instrument_type, native_currency, external_ids) VALUES ('BTCUSD', 'crypto_spot', 'USD', '{"provider":"binance"}')`); err != nil {
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
	request := binance.KlineRequest{Symbol: "BTCUSD"}
	adapter, err := binance.NewAdapter(client, nil, request)
	if err != nil {
		t.Fatal(err)
	}
	payload := ingestion.RawPayload{Body: body, MediaType: "application/json", Archive: archive.Reference{Key: archive.ObjectKey(digest), ContentSHA256: digest, ByteLength: int64(len(body)), MediaType: "application/json"}}
	records, err := adapter.NormalizeKlines(ctx, "BTCUSD", payload)
	if err != nil {
		t.Fatal(err)
	}
	store := binance.Store{Pool: pool}
	checkpoint := binance.NewKlineCheckpoint(request).Advance(binance.KlinePage{Request: request, Klines: []binance.Kline{{OpenTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, {OpenTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)}}})
	inserted, err := store.PersistPriceRecordsAndCheckpoint(ctx, runID, "BTCUSD", records, checkpoint)
	if err != nil || inserted != 2 {
		t.Fatalf("persisted %d complete Binance prices, want 2: %v", inserted, err)
	}
	if inserted, err = store.PersistPriceRecordsAndCheckpoint(ctx, runID, "BTCUSD", records, checkpoint); err != nil || inserted != 0 {
		t.Fatalf("duplicate Binance page was not idempotent: inserted=%d err=%v", inserted, err)
	}
	var count int
	var missingPublication, qualityEvidence, provenance bool
	if err := pool.QueryRow(ctx, `
SELECT count(*)::int,
       bool_and(source_known_at IS NULL),
       bool_and(quality_flags->>'source_publication_time_unknown' = 'true'),
       bool_and(raw_object_id IN (SELECT id FROM raw_objects WHERE content_sha256 = $1))
FROM price_revisions WHERE instrument_id=(SELECT id FROM instruments WHERE canonical_symbol='BTCUSD')`, digest).Scan(&count, &missingPublication, &qualityEvidence, &provenance); err != nil {
		t.Fatal(err)
	}
	if count != 2 || !missingPublication || !qualityEvidence || !provenance {
		t.Fatalf("Binance price evidence incomplete: count=%d publication_unknown=%v quality=%v provenance=%v", count, missingPublication, qualityEvidence, provenance)
	}
	var rawSHA string
	if err := pool.QueryRow(ctx, `
SELECT r.content_sha256 FROM price_revisions p JOIN raw_objects r ON r.id=p.raw_object_id
WHERE p.instrument_id=(SELECT id FROM instruments WHERE canonical_symbol='BTCUSD') ORDER BY p.observation_time LIMIT 1`).Scan(&rawSHA); err != nil {
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
	wrongRecords, err := adapter.NormalizeKlines(ctx, "ETHUSD", payload)
	if err != nil {
		t.Fatal(err)
	}
	failedCheckpoint := checkpoint
	failedCheckpoint.Pages = 99
	if _, err := store.PersistPriceRecordsAndCheckpoint(ctx, runID, "BTCUSD", wrongRecords, failedCheckpoint); err == nil {
		t.Fatal("mismatched symbol unexpectedly advanced checkpoint")
	}
	if err := pool.QueryRow(ctx, `SELECT (coverage->'pages')::int FROM ingestion_runs WHERE id=$1::uuid`, runID).Scan(&storedPages); err != nil {
		t.Fatal(err)
	}
	if storedPages != 1 {
		t.Fatalf("failed price persistence advanced checkpoint: pages=%d", storedPages)
	}

	statusBody := []byte(`{"symbols":[{"symbol":"BTCUSD","status":"TRADING","baseAsset":"BTC","quoteAsset":"USD","permissions":["SPOT"],"isSpotTradingAllowed":true},{"symbol":"ETHUSD","status":"BREAK","baseAsset":"ETH","quoteAsset":"USD","permissions":["SPOT"],"isSpotTradingAllowed":false}]}`)
	statusDigest := archive.SHA256Hex(statusBody)
	if _, err := pool.Exec(ctx, `INSERT INTO raw_objects (content_sha256, object_key, media_type, byte_length, retrieved_at, request_metadata, ingestion_run_id) VALUES ($1, $2, 'application/json', $3, $4, '{"fixture":"binance-status"}', $5::uuid)`, statusDigest, archive.ObjectKey(statusDigest), len(statusBody), time.Now().UTC(), runID); err != nil {
		t.Fatal(err)
	}
	statusAdapter, err := binance.NewAdapter(client, []string{"BTCUSD", "DOGEUSD"}, binance.KlineRequest{})
	if err != nil {
		t.Fatal(err)
	}
	statusRecords, err := statusAdapter.Normalize(ctx, ingestion.RawPayload{Body: statusBody, MediaType: "application/json", Archive: archive.Reference{Key: archive.ObjectKey(statusDigest), ContentSHA256: statusDigest, ByteLength: int64(len(statusBody)), MediaType: "application/json"}})
	if err != nil {
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
		if item["symbol"] == "DOGEUSD" && item["missing_from_catalog"] == true {
			foundMissing = true
		}
		if item["symbol"] == "ETHUSD" && item["upstream_status"] == "BREAK" {
			foundBreak = true
		}
	}
	if !foundMissing || !foundBreak {
		t.Fatalf("missing/status evidence not durable: missing=%v break=%v", foundMissing, foundBreak)
	}
}
