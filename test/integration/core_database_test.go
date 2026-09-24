package integration

import (
	"context"
	"crypto/sha256"
	"fmt"
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
const requireTestDatabaseEnv = "ATLASRISK_REQUIRE_TEST_DATABASE"

func isolatedTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(testDatabaseEnv)
	if err := database.ValidateIsolatedTestDatabaseURL(dsn); err != nil {
		if os.Getenv(requireTestDatabaseEnv) == "1" {
			t.Fatalf("isolated database validation failed: %v", err)
		}
		t.Skipf("isolated database unavailable: %v", err)
	}
	return dsn
}

func testDatabase(t *testing.T) (*database.Queries, *pgxpool.Pool) {
	t.Helper()
	dsn := isolatedTestDSN(t)

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
	dsn := isolatedTestDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, dsn, migrationDir(t)); err != nil {
		t.Fatalf("migrate isolated test database: %v", err)
	}
}

func TestTestDatabaseDSNValidation(t *testing.T) {
	valid := "postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?sslmode=disable"
	if err := database.ValidateIsolatedTestDatabaseURL(valid); err != nil {
		t.Fatalf("valid isolated DSN rejected: %v", err)
	}
	unsafe := []string{
		"",
		"postgres://test-user:test-password@127.0.0.1:55432/contest",
		"postgres://test-user:test-password@127.0.0.1:55432/latest",
		"postgres://test-user:test-password@db.example.test:55432/atrisk_test",
		"postgres://test-user:test-password/atrisk_test",
		"postgres://127.0.0.1:55432/atrisk_test",
		"not-a-dsn",
	}
	for _, dsn := range unsafe {
		if err := database.ValidateIsolatedTestDatabaseURL(dsn); err == nil {
			t.Errorf("unsafe DSN was accepted")
		}
		if err := database.Migrate(context.Background(), dsn, migrationDir(t)); err == nil {
			t.Errorf("migration entrypoint accepted unsafe DSN")
		}
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
	var currentVersion int64
	if err := pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&currentVersion); err != nil {
		t.Fatalf("inspect migration version: %v", err)
	}
	if currentVersion != 2 {
		t.Fatalf("expected hardening migration version 2, got %d", currentVersion)
	}
	var compositeForeignKeys int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM pg_constraint WHERE conname = 'ingestion_runs_source_dataset_fk'`).Scan(&compositeForeignKeys); err != nil {
		t.Fatalf("inspect source/dataset foreign key: %v", err)
	}
	if compositeForeignKeys != 1 {
		t.Fatalf("expected source/dataset composite foreign key, got %d", compositeForeignKeys)
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
	fixtureName := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())

	if rows, err := queries.InsertDataSource(ctx, database.InsertDataSourceParams{
		Code: "integration-core-" + fixtureName, Name: "Core integration source", AdapterVersion: "test-1", Metadata: []byte(`{"fixture":true}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert source: rows=%d err=%v", rows, err)
	}
	sourceCode := "integration-core-" + fixtureName
	source, err := queries.GetDataSourceByCode(ctx, sourceCode)
	if err != nil {
		t.Fatalf("get source: %v", err)
	}
	if rows, err := queries.InsertDataSource(ctx, database.InsertDataSourceParams{
		Code: sourceCode, Name: "rewritten source", AdapterVersion: "attacker-version", Metadata: []byte(`{"rewritten":true}`),
	}); err != nil || rows != 0 {
		t.Fatalf("changed source metadata was not idempotently rejected: rows=%d err=%v", rows, err)
	}
	source, err = queries.GetDataSourceByCode(ctx, sourceCode)
	if err != nil || source.Name != "Core integration source" || source.AdapterVersion != "test-1" {
		t.Fatalf("source metadata was overwritten: source=%+v err=%v", source, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE data_sources SET name = 'direct rewrite' WHERE code = $1`, sourceCode); err == nil {
		t.Fatal("direct source metadata update unexpectedly succeeded")
	}
	externalKey := "dataset-" + fixtureName
	if rows, err := queries.InsertDataset(ctx, database.InsertDatasetParams{
		SourceID: validUUID(t, source.ID), ExternalKey: externalKey, Name: "Core dataset", Metadata: []byte(`{}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert dataset: rows=%d err=%v", rows, err)
	}
	dataset, err := queries.GetDatasetByExternalKey(ctx, database.GetDatasetByExternalKeyParams{
		SourceID: validUUID(t, source.ID), ExternalKey: externalKey,
	})
	if err != nil {
		t.Fatalf("get dataset: %v", err)
	}
	if rows, err := queries.InsertDataset(ctx, database.InsertDatasetParams{
		SourceID: validUUID(t, source.ID), ExternalKey: externalKey, Name: "rewritten dataset", Metadata: []byte(`{"rewritten":true}`),
	}); err != nil || rows != 0 {
		t.Fatalf("changed dataset metadata was not idempotently rejected: rows=%d err=%v", rows, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE datasets SET name = 'direct rewrite' WHERE id = $1`, validUUID(t, dataset.ID)); err == nil {
		t.Fatal("direct dataset metadata update unexpectedly succeeded")
	}
	sourceCodeForSeries := "SERIES-" + fixtureName
	if rows, err := queries.InsertSeries(ctx, database.InsertSeriesParams{
		DatasetID: validUUID(t, dataset.ID), SourceCode: sourceCodeForSeries, Name: "Fixture series", Unit: "TRY", Frequency: "daily", FreshnessPolicy: []byte(`{"max_age":"P2D"}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert series: rows=%d err=%v", rows, err)
	}
	series, err := queries.GetSeriesBySourceCode(ctx, database.GetSeriesBySourceCodeParams{
		DatasetID: validUUID(t, dataset.ID), SourceCode: sourceCodeForSeries,
	})
	if err != nil {
		t.Fatalf("get series: %v", err)
	}
	if rows, err := queries.InsertSeries(ctx, database.InsertSeriesParams{
		DatasetID: validUUID(t, dataset.ID), SourceCode: sourceCodeForSeries, Name: "rewritten series", Unit: "USD", Frequency: "monthly", FreshnessPolicy: []byte(`{"rewritten":true}`),
	}); err != nil || rows != 0 {
		t.Fatalf("changed series metadata was not idempotently rejected: rows=%d err=%v", rows, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE series SET name = 'direct rewrite' WHERE id = $1`, validUUID(t, series.ID)); err == nil {
		t.Fatal("direct series metadata update unexpectedly succeeded")
	}
	instrumentSymbol := "TEST-" + fixtureName
	if rows, err := queries.InsertInstrument(ctx, database.InsertInstrumentParams{
		CanonicalSymbol: instrumentSymbol, InstrumentType: "spot", NativeCurrency: "TRY", ExternalIds: []byte(`{}`), Status: "active",
	}); err != nil || rows != 1 {
		t.Fatalf("insert instrument: rows=%d err=%v", rows, err)
	}
	instrument, err := queries.GetInstrumentBySymbol(ctx, instrumentSymbol)
	if err != nil {
		t.Fatalf("get instrument: %v", err)
	}
	if rows, err := queries.InsertInstrument(ctx, database.InsertInstrumentParams{
		CanonicalSymbol: instrumentSymbol, InstrumentType: "bond", NativeCurrency: "USD", ExternalIds: []byte(`{"rewritten":true}`), Status: "delisted",
	}); err != nil || rows != 0 {
		t.Fatalf("changed instrument metadata was not idempotently rejected: rows=%d err=%v", rows, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE instruments SET instrument_type = 'bond' WHERE id = $1`, validUUID(t, instrument.ID)); err == nil {
		t.Fatal("direct instrument identity update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `UPDATE instruments SET status = 'inactive' WHERE id = $1`, validUUID(t, instrument.ID)); err != nil {
		t.Fatalf("instrument lifecycle status update unexpectedly failed: %v", err)
	}
	otherSourceCode := "integration-other-" + fixtureName
	if rows, err := queries.InsertDataSource(ctx, database.InsertDataSourceParams{
		Code: otherSourceCode, Name: "Other source", AdapterVersion: "test-1", Metadata: []byte(`{}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert second source: rows=%d err=%v", rows, err)
	}
	otherSource, err := queries.GetDataSourceByCode(ctx, otherSourceCode)
	if err != nil {
		t.Fatalf("get second source: %v", err)
	}
	otherExternalKey := "other-dataset-" + fixtureName
	if rows, err := queries.InsertDataset(ctx, database.InsertDatasetParams{
		SourceID: validUUID(t, otherSource.ID), ExternalKey: otherExternalKey, Name: "Other dataset", Metadata: []byte(`{}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert second dataset: rows=%d err=%v", rows, err)
	}
	otherDataset, err := queries.GetDatasetByExternalKey(ctx, database.GetDatasetByExternalKeyParams{
		SourceID: validUUID(t, otherSource.ID), ExternalKey: otherExternalKey,
	})
	if err != nil {
		t.Fatalf("get second dataset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO ingestion_runs (source_id, dataset_id, idempotency_key, adapter_version, status)
		VALUES ($1, $2, $3, 'test-1', 'running')`, validUUID(t, source.ID), validUUID(t, dataset.ID), "valid-"+fixtureName); err != nil {
		t.Fatalf("valid source/dataset ingestion relation rejected: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO ingestion_runs (source_id, dataset_id, idempotency_key, adapter_version, status)
		VALUES ($1, $2, $3, 'test-1', 'running')`, validUUID(t, source.ID), validUUID(t, otherDataset.ID), "invalid-"+fixtureName); err == nil {
		t.Fatal("mismatched source/dataset ingestion relation unexpectedly succeeded")
	}

	digest := sha256.Sum256([]byte(fixtureName))
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
	if rows, err := queries.InsertRawObject(ctx, database.InsertRawObjectParams{
		ContentSha256: sha, ObjectKey: "sha256/" + sha, MediaType: "application/json", ByteLength: 13, RetrievedAt: timestamp("2026-01-01T00:00:00Z"), RequestMetadata: []byte(`{"rewritten":true}`),
	}); err != nil || rows != 0 {
		t.Fatalf("duplicate raw object was not idempotently rejected: rows=%d err=%v", rows, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE raw_objects SET media_type = 'text/plain' WHERE id = $1`, validUUID(t, raw.ID)); err == nil {
		t.Fatal("raw object update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM raw_objects WHERE id = $1`, validUUID(t, raw.ID)); err == nil {
		t.Fatal("raw object delete unexpectedly succeeded")
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
	sourceAsOf, err := queries.ListObservationsSourceAsOf(ctx, database.ListObservationsSourceAsOfParams{
		SeriesID: seriesID, ObservationTime: timestamp("2025-12-31T00:00:00Z"), ObservationTime_2: timestamp("2026-01-02T00:00:00Z"), SourceKnownAt: timestamp("2026-01-03T00:00:00Z"),
	})
	if err != nil || len(sourceAsOf) != 1 || !sourceAsOf[0].SourceKnownAt.Valid {
		t.Fatalf("source-as-of did not select source-known revision: count=%d err=%v rows=%+v", len(sourceAsOf), err, sourceAsOf)
	}
	systemAsOf, err := queries.ListObservationsSystemAsOf(ctx, database.ListObservationsSystemAsOfParams{
		SeriesID: seriesID, ObservationTime: timestamp("2025-12-31T00:00:00Z"), ObservationTime_2: timestamp("2026-01-02T00:00:00Z"), SystemKnownAt: timestamp("2999-01-01T00:00:00Z"),
	})
	if err != nil || len(systemAsOf) != 1 || systemAsOf[0].SourceKnownAt.Valid || systemAsOf[0].KnowledgeTimeBasis != "first_observed_by_system" {
		t.Fatalf("system-as-of did not select latest captured revision: count=%d err=%v rows=%+v", len(systemAsOf), err, systemAsOf)
	}

	var stored string
	if err := pool.QueryRow(ctx, `SELECT value::text FROM observation_revisions WHERE series_id = $1 AND value = $2`, seriesID, value).Scan(&stored); err != nil {
		t.Fatalf("read exact observation: %v", err)
	}
	if stored != "123.456789012345678901" {
		t.Fatalf("exact decimal changed: got %q", stored)
	}

	priceParams := database.InsertPriceRevisionParams{
		InstrumentID: validUUID(t, instrument.ID), QuoteCurrency: "TRY", ObservationTime: observationTime, Price: decimal("98765.432100000000000000"), KnowledgeTimeBasis: "first_observed_by_system", RawObjectID: validUUID(t, raw.ID), QualityFlags: []byte(`{}`),
	}
	if rows, err := queries.InsertPriceRevision(ctx, priceParams); err != nil || rows != 1 {
		t.Fatalf("insert price revision: rows=%d err=%v", rows, err)
	}
	if rows, err := queries.InsertPriceRevision(ctx, priceParams); err != nil || rows != 0 {
		t.Fatalf("duplicate price revision was not idempotently rejected: rows=%d err=%v", rows, err)
	}
	priceRevision, err := queries.ListPriceRevisions(ctx, database.ListPriceRevisionsParams{
		InstrumentID: validUUID(t, instrument.ID), QuoteCurrency: "TRY", ObservationTime: timestamp("2025-12-31T00:00:00Z"), ObservationTime_2: timestamp("2026-01-02T00:00:00Z"),
	})
	if err != nil || len(priceRevision) != 1 {
		t.Fatalf("list price revisions: count=%d err=%v", len(priceRevision), err)
	}
	if rows, err := queries.InsertPriceRevision(ctx, database.InsertPriceRevisionParams{
		InstrumentID: validUUID(t, instrument.ID), QuoteCurrency: "TRY", ObservationTime: observationTime, Price: decimal("98765.432200000000000000"), KnowledgeTimeBasis: "first_observed_by_system", RawObjectID: validUUID(t, raw.ID), QualityFlags: []byte(`{"revised":true}`),
	}); err != nil || rows != 1 {
		t.Fatalf("revised price insert: rows=%d err=%v", rows, err)
	}
	if rows, err := queries.InsertPriceRevision(ctx, database.InsertPriceRevisionParams{
		InstrumentID: validUUID(t, instrument.ID), QuoteCurrency: "TRY", ObservationTime: observationTime, Price: decimal("98765.432300000000000000"), SourceKnownAt: sourceTime, KnowledgeTimeBasis: "source_published_at", RawObjectID: validUUID(t, raw.ID), QualityFlags: []byte(`{"source_known":true}`),
	}); err != nil || rows != 1 {
		t.Fatalf("source-known price insert: rows=%d err=%v", rows, err)
	}
	priceSourceAsOf, err := queries.ListPricesSourceAsOf(ctx, database.ListPricesSourceAsOfParams{
		InstrumentID: validUUID(t, instrument.ID), QuoteCurrency: "TRY", ObservationTime: timestamp("2025-12-31T00:00:00Z"), ObservationTime_2: timestamp("2026-01-02T00:00:00Z"), SourceKnownAt: timestamp("2026-01-03T00:00:00Z"),
	})
	if err != nil || len(priceSourceAsOf) != 1 || !priceSourceAsOf[0].SourceKnownAt.Valid {
		t.Fatalf("price source-as-of did not select source-known revision: count=%d err=%v rows=%+v", len(priceSourceAsOf), err, priceSourceAsOf)
	}
	priceSystemAsOf, err := queries.ListPricesSystemAsOf(ctx, database.ListPricesSystemAsOfParams{
		InstrumentID: validUUID(t, instrument.ID), QuoteCurrency: "TRY", ObservationTime: timestamp("2025-12-31T00:00:00Z"), ObservationTime_2: timestamp("2026-01-02T00:00:00Z"), SystemKnownAt: timestamp("2999-01-01T00:00:00Z"),
	})
	if err != nil || len(priceSystemAsOf) != 1 || !priceSystemAsOf[0].SourceKnownAt.Valid {
		t.Fatalf("price system-as-of did not select latest captured revision: count=%d err=%v rows=%+v", len(priceSystemAsOf), err, priceSystemAsOf)
	}
	if _, err := pool.Exec(ctx, `UPDATE price_revisions SET price = 1 WHERE instrument_id = $1`, validUUID(t, instrument.ID)); err == nil {
		t.Fatal("price revision update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM price_revisions WHERE instrument_id = $1`, validUUID(t, instrument.ID)); err == nil {
		t.Fatal("price revision delete unexpectedly succeeded")
	}

	fxParams := database.InsertFXQuoteRevisionParams{
		BaseCurrency: "USD", QuoteCurrency: "TRY", ObservationTime: observationTime, Rate: decimal("34.123456789012345678"), SourceKnownAt: sourceTime, KnowledgeTimeBasis: "source_published_at", RawObjectID: validUUID(t, raw.ID), QualityFlags: []byte(`{}`),
	}
	if rows, err := queries.InsertFXQuoteRevision(ctx, fxParams); err != nil || rows != 1 {
		t.Fatalf("insert FX revision: rows=%d err=%v", rows, err)
	}
	if rows, err := queries.InsertFXQuoteRevision(ctx, fxParams); err != nil || rows != 0 {
		t.Fatalf("duplicate FX revision was not idempotently rejected: rows=%d err=%v", rows, err)
	}
	if rows, err := queries.InsertFXQuoteRevision(ctx, database.InsertFXQuoteRevisionParams{
		BaseCurrency: "USD", QuoteCurrency: "TRY", ObservationTime: observationTime, Rate: decimal("34.223456789012345678"), SourceKnownAt: sourceTime, KnowledgeTimeBasis: "source_published_at", RawObjectID: validUUID(t, raw.ID), QualityFlags: []byte(`{"revised":true}`),
	}); err != nil || rows != 1 {
		t.Fatalf("revised FX insert: rows=%d err=%v", rows, err)
	}
	fxSourceAsOf, err := queries.ListFXQuotesSourceAsOf(ctx, database.ListFXQuotesSourceAsOfParams{
		BaseCurrency: "USD", QuoteCurrency: "TRY", ObservationTime: timestamp("2025-12-31T00:00:00Z"), ObservationTime_2: timestamp("2026-01-02T00:00:00Z"), SourceKnownAt: timestamp("2026-01-03T00:00:00Z"),
	})
	if err != nil || len(fxSourceAsOf) != 1 || !fxSourceAsOf[0].SourceKnownAt.Valid {
		t.Fatalf("FX source-as-of did not select source-known revision: count=%d err=%v rows=%+v", len(fxSourceAsOf), err, fxSourceAsOf)
	}
	fxSystemAsOf, err := queries.ListFXQuotesSystemAsOf(ctx, database.ListFXQuotesSystemAsOfParams{
		BaseCurrency: "USD", QuoteCurrency: "TRY", ObservationTime: timestamp("2025-12-31T00:00:00Z"), ObservationTime_2: timestamp("2026-01-02T00:00:00Z"), SystemKnownAt: timestamp("2999-01-01T00:00:00Z"),
	})
	if err != nil || len(fxSystemAsOf) != 1 || !fxSystemAsOf[0].SourceKnownAt.Valid {
		t.Fatalf("FX system-as-of did not select latest captured revision: count=%d err=%v rows=%+v", len(fxSystemAsOf), err, fxSystemAsOf)
	}
	if _, err := pool.Exec(ctx, `UPDATE fx_quote_revisions SET rate = 1 WHERE base_currency = 'USD' AND quote_currency = 'TRY'`); err == nil {
		t.Fatal("FX revision update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM fx_quote_revisions WHERE base_currency = 'USD' AND quote_currency = 'TRY'`); err == nil {
		t.Fatal("FX revision delete unexpectedly succeeded")
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
	instrumentID := validUUID(t, instrument.ID)
	assertPlanIndex(t, conn, "price_revisions_instrument_time_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM price_revisions WHERE instrument_id = $1 AND quote_currency = $2 AND observation_time >= $3 AND observation_time < $4 ORDER BY observation_time`, instrumentID, "TRY", timestamp("2025-12-31T00:00:00Z"), timestamp("2026-01-02T00:00:00Z"))
	assertPlanIndex(t, conn, "price_revisions_instrument_system_asof_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM price_revisions WHERE instrument_id = $1 AND system_known_at <= $2`, instrumentID, timestamp("2999-01-01T00:00:00Z"))
	assertPlanIndex(t, conn, "price_revisions_instrument_source_asof_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM price_revisions WHERE instrument_id = $1 AND source_known_at IS NOT NULL AND source_known_at <= $2`, instrumentID, timestamp("2026-01-03T00:00:00Z"))
	assertPlanIndex(t, conn, "fx_quote_revisions_pair_time_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM fx_quote_revisions WHERE base_currency = $1 AND quote_currency = $2 AND observation_time >= $3 AND observation_time < $4 ORDER BY observation_time`, "USD", "TRY", timestamp("2025-12-31T00:00:00Z"), timestamp("2026-01-02T00:00:00Z"))
	assertPlanIndex(t, conn, "fx_quote_revisions_pair_system_asof_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM fx_quote_revisions WHERE base_currency = $1 AND quote_currency = $2 AND system_known_at <= $3`, "USD", "TRY", timestamp("2999-01-01T00:00:00Z"))
	assertPlanIndex(t, conn, "fx_quote_revisions_pair_source_asof_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM fx_quote_revisions WHERE base_currency = $1 AND quote_currency = $2 AND source_known_at IS NOT NULL AND source_known_at <= $3`, "USD", "TRY", timestamp("2026-01-03T00:00:00Z"))
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
