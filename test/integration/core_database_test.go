package integration

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oplosy/atrisk/internal/platform/database"
)

const testDatabaseEnv = "ATLASRISK_TEST_DATABASE_URL"

func testDatabase(t *testing.T) (*database.Queries, *pgxpool.Pool) {
	t.Helper()
	dsn := os.Getenv(testDatabaseEnv)
	if dsn == "" {
		t.Skipf("%s is not set; refusing to touch a non-isolated database", testDatabaseEnv)
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(strings.ToLower(strings.Trim(u.Path, "/")), "test") {
		t.Fatalf("%s must point to a database whose name contains 'test'", testDatabaseEnv)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	p, err := database.OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open isolated test database: %v", err)
	}
	return database.New(p), p
}

func migrationDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", "..", "db", "migrations"))
}

func migrateTestDatabase(t *testing.T) {
	t.Helper()
	dsn := os.Getenv(testDatabaseEnv)
	if dsn == "" {
		t.Skipf("%s is not set; refusing to migrate a non-isolated database", testDatabaseEnv)
	}
	u, err := url.Parse(dsn)
	if err != nil || !strings.Contains(strings.ToLower(strings.Trim(u.Path, "/")), "test") {
		t.Fatalf("%s must point to a database whose name contains 'test'", testDatabaseEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, dsn, migrationDir(t)); err != nil {
		t.Fatalf("migrate isolated test database: %v", err)
	}
}

func validUUID(t *testing.T, id pgtype.UUID) pgtype.UUID {
	t.Helper()
	if !id.Valid {
		t.Fatal("expected a valid UUID")
	}
	return id
}

func decimal(value string) pgtype.Numeric {
	var n pgtype.Numeric
	if err := n.Scan(value); err != nil {
		panic(fmt.Sprintf("invalid test decimal %q: %v", value, err))
	}
	return n
}

func timestamp(value string) pgtype.Timestamptz {
	timeValue, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return pgtype.Timestamptz{Time: timeValue, Valid: true}
}

func TestCoreDatabaseMigrations(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	var tableCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int FROM information_schema.tables
		WHERE table_schema = 'public'
		  AND table_name IN ('data_sources', 'datasets', 'series', 'instruments',
			'raw_objects', 'observation_revisions', 'price_revisions', 'fx_quote_revisions')`).Scan(&tableCount); err != nil {
		t.Fatalf("inspect migrated tables: %v", err)
	}
	if tableCount != 8 {
		t.Fatalf("expected eight core tables, got %d", tableCount)
	}
	// A second forward migration must be a no-op, proving the version table and
	// migration are safe to run from a previously migrated test database.
	migrateTestDatabase(t)
}

func TestCoreDatabase(t *testing.T) {
	migrateTestDatabase(t)
	queries, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()

	source, err := queries.InsertDataSource(ctx, database.InsertDataSourceParams{
		Code: "integration-core-" + t.Name(), Name: "Core integration source", AdapterVersion: "test-1", Metadata: []byte(`{"fixture":true}`),
	})
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}
	dataset, err := queries.InsertDataset(ctx, database.InsertDatasetParams{
		SourceID: validUUID(t, source.ID), ExternalKey: "dataset-" + t.Name(), Name: "Core dataset", Metadata: []byte(`{}`),
	})
	if err != nil {
		t.Fatalf("insert dataset: %v", err)
	}
	series, err := queries.InsertSeries(ctx, database.InsertSeriesParams{
		DatasetID: validUUID(t, dataset.ID), SourceCode: "SERIES-" + t.Name(), Name: "Fixture series", Unit: "TRY", Frequency: "daily", FreshnessPolicy: []byte(`{"max_age":"P2D"}`),
	})
	if err != nil {
		t.Fatalf("insert series: %v", err)
	}
	instrument, err := queries.InsertInstrument(ctx, database.InsertInstrumentParams{
		CanonicalSymbol: "TEST-" + t.Name(), InstrumentType: "spot", NativeCurrency: "TRY", ExternalIds: []byte(`{}`), Status: "active",
	})
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}

	digest := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())))
	sha := fmt.Sprintf("%x", digest)
	if rows, err := queries.InsertRawObject(ctx, database.InsertRawObjectParams{
		ContentSha256: sha, ObjectKey: "sha256/" + sha, MediaType: "application/json", ByteLength: 13, RetrievedAt: timestamp("2026-01-01T00:00:00Z"), RequestMetadata: []byte(`{"source":"fixture"}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert raw object: rows=%d err=%v", rows, err)
	}
	raw, err := queries.GetRawObjectBySHA256(ctx, sha)
	if err != nil {
		t.Fatalf("get raw object: %v", err)
	}

	observationTime := timestamp("2026-01-01T00:00:00Z")
	sourceTime := timestamp("2026-01-02T00:00:00Z")
	value := decimal("123.456789012345678901")
	seriesID := validUUID(t, series.ID)
	rawID := validUUID(t, raw.ID)
	insertObservation := func(v pgtype.Numeric, known pgtype.Timestamptz, basis string) int64 {
		rows, insertErr := queries.InsertObservationRevision(ctx, database.InsertObservationRevisionParams{
			SeriesID: seriesID, ObservationTime: observationTime, Value: v, SourceKnownAt: known, KnowledgeTimeBasis: basis, RawObjectID: rawID, QualityFlags: []byte(`{"quality":"fresh"}`),
		})
		if insertErr != nil {
			t.Fatalf("insert observation: %v", insertErr)
		}
		return rows
	}
	// PostgreSQL's unique identity is the concurrency boundary: all workers
	// race to insert the same normalized/raw identity, but exactly one wins.
	var wg sync.WaitGroup
	results := make(chan int64, 8)
	errors := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, insertErr := queries.InsertObservationRevision(ctx, database.InsertObservationRevisionParams{
				SeriesID: seriesID, ObservationTime: observationTime, Value: value, SourceKnownAt: sourceTime, KnowledgeTimeBasis: "source_published_at", RawObjectID: rawID, QualityFlags: []byte(`{"quality":"fresh"}`),
			})
			if insertErr != nil {
				errors <- insertErr
				return
			}
			results <- rows
		}()
	}
	wg.Wait()
	close(results)
	close(errors)
	var inserted int64
	for rows := range results {
		inserted += rows
	}
	for insertErr := range errors {
		t.Fatalf("concurrent observation insert: %v", insertErr)
	}
	if inserted != 1 {
		t.Fatalf("concurrent identical identity inserted %d rows, want 1", inserted)
	}
	if got := insertObservation(decimal("123.456789012345678902"), sourceTime, "source_published_at"); got != 1 {
		t.Fatalf("revised observation insert affected %d rows", got)
	}
	if got := insertObservation(decimal("321.000000000000000000"), pgtype.Timestamptz{}, "first_observed_by_system"); got != 1 {
		t.Fatalf("source-unknown observation insert affected %d rows", got)
	}

	var stored string
	if err := pool.QueryRow(ctx, `SELECT value::text FROM observation_revisions WHERE series_id = $1 AND value = $2`, seriesID, value).Scan(&stored); err != nil {
		t.Fatalf("read exact observation: %v", err)
	}
	if stored != "123.456789012345678901" {
		t.Fatalf("exact decimal changed: got %q", stored)
	}

	if rows, err := queries.InsertPriceRevision(ctx, database.InsertPriceRevisionParams{
		InstrumentID: validUUID(t, instrument.ID), QuoteCurrency: "TRY", ObservationTime: observationTime, Price: decimal("98765.432100000000000000"), KnowledgeTimeBasis: "first_observed_by_system", RawObjectID: validUUID(t, raw.ID), QualityFlags: []byte(`{}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert price revision: rows=%d err=%v", rows, err)
	}
	if rows, err := queries.InsertFXQuoteRevision(ctx, database.InsertFXQuoteRevisionParams{
		BaseCurrency: "USD", QuoteCurrency: "TRY", ObservationTime: observationTime, Rate: decimal("34.123456789012345678"), SourceKnownAt: sourceTime, KnowledgeTimeBasis: "source_published_at", RawObjectID: validUUID(t, raw.ID), QualityFlags: []byte(`{}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert FX revision: rows=%d err=%v", rows, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE observation_revisions SET value = 1 WHERE series_id = $1`, seriesID); err == nil {
		t.Fatal("observation update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM observation_revisions WHERE series_id = $1`, seriesID); err == nil {
		t.Fatal("observation delete unexpectedly succeeded")
	}

	var plan string
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire plan connection: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SET enable_seqscan = off`); err != nil {
		t.Fatalf("disable sequential scan for plan assertion: %v", err)
	}
	if err := conn.QueryRow(ctx, `EXPLAIN (FORMAT TEXT) SELECT id FROM observation_revisions WHERE series_id = $1 AND observation_time >= $2 AND observation_time < $3`, seriesID, observationTime, timestamp("2026-01-02T00:00:00Z")).Scan(&plan); err != nil {
		t.Fatalf("explain observation query: %v", err)
	}
	if !strings.Contains(plan, "observation_revisions_series_time_idx") {
		t.Fatalf("observation query did not use intended index: %s", plan)
	}
	assertPlanIndex(t, conn, "observation_revisions_series_system_asof_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM observation_revisions WHERE series_id = $1 AND system_known_at <= $2`, seriesID, timestamp("2026-01-03T00:00:00Z"))
	assertPlanIndex(t, conn, "observation_revisions_series_source_asof_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM observation_revisions WHERE series_id = $1 AND source_known_at IS NOT NULL AND source_known_at <= $2`, seriesID, timestamp("2026-01-03T00:00:00Z"))
}

func assertPlanIndex(t *testing.T, conn *pgxpool.Conn, indexName, statement string, args ...any) {
	t.Helper()
	var plan string
	if err := conn.QueryRow(context.Background(), statement, args...).Scan(&plan); err != nil {
		t.Fatalf("explain query for %s: %v", indexName, err)
	}
	if !strings.Contains(plan, indexName) {
		t.Fatalf("query did not use intended index %s: %s", indexName, plan)
	}
}
