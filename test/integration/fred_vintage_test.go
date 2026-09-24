package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/ingestion"
	"github.com/oplosy/atrisk/internal/platform/database"
	"github.com/oplosy/atrisk/internal/sources/fred"
)

func TestFREDVintage(t *testing.T) {
	migrateTestDatabase(t)
	queries, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	fixtureName := "fred-vintage-" + time.Now().UTC().Format("20060102150405.000000000")
	body, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "fred", "vintage-observations.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest := archive.SHA256Hex(body)
	retrievedAt := time.Now().UTC().Truncate(time.Microsecond)
	if rows, err := queries.InsertDataSource(ctx, database.InsertDataSourceParams{
		Code: "fred-" + fixtureName, Name: "FRED", AdapterVersion: fred.AdapterVersion, Metadata: []byte(`{"provider":"fred"}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert FRED source: rows=%d err=%v", rows, err)
	}
	source, err := queries.GetDataSourceByCode(ctx, "fred-"+fixtureName)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := queries.InsertDataset(ctx, database.InsertDatasetParams{
		SourceID: validUUID(t, source.ID), ExternalKey: "macro", Name: "FRED macro", Metadata: []byte(`{"vintages":true}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert FRED dataset: rows=%d err=%v", rows, err)
	}
	dataset, err := queries.GetDatasetByExternalKey(ctx, database.GetDatasetByExternalKeyParams{SourceID: validUUID(t, source.ID), ExternalKey: "macro"})
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := queries.InsertSeries(ctx, database.InsertSeriesParams{
		DatasetID: validUUID(t, dataset.ID), SourceCode: "CPIAUCSL", Name: "Consumer Price Index", Unit: "lin", Frequency: "monthly", FreshnessPolicy: []byte(`{}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert FRED series: rows=%d err=%v", rows, err)
	}
	series, err := queries.GetSeriesBySourceCode(ctx, database.GetSeriesBySourceCodeParams{DatasetID: validUUID(t, dataset.ID), SourceCode: "CPIAUCSL"})
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := queries.InsertRawObject(ctx, database.InsertRawObjectParams{
		ContentSha256: digest, ObjectKey: archive.ObjectKey(digest), MediaType: "application/json", ByteLength: int64(len(body)),
		RetrievedAt: pgtype.Timestamptz{Time: retrievedAt, Valid: true}, RequestMetadata: []byte(`{"fixture":"fred"}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert FRED raw object: rows=%d err=%v", rows, err)
	}
	client, err := fred.NewClient(fred.Config{BaseURL: "https://api.stlouisfed.org", APIKey: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	request := fred.ObservationRequest{SeriesID: "CPIAUCSL", OutputType: 2}
	adapter, err := fred.NewAdapter(client, "CPIAUCSL", request)
	if err != nil {
		t.Fatal(err)
	}
	records, err := adapter.Normalize(ctx, fredRawPayload(body, digest))
	if err != nil {
		t.Fatal(err)
	}
	store := fred.Store{Pool: pool}
	datasetID := dataset.ID.String()
	runStore := ingestion.DatabaseStore{Pool: pool}
	runID, duplicate, err := runStore.StartRun(ctx, ingestion.RunSpec{
		SourceID: source.ID.String(), DatasetID: &datasetID, IdempotencyKey: fixtureName, AdapterVersion: fred.AdapterVersion,
	})
	if err != nil || duplicate {
		t.Fatalf("start FRED ingestion run: id=%q duplicate=%v err=%v", runID, duplicate, err)
	}
	checkpoint, err := fred.NewObservationCheckpoint(request, 1000)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint.NextOffset = 2
	checkpoint.Pages = 1
	checkpoint.Observations = 2
	if err := store.SaveCheckpoint(ctx, runID, checkpoint); err != nil {
		t.Fatal(err)
	}
	loadedCheckpoint, err := store.LoadCheckpoint(ctx, runID)
	if err != nil || loadedCheckpoint.NextOffset != 2 || loadedCheckpoint.Observations != 2 {
		t.Fatalf("checkpoint did not round-trip: %+v err=%v", loadedCheckpoint, err)
	}
	if resumed, err := loadedCheckpoint.NextRequest(request, 1000); err != nil || resumed.Offset != 2 {
		t.Fatalf("persisted checkpoint did not resume original request: request=%+v err=%v", resumed, err)
	}
	if _, err := loadedCheckpoint.NextRequest(fred.ObservationRequest{SeriesID: "CPIAUCSL", OutputType: 2, VintageDates: "2025-02-01"}, 1000); !errors.Is(err, fred.ErrCheckpointRequestMismatch) {
		t.Fatalf("persisted checkpoint accepted changed vintage filter: %v", err)
	}
	inserted, err := store.PersistRecords(ctx, series.ID.String(), records)
	if err != nil {
		t.Fatal(err)
	}
	if inserted != 4 {
		t.Fatalf("inserted %d FRED revisions, want 4", inserted)
	}
	if inserted, err = store.PersistRecords(ctx, series.ID.String(), records); err != nil || inserted != 0 {
		t.Fatalf("duplicate FRED ingest was not idempotent: inserted=%d err=%v", inserted, err)
	}

	sourceAsOf, err := queries.ListObservationsSourceAsOf(ctx, database.ListObservationsSourceAsOfParams{
		SeriesID: validUUID(t, series.ID), ObservationTime: timestamp("2024-01-01T00:00:00Z"), ObservationTime_2: timestamp("2024-01-02T00:00:00Z"), SourceKnownAt: timestamp("2025-01-15T00:00:00Z"),
	})
	if err != nil || len(sourceAsOf) != 1 {
		t.Fatalf("source-as-of returned %d rows err=%v", len(sourceAsOf), err)
	}
	assertDecimal(t, sourceAsOf[0].Value, "100.123456789012345678")
	sourceAsOf, err = queries.ListObservationsSourceAsOf(ctx, database.ListObservationsSourceAsOfParams{
		SeriesID: validUUID(t, series.ID), ObservationTime: timestamp("2024-01-01T00:00:00Z"), ObservationTime_2: timestamp("2024-01-02T00:00:00Z"), SourceKnownAt: timestamp("2025-02-15T00:00:00Z"),
	})
	if err != nil || len(sourceAsOf) != 1 {
		t.Fatalf("revised source-as-of returned %d rows err=%v", len(sourceAsOf), err)
	}
	assertDecimal(t, sourceAsOf[0].Value, "101.987654321098765432")
	systemBefore, err := queries.ListObservationsSystemAsOf(ctx, database.ListObservationsSystemAsOfParams{
		SeriesID: validUUID(t, series.ID), ObservationTime: timestamp("2024-01-01T00:00:00Z"), ObservationTime_2: timestamp("2024-01-02T00:00:00Z"), SystemKnownAt: pgtype.Timestamptz{Time: retrievedAt.Add(-time.Minute), Valid: true},
	})
	if err != nil || len(systemBefore) != 0 {
		t.Fatalf("system-as-of exposed data before ingestion: rows=%d err=%v", len(systemBefore), err)
	}
	systemAfter, err := queries.ListObservationsSystemAsOf(ctx, database.ListObservationsSystemAsOfParams{
		SeriesID: validUUID(t, series.ID), ObservationTime: timestamp("2024-01-01T00:00:00Z"), ObservationTime_2: timestamp("2024-01-02T00:00:00Z"), SystemKnownAt: timestamp("2999-01-01T00:00:00Z"),
	})
	if err != nil || len(systemAfter) != 1 {
		t.Fatalf("system-as-of returned %d rows err=%v", len(systemAfter), err)
	}
	assertDecimal(t, systemAfter[0].Value, "101.987654321098765432")

	var missingValue, missingText, missingFlag bool
	if err := pool.QueryRow(ctx, `
SELECT value IS NULL, value_text = '.', quality_flags->>'missing' = 'true'
FROM observation_revisions WHERE series_id = $1 AND observation_time = $2`, validUUID(t, series.ID), timestamp("2024-02-01T00:00:00Z")).Scan(&missingValue, &missingText, &missingFlag); err != nil {
		t.Fatal(err)
	}
	if !missingValue || !missingText || !missingFlag {
		t.Fatalf("missing FRED marker was not preserved: null=%v text=%v flag=%v", missingValue, missingText, missingFlag)
	}
	if err := runStore.CompleteRun(ctx, runID, "succeeded", map[string]any{"checkpoint": loadedCheckpoint}); err != nil {
		t.Fatal(err)
	}
}

func fredRawPayload(body []byte, digest string) ingestion.RawPayload {
	return ingestion.RawPayload{Body: body, MediaType: "application/json", Archive: archive.Reference{Key: archive.ObjectKey(digest), ContentSHA256: digest, ByteLength: int64(len(body)), MediaType: "application/json"}}
}
