package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/oplosy/atrisk/internal/platform/database"
	"github.com/pressly/goose/v3"
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
	t.Setenv("PGHOST", "remote.example")
	t.Setenv("PGPORT", "65432")
	t.Setenv("PGDATABASE", "remote_database")
	t.Setenv("PGUSER", "remote_user")
	t.Setenv("PGSSLMODE", "require")
	t.Setenv("PGSERVICE", "")
	t.Setenv("PGSERVICEFILE", "")
	if err := database.ValidateIsolatedTestDatabaseURL(valid); err != nil {
		t.Fatalf("explicit isolated URI was affected by PostgreSQL environment defaults: %v", err)
	}
	unsafe := []string{
		"",
		"postgres://test-user:test-password@127.0.0.1:55432/contest",
		"postgres://test-user:test-password@127.0.0.1:55432/latest",
		"postgres://test-user:test-password@db.example.test:55432/atrisk_test",
		"postgres://test-user:test-password/atrisk_test",
		"postgres://127.0.0.1:55432/atrisk_test",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?host=remote.example&sslmode=disable",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?sslmode=disable&host=remote.example",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?port=65432",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?dbname=remote_database",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?database=remote_database",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?user=remote_user",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?service=remote",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?servicefile=%2Ftmp%2Fremote.conf",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?options=-c%20search_path%3Dpublic",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?sslmode=require",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?sslmode=disable&sslmode=require",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?SSLMode=disable",
		"postgres://test-user:test-password@127.0.0.1:55432/atrisk_test?ssl=disable",
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

func assertDecimal(t *testing.T, got pgtype.Numeric, want string) {
	t.Helper()
	expected := decimal(want)
	if !got.Valid || got.NaN != expected.NaN || got.InfinityModifier != expected.InfinityModifier || got.Exp != expected.Exp ||
		(got.Int == nil) != (expected.Int == nil) || (got.Int != nil && got.Int.Cmp(expected.Int) != 0) {
		t.Fatalf("numeric value mismatch: got=%+v want=%+v", got, expected)
	}
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
	if currentVersion != 5 {
		t.Fatalf("expected latest migration version 5, got %d", currentVersion)
	}
	var assetUnitTypes int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int FROM information_schema.columns
		WHERE table_schema='public' AND ((table_name='instruments' AND column_name='native_currency' AND data_type='text')
		   OR (table_name='price_revisions' AND column_name='quote_currency' AND data_type='text'))`).Scan(&assetUnitTypes); err != nil {
		t.Fatalf("inspect asset unit column types: %v", err)
	}
	if assetUnitTypes != 2 {
		t.Fatalf("expected two widened asset-unit columns, got %d", assetUnitTypes)
	}
	var fxUnitType string
	if err := pool.QueryRow(ctx, `SELECT data_type FROM information_schema.columns WHERE table_schema='public' AND table_name='fx_quote_revisions' AND column_name='quote_currency'`).Scan(&fxUnitType); err != nil {
		t.Fatalf("inspect FX quote unit type: %v", err)
	}
	if fxUnitType != "character" {
		t.Fatalf("FX quote currency was widened unexpectedly: %q", fxUnitType)
	}
	var latestPriceViewCount int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM pg_views WHERE schemaname='public' AND viewname='latest_price_revisions'`).Scan(&latestPriceViewCount); err != nil {
		t.Fatalf("inspect latest price view: %v", err)
	}
	if latestPriceViewCount != 1 {
		t.Fatalf("latest price projection was not recreated after asset-unit migration: %d", latestPriceViewCount)
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

func TestCoreDatabasePreviousVersionUpgrade(t *testing.T) {
	dsn := isolatedTestDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schemaName := fmt.Sprintf("ar101_upgrade_%d", time.Now().UnixNano())

	setupPool, err := database.OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open isolated database for upgrade schema: %v", err)
	}
	if _, err := setupPool.Exec(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		setupPool.Close()
		t.Fatalf("create isolated upgrade schema: %v", err)
	}
	setupPool.Close()

	t.Cleanup(func() {
		cleanupPool, cleanupErr := database.OpenPool(context.Background(), dsn)
		if cleanupErr != nil {
			t.Errorf("open isolated database for upgrade-schema cleanup: %v", cleanupErr)
			return
		}
		defer cleanupPool.Close()
		if _, cleanupErr := cleanupPool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE"); cleanupErr != nil {
			t.Errorf("drop isolated upgrade schema: %v", cleanupErr)
		}
	})

	versionDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open SQL migration connection: %v", err)
	}
	versionDB.SetMaxOpenConns(1)
	versionDB.SetMaxIdleConns(1)
	if err := versionDB.PingContext(ctx); err != nil {
		versionDB.Close()
		t.Fatalf("ping SQL migration connection: %v", err)
	}
	if _, err := versionDB.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		versionDB.Close()
		t.Fatalf("set v1 migration schema: %v", err)
	}
	var currentSchema, currentSearchPath string
	if err := versionDB.QueryRowContext(ctx, "SELECT current_schema(), current_setting('search_path')").Scan(&currentSchema, &currentSearchPath); err != nil {
		versionDB.Close()
		t.Fatalf("inspect v1 migration schema: %v", err)
	}
	if currentSchema != schemaName {
		versionDB.Close()
		t.Fatalf("v1 migration connection did not select isolated schema: current=%q search_path=%q", currentSchema, currentSearchPath)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		versionDB.Close()
		t.Fatalf("set Goose dialect: %v", err)
	}
	goose.SetTableName(schemaName + "." + goose.DefaultTablename)
	defer goose.SetTableName(goose.DefaultTablename)
	if err := goose.UpToContext(ctx, versionDB, migrationDir(t), 1); err != nil {
		versionDB.Close()
		t.Fatalf("apply v1 migration in isolated schema: %v", err)
	}
	var version int64
	if err := versionDB.QueryRowContext(ctx, "SELECT max(version_id) FROM "+schemaName+"."+goose.DefaultTablename).Scan(&version); err != nil {
		versionDB.Close()
		t.Fatalf("inspect v1 schema migration version: %v", err)
	}
	if version != 1 {
		versionDB.Close()
		t.Fatalf("expected isolated schema at migration version 1, got %d", version)
	}
	sentinelCode := schemaName + "_sentinel"
	if _, err := versionDB.ExecContext(ctx, `
		INSERT INTO data_sources (code, name, adapter_version, metadata)
		VALUES ($1, 'v1 sentinel', 'upgrade-test', '{"sentinel":true}')`, sentinelCode); err != nil {
		versionDB.Close()
		t.Fatalf("insert v1 sentinel row: %v", err)
	}
	if _, err := versionDB.ExecContext(ctx, `
		INSERT INTO instruments (canonical_symbol, instrument_type, native_currency, external_ids)
		VALUES ($1, 'crypto_spot', 'TRY', '{"sentinel":true}')`, sentinelCode); err != nil {
		versionDB.Close()
		t.Fatalf("insert v1 instrument sentinel: %v", err)
	}
	for _, symbol := range []string{schemaName + "_btc_usdt", schemaName + "_eth_usdt"} {
		if _, err := versionDB.ExecContext(ctx, `
			INSERT INTO instruments (canonical_symbol, instrument_type, native_currency, external_ids)
			VALUES ($1, 'crypto_spot', 'USD', '{"provider":"binance"}')`, symbol); err != nil {
			versionDB.Close()
			t.Fatalf("insert v1 Binance instrument %s: %v", symbol, err)
		}
	}
	versionDB.Close()

	var hardeningConstraints int
	checkPool, err := database.OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open isolated database before upgrade: %v", err)
	}
	if err := checkPool.QueryRow(ctx, `
		SELECT count(*)::int
		FROM pg_constraint c
		JOIN pg_class r ON r.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = r.relnamespace
		WHERE n.nspname = $1 AND c.conname = 'ingestion_runs_source_dataset_fk'`, schemaName).Scan(&hardeningConstraints); err != nil {
		checkPool.Close()
		t.Fatalf("inspect pre-upgrade hardening constraint: %v", err)
	}
	checkPool.Close()
	if hardeningConstraints != 0 {
		t.Fatalf("v1 schema unexpectedly contains v2 hardening constraint: %d", hardeningConstraints)
	}

	if err := database.MigrateInSchema(ctx, dsn, migrationDir(t), schemaName); err != nil {
		t.Fatalf("upgrade isolated v1 schema through production migration path: %v", err)
	}
	upgradedDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open isolated database after upgrade: %v", err)
	}
	upgradedDB.SetMaxOpenConns(1)
	upgradedDB.SetMaxIdleConns(1)
	defer upgradedDB.Close()
	if err := upgradedDB.PingContext(ctx); err != nil {
		t.Fatalf("ping isolated database after upgrade: %v", err)
	}
	if _, err := upgradedDB.ExecContext(ctx, "SET search_path TO "+schemaName+", public"); err != nil {
		t.Fatalf("set upgraded migration schema: %v", err)
	}
	if err := upgradedDB.QueryRowContext(ctx, "SELECT max(version_id) FROM "+schemaName+"."+goose.DefaultTablename).Scan(&version); err != nil {
		t.Fatalf("inspect upgraded schema migration version: %v", err)
	}
	if version != 5 {
		t.Fatalf("expected isolated schema at migration version 5, got %d", version)
	}
	if err := upgradedDB.QueryRowContext(ctx, `
		SELECT count(*)::int
		FROM pg_constraint c
		JOIN pg_class r ON r.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = r.relnamespace
		WHERE n.nspname = $1 AND c.conname = 'ingestion_runs_source_dataset_fk'`, schemaName).Scan(&hardeningConstraints); err != nil {
		t.Fatalf("inspect upgraded hardening constraint: %v", err)
	}
	if hardeningConstraints != 1 {
		t.Fatalf("v2 upgrade did not add source/dataset hardening constraint: %d", hardeningConstraints)
	}
	var sentinelName string
	if err := upgradedDB.QueryRowContext(ctx, "SELECT name FROM "+schemaName+".data_sources WHERE code = $1", sentinelCode).Scan(&sentinelName); err != nil {
		t.Fatalf("inspect v1 sentinel row after upgrade: %v", err)
	}
	if sentinelName != "v1 sentinel" {
		t.Fatalf("v1 sentinel row changed during upgrade: %q", sentinelName)
	}
	var sentinelCurrency string
	if err := upgradedDB.QueryRowContext(ctx, "SELECT native_currency FROM "+schemaName+".instruments WHERE canonical_symbol = $1", sentinelCode).Scan(&sentinelCurrency); err != nil {
		t.Fatalf("inspect v1 instrument sentinel after upgrade: %v", err)
	}
	if sentinelCurrency != "TRY" {
		t.Fatalf("v1 instrument value changed during upgrade: %q", sentinelCurrency)
	}
	var binanceIdentifiers int
	if err := upgradedDB.QueryRowContext(ctx, `
		SELECT count(*)::int FROM `+schemaName+`.instrument_external_identifiers
		WHERE namespace = 'binance.symbol' AND external_id IN ($1, $2)`, schemaName+"_btc_usdt", schemaName+"_eth_usdt").Scan(&binanceIdentifiers); err != nil {
		t.Fatalf("inspect upgraded Binance identifiers: %v", err)
	}
	if binanceIdentifiers != 2 {
		t.Fatalf("expected two upgraded Binance identifiers, got %d", binanceIdentifiers)
	}
	binanceSymbol := schemaName + "_btc_usdt"
	if _, err := upgradedDB.ExecContext(ctx, "UPDATE "+schemaName+".instruments SET status = 'inactive' WHERE canonical_symbol = $1", binanceSymbol); err != nil {
		t.Fatalf("update upgraded Binance status: %v", err)
	}
	if _, err := upgradedDB.ExecContext(ctx, `
		INSERT INTO instruments (canonical_symbol, instrument_type, native_currency, external_ids, status)
		VALUES ($1, 'crypto_spot', 'USDT', '{"provider":"binance"}', 'active')
		ON CONFLICT (canonical_symbol) DO UPDATE SET status = EXCLUDED.status`, binanceSymbol); err != nil {
		t.Fatalf("replay Binance upsert after upgrade: %v", err)
	}
	var binanceStatus string
	if err := upgradedDB.QueryRowContext(ctx, "SELECT status FROM "+schemaName+".instruments WHERE canonical_symbol = $1", binanceSymbol).Scan(&binanceStatus); err != nil {
		t.Fatalf("inspect replayed Binance status: %v", err)
	}
	if binanceStatus != "active" {
		t.Fatalf("replayed Binance status=%q", binanceStatus)
	}
	if err := upgradedDB.QueryRowContext(ctx, `
		SELECT count(*)::int FROM `+schemaName+`.instrument_external_identifiers
		WHERE namespace = 'binance.symbol' AND external_id = $1`, binanceSymbol).Scan(&binanceIdentifiers); err != nil {
		t.Fatalf("inspect replayed Binance identifier: %v", err)
	}
	if binanceIdentifiers != 1 {
		t.Fatalf("replayed Binance identifier count=%d", binanceIdentifiers)
	}
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
	fxBaseCurrency := fmt.Sprintf("X%02X", digest[0])
	fxQuoteCurrency := fmt.Sprintf("Y%02X", digest[1])
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
	runConcurrentInsert := func(label string, insert func() (int64, error)) int64 {
		var workers sync.WaitGroup
		results := make(chan int64, 8)
		errors := make(chan error, 8)
		for range 8 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				rows, insertErr := insert()
				if insertErr != nil {
					errors <- insertErr
					return
				}
				results <- rows
			}()
		}
		workers.Wait()
		close(results)
		close(errors)
		var inserted int64
		for rows := range results {
			inserted += rows
		}
		for insertErr := range errors {
			t.Fatalf("concurrent %s insert: %v", label, insertErr)
		}
		return inserted
	}
	concurrentRawDigest := sha256.Sum256([]byte(fixtureName + "-concurrent-raw"))
	concurrentRawSHA := fmt.Sprintf("%x", concurrentRawDigest)
	if inserted := runConcurrentInsert("raw object", func() (int64, error) {
		return queries.InsertRawObject(ctx, database.InsertRawObjectParams{
			ContentSha256: concurrentRawSHA, ObjectKey: "sha256/" + concurrentRawSHA, MediaType: "application/json", ByteLength: 13, RetrievedAt: timestamp("2026-01-01T00:00:00Z"), RequestMetadata: []byte(`{"source":"concurrent-fixture"}`),
		})
	}); inserted != 1 {
		t.Fatalf("concurrent identical raw object insert affected %d rows, want 1", inserted)
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
	assertDecimal(t, sourceAsOf[0].Value, "123.456789012345678902")
	systemAsOf, err := queries.ListObservationsSystemAsOf(ctx, database.ListObservationsSystemAsOfParams{
		SeriesID: seriesID, ObservationTime: timestamp("2025-12-31T00:00:00Z"), ObservationTime_2: timestamp("2026-01-02T00:00:00Z"), SystemKnownAt: timestamp("2999-01-01T00:00:00Z"),
	})
	if err != nil || len(systemAsOf) != 1 || systemAsOf[0].SourceKnownAt.Valid || systemAsOf[0].KnowledgeTimeBasis != "first_observed_by_system" {
		t.Fatalf("system-as-of did not select latest captured revision: count=%d err=%v rows=%+v", len(systemAsOf), err, systemAsOf)
	}
	assertDecimal(t, systemAsOf[0].Value, "321.000000000000000000")

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
	concurrentPriceParams := database.InsertPriceRevisionParams{
		InstrumentID: validUUID(t, instrument.ID), QuoteCurrency: "TRY", ObservationTime: timestamp("2026-01-03T00:00:00Z"), Price: decimal("100.000000000000000001"), KnowledgeTimeBasis: "first_observed_by_system", RawObjectID: validUUID(t, raw.ID), QualityFlags: []byte(`{"concurrent":true}`),
	}
	if inserted := runConcurrentInsert("price revision", func() (int64, error) {
		return queries.InsertPriceRevision(ctx, concurrentPriceParams)
	}); inserted != 1 {
		t.Fatalf("concurrent identical price revision insert affected %d rows, want 1", inserted)
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
	assertDecimal(t, priceSourceAsOf[0].Price, "98765.432300000000000000")
	priceSystemAsOf, err := queries.ListPricesSystemAsOf(ctx, database.ListPricesSystemAsOfParams{
		InstrumentID: validUUID(t, instrument.ID), QuoteCurrency: "TRY", ObservationTime: timestamp("2025-12-31T00:00:00Z"), ObservationTime_2: timestamp("2026-01-02T00:00:00Z"), SystemKnownAt: timestamp("2999-01-01T00:00:00Z"),
	})
	if err != nil || len(priceSystemAsOf) != 1 || !priceSystemAsOf[0].SourceKnownAt.Valid {
		t.Fatalf("price system-as-of did not select latest captured revision: count=%d err=%v rows=%+v", len(priceSystemAsOf), err, priceSystemAsOf)
	}
	assertDecimal(t, priceSystemAsOf[0].Price, "98765.432300000000000000")
	if _, err := pool.Exec(ctx, `UPDATE price_revisions SET price = 1 WHERE instrument_id = $1`, validUUID(t, instrument.ID)); err == nil {
		t.Fatal("price revision update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM price_revisions WHERE instrument_id = $1`, validUUID(t, instrument.ID)); err == nil {
		t.Fatal("price revision delete unexpectedly succeeded")
	}

	fxParams := database.InsertFXQuoteRevisionParams{
		BaseCurrency: fxBaseCurrency, QuoteCurrency: fxQuoteCurrency, ObservationTime: observationTime, Rate: decimal("34.123456789012345678"), SourceKnownAt: sourceTime, KnowledgeTimeBasis: "source_published_at", RawObjectID: validUUID(t, raw.ID), QualityFlags: []byte(`{}`),
	}
	if rows, err := queries.InsertFXQuoteRevision(ctx, fxParams); err != nil || rows != 1 {
		t.Fatalf("insert FX revision: rows=%d err=%v", rows, err)
	}
	if rows, err := queries.InsertFXQuoteRevision(ctx, fxParams); err != nil || rows != 0 {
		t.Fatalf("duplicate FX revision was not idempotently rejected: rows=%d err=%v", rows, err)
	}
	concurrentFXParams := database.InsertFXQuoteRevisionParams{
		BaseCurrency: fxBaseCurrency, QuoteCurrency: fxQuoteCurrency, ObservationTime: timestamp("2026-01-03T00:00:00Z"), Rate: decimal("35.000000000000000001"), KnowledgeTimeBasis: "first_observed_by_system", RawObjectID: validUUID(t, raw.ID), QualityFlags: []byte(`{"concurrent":true}`),
	}
	if inserted := runConcurrentInsert("FX revision", func() (int64, error) {
		return queries.InsertFXQuoteRevision(ctx, concurrentFXParams)
	}); inserted != 1 {
		t.Fatalf("concurrent identical FX revision insert affected %d rows, want 1", inserted)
	}
	if rows, err := queries.InsertFXQuoteRevision(ctx, database.InsertFXQuoteRevisionParams{
		BaseCurrency: fxBaseCurrency, QuoteCurrency: fxQuoteCurrency, ObservationTime: observationTime, Rate: decimal("34.223456789012345678"), SourceKnownAt: sourceTime, KnowledgeTimeBasis: "source_published_at", RawObjectID: validUUID(t, raw.ID), QualityFlags: []byte(`{"revised":true}`),
	}); err != nil || rows != 1 {
		t.Fatalf("revised FX insert: rows=%d err=%v", rows, err)
	}
	fxSourceAsOf, err := queries.ListFXQuotesSourceAsOf(ctx, database.ListFXQuotesSourceAsOfParams{
		BaseCurrency: fxBaseCurrency, QuoteCurrency: fxQuoteCurrency, ObservationTime: timestamp("2025-12-31T00:00:00Z"), ObservationTime_2: timestamp("2026-01-02T00:00:00Z"), SourceKnownAt: timestamp("2026-01-03T00:00:00Z"),
	})
	if err != nil || len(fxSourceAsOf) != 1 || !fxSourceAsOf[0].SourceKnownAt.Valid {
		t.Fatalf("FX source-as-of did not select source-known revision: count=%d err=%v rows=%+v", len(fxSourceAsOf), err, fxSourceAsOf)
	}
	assertDecimal(t, fxSourceAsOf[0].Rate, "34.223456789012345678")
	fxSystemAsOf, err := queries.ListFXQuotesSystemAsOf(ctx, database.ListFXQuotesSystemAsOfParams{
		BaseCurrency: fxBaseCurrency, QuoteCurrency: fxQuoteCurrency, ObservationTime: timestamp("2025-12-31T00:00:00Z"), ObservationTime_2: timestamp("2026-01-02T00:00:00Z"), SystemKnownAt: timestamp("2999-01-01T00:00:00Z"),
	})
	if err != nil || len(fxSystemAsOf) != 1 || !fxSystemAsOf[0].SourceKnownAt.Valid {
		t.Fatalf("FX system-as-of did not select latest captured revision: count=%d err=%v rows=%+v", len(fxSystemAsOf), err, fxSystemAsOf)
	}
	assertDecimal(t, fxSystemAsOf[0].Rate, "34.223456789012345678")
	if _, err := pool.Exec(ctx, `UPDATE fx_quote_revisions SET rate = 1 WHERE base_currency = $1 AND quote_currency = $2`, fxBaseCurrency, fxQuoteCurrency); err == nil {
		t.Fatal("FX revision update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM fx_quote_revisions WHERE base_currency = $1 AND quote_currency = $2`, fxBaseCurrency, fxQuoteCurrency); err == nil {
		t.Fatal("FX revision delete unexpectedly succeeded")
	}

	if _, err := pool.Exec(ctx, `UPDATE observation_revisions SET value = 1 WHERE series_id = $1`, seriesID); err == nil {
		t.Fatal("observation update unexpectedly succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM observation_revisions WHERE series_id = $1`, seriesID); err == nil {
		t.Fatal("observation delete unexpectedly succeeded")
	}
	// Add selective planner fixtures in the tested time window. Keeping the rows
	// in the same series/pair makes the default planner choose the intended
	// indexes without disabling sequential scans artificially; future knowledge
	// clocks keep them out of the as-of result assertions above.
	if _, err := pool.Exec(ctx, `
		INSERT INTO observation_revisions (
			series_id, observation_time, value, source_known_at, system_known_at,
			knowledge_time_basis, raw_object_id
		)
		SELECT $1, TIMESTAMPTZ '2026-01-01T00:00:00Z' + (g * INTERVAL '1 minute'), g::numeric,
			TIMESTAMPTZ '2100-01-01T00:00:00Z' + (g * INTERVAL '1 day'),
			TIMESTAMPTZ '2999-01-01T00:00:00Z', 'source_published_at', $2
		FROM generate_series(1, 100) AS g`, seriesID, rawID); err != nil {
		t.Fatalf("seed observation planner rows: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO observation_revisions (
			series_id, observation_time, value, source_known_at, system_known_at,
			knowledge_time_basis, raw_object_id
		)
		SELECT $1, TIMESTAMPTZ '2000-01-01T00:00:00Z' + (g * INTERVAL '1 day'), g::numeric,
			TIMESTAMPTZ '2100-01-01T00:00:00Z' + (g * INTERVAL '1 day'),
			TIMESTAMPTZ '2999-01-01T00:00:00Z', 'source_published_at', $2
		FROM generate_series(1, 5000) AS g`, seriesID, rawID); err != nil {
		t.Fatalf("seed observation planner background rows: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO price_revisions (
			instrument_id, quote_currency, observation_time, price, system_known_at,
			knowledge_time_basis, raw_object_id
		)
		SELECT $1, 'TRY', TIMESTAMPTZ '2026-01-01T00:00:00Z' + (g * INTERVAL '1 minute'), g::numeric,
			TIMESTAMPTZ '2999-01-01T00:00:00Z', 'first_observed_by_system', $2
		FROM generate_series(1, 100) AS g`, validUUID(t, instrument.ID), rawID); err != nil {
		t.Fatalf("seed price planner rows: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO price_revisions (
			instrument_id, quote_currency, observation_time, price, system_known_at,
			knowledge_time_basis, raw_object_id
		)
		SELECT $1, 'TRY', TIMESTAMPTZ '2000-01-01T00:00:00Z' + (g * INTERVAL '1 day'), g::numeric,
			TIMESTAMPTZ '2999-01-01T00:00:00Z', 'first_observed_by_system', $2
		FROM generate_series(1, 5000) AS g`, validUUID(t, instrument.ID), rawID); err != nil {
		t.Fatalf("seed price planner background rows: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO fx_quote_revisions (
			base_currency, quote_currency, observation_time, rate, system_known_at,
			knowledge_time_basis, raw_object_id
		)
		SELECT $1, $2, TIMESTAMPTZ '2026-01-01T00:00:00Z' + (g * INTERVAL '1 minute'), g::numeric,
			TIMESTAMPTZ '2999-01-01T00:00:00Z', 'first_observed_by_system', $3
		FROM generate_series(1, 100) AS g`, fxBaseCurrency, fxQuoteCurrency, rawID); err != nil {
		t.Fatalf("seed FX planner rows: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO fx_quote_revisions (
			base_currency, quote_currency, observation_time, rate, system_known_at,
			knowledge_time_basis, raw_object_id
		)
		SELECT $1, $2, TIMESTAMPTZ '2000-01-01T00:00:00Z' + (g * INTERVAL '1 day'), g::numeric,
			TIMESTAMPTZ '2999-01-01T00:00:00Z', 'first_observed_by_system', $3
		FROM generate_series(1, 5000) AS g`, fxBaseCurrency, fxQuoteCurrency, rawID); err != nil {
		t.Fatalf("seed FX planner background rows: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE observation_revisions, price_revisions, fx_quote_revisions`); err != nil {
		t.Fatalf("analyze revision planner fixtures: %v", err)
	}

	var plan string
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire plan connection: %v", err)
	}
	defer conn.Release()
	plan, err = explainPlan(ctx, conn, `EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) SELECT id FROM observation_revisions WHERE series_id = $1 AND observation_time >= $2 AND observation_time < $3`, seriesID, observationTime, timestamp("2026-01-02T00:00:00Z"))
	if err != nil {
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
		`EXPLAIN (FORMAT TEXT) SELECT id FROM price_revisions WHERE instrument_id = $1 AND quote_currency = $2 AND observation_time >= $3 AND observation_time < $4 ORDER BY observation_time, system_known_at`, instrumentID, "TRY", timestamp("2025-12-31T00:00:00Z"), timestamp("2026-01-02T00:00:00Z"))
	assertPlanIndex(t, conn, "price_revisions_instrument_system_asof_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM price_revisions WHERE instrument_id = $1 AND system_known_at <= $2`, instrumentID, timestamp("2026-01-03T00:00:00Z"))
	assertPlanIndex(t, conn, "price_revisions_instrument_source_asof_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM price_revisions WHERE instrument_id = $1 AND source_known_at IS NOT NULL AND source_known_at <= $2`, instrumentID, timestamp("2026-01-03T00:00:00Z"))
	assertPlanIndex(t, conn, "fx_quote_revisions_pair_time_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM fx_quote_revisions WHERE base_currency = $1 AND quote_currency = $2 AND observation_time >= $3 AND observation_time < $4 ORDER BY observation_time`, fxBaseCurrency, fxQuoteCurrency, timestamp("2025-12-31T00:00:00Z"), timestamp("2026-01-02T00:00:00Z"))
	assertPlanIndex(t, conn, "fx_quote_revisions_pair_system_asof_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM fx_quote_revisions WHERE base_currency = $1 AND quote_currency = $2 AND system_known_at <= $3`, fxBaseCurrency, fxQuoteCurrency, timestamp("2026-01-03T00:00:00Z"))
	assertPlanIndex(t, conn, "fx_quote_revisions_pair_source_asof_idx",
		`EXPLAIN (FORMAT TEXT) SELECT id FROM fx_quote_revisions WHERE base_currency = $1 AND quote_currency = $2 AND source_known_at IS NOT NULL AND source_known_at <= $3`, fxBaseCurrency, fxQuoteCurrency, timestamp("2026-01-03T00:00:00Z"))
}

func assertPlanIndex(t *testing.T, conn *pgxpool.Conn, indexName, statement string, args ...any) {
	t.Helper()
	statement = strings.Replace(statement, "EXPLAIN (FORMAT TEXT)", "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)", 1)
	plan, err := explainPlan(context.Background(), conn, statement, args...)
	if err != nil {
		t.Fatalf("explain query for %s: %v", indexName, err)
	}
	if !strings.Contains(plan, indexName) {
		t.Fatalf("query did not use intended index %s: %s", indexName, plan)
	}
}

func explainPlan(ctx context.Context, conn *pgxpool.Conn, statement string, args ...any) (string, error) {
	rows, err := conn.Query(ctx, statement, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n"), nil
}
