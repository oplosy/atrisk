package integration

import (
	"context"
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
	if _, err := pool.Exec(ctx, `INSERT INTO raw_objects (content_sha256, object_key, media_type, byte_length, retrieved_at, request_metadata) VALUES ($1, $2, 'application/json', $3, $4, '{"fixture":"binance"}')`, digest, archive.ObjectKey(digest), len(body), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	client, err := binance.NewClient(binance.Config{BaseURL: binance.DataAPIBaseURL, Now: func() time.Time { return time.Date(2024, 1, 3, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := binance.NewAdapter(client, nil, binance.KlineRequest{Symbol: "BTCUSDT"})
	if err != nil {
		t.Fatal(err)
	}
	payload := ingestion.RawPayload{Body: body, MediaType: "application/json", Archive: archive.Reference{Key: archive.ObjectKey(digest), ContentSHA256: digest, ByteLength: int64(len(body)), MediaType: "application/json"}}
	records, err := adapter.NormalizeKlines(ctx, "BTCUSDT", payload)
	if err != nil {
		t.Fatal(err)
	}
	store := binance.Store{Pool: pool}
	inserted, err := store.PersistPriceRecords(ctx, "BTCUSDT", records)
	if err != nil || inserted != 2 {
		t.Fatalf("persisted %d complete Binance prices, want 2: %v", inserted, err)
	}
	if inserted, err = store.PersistPriceRecords(ctx, "BTCUSDT", records); err != nil || inserted != 0 {
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
	var rawSHA string
	if err := pool.QueryRow(ctx, `
SELECT r.content_sha256 FROM price_revisions p JOIN raw_objects r ON r.id=p.raw_object_id
WHERE p.instrument_id=(SELECT id FROM instruments WHERE canonical_symbol='BTCUSDT') ORDER BY p.observation_time LIMIT 1`).Scan(&rawSHA); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(rawSHA, digest) {
		t.Fatalf("price revision raw provenance SHA=%s, want %s", rawSHA, digest)
	}
	_ = sourceID
}
