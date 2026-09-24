package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/ingestion"
	"github.com/oplosy/atrisk/internal/platform/database"
	"github.com/oplosy/atrisk/internal/sources/tcmb"
)

func TestTCMBIngestion(t *testing.T) {
	migrateTestDatabase(t)
	queries, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	fixtureName := "tcmb-" + time.Now().UTC().Format("20060102150405.000000000")
	body, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "tcmb", "evds-observations.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest := archive.SHA256Hex(body)
	retrievedAt := time.Now().UTC().Truncate(time.Microsecond)
	if rows, err := queries.InsertDataSource(ctx, database.InsertDataSourceParams{
		Code: "tcmb-" + fixtureName, Name: "TCMB EVDS", AdapterVersion: tcmb.AdapterVersion, Metadata: []byte(`{"provider":"tcmb","contract":"evds2"}`),
	}); err != nil || rows != 1 {
		t.Fatalf("insert TCMB source: rows=%d err=%v", rows, err)
	}
	source, err := queries.GetDataSourceByCode(ctx, "tcmb-"+fixtureName)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := queries.InsertDataset(ctx, database.InsertDatasetParams{SourceID: validUUID(t, source.ID), ExternalKey: "evds", Name: "EVDS series", Metadata: []byte(`{"contract":"evds2"}`)}); err != nil || rows != 1 {
		t.Fatalf("insert TCMB dataset: rows=%d err=%v", rows, err)
	}
	dataset, err := queries.GetDatasetByExternalKey(ctx, database.GetDatasetByExternalKeyParams{SourceID: validUUID(t, source.ID), ExternalKey: "evds"})
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := queries.InsertSeries(ctx, database.InsertSeriesParams{DatasetID: validUUID(t, dataset.ID), SourceCode: "TP.DK.USD.A", Name: "USD buying rate", Unit: "TRY", Frequency: "daily", FreshnessPolicy: []byte(`{"expected":"business_daily"}`)}); err != nil || rows != 1 {
		t.Fatalf("insert TCMB series: rows=%d err=%v", rows, err)
	}
	series, err := queries.GetSeriesBySourceCode(ctx, database.GetSeriesBySourceCodeParams{DatasetID: validUUID(t, dataset.ID), SourceCode: "TP.DK.USD.A"})
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := queries.InsertRawObject(ctx, database.InsertRawObjectParams{ContentSha256: digest, ObjectKey: archive.ObjectKey(digest), MediaType: "application/json", ByteLength: int64(len(body)), RetrievedAt: pgtype.Timestamptz{Time: retrievedAt, Valid: true}, RequestMetadata: []byte(`{"fixture":"tcmb"}`)}); err != nil || rows != 1 {
		t.Fatalf("insert TCMB raw object: rows=%d err=%v", rows, err)
	}
	client, err := tcmb.NewClient(tcmb.Config{BaseURL: "https://evds2.tcmb.gov.tr", APIKey: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	request := tcmb.SeriesRequest{Series: []string{"TP.DK.USD.A", "TP.FAIZ.O1"}, StartDate: "02-01-2024", EndDate: "08-01-2024", DecimalSeparator: ",", Frequency: "1", AggregationTypes: "avg-avg"}
	adapter, err := tcmb.NewAdapter(client, request)
	if err != nil {
		t.Fatal(err)
	}
	records, err := adapter.Normalize(ctx, tcmbRawPayload(body, digest))
	if err != nil {
		t.Fatal(err)
	}
	store := tcmb.Store{Pool: pool}
	datasetID := dataset.ID.String()
	runStore := ingestion.DatabaseStore{Pool: pool}
	runID, duplicate, err := runStore.StartRun(ctx, ingestion.RunSpec{SourceID: source.ID.String(), DatasetID: &datasetID, IdempotencyKey: fixtureName, AdapterVersion: tcmb.AdapterVersion})
	if err != nil || duplicate {
		t.Fatalf("start TCMB ingestion run: id=%q duplicate=%v err=%v", runID, duplicate, err)
	}
	checkpoint, err := tcmb.NewObservationCheckpoint(request, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveCheckpoint(ctx, runID, checkpoint); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadCheckpoint(ctx, runID)
	if err != nil || loaded.RequestFingerprint != checkpoint.RequestFingerprint {
		t.Fatalf("TCMB checkpoint did not round-trip: %+v err=%v", loaded, err)
	}
	fxRecords := make([]ingestion.NormalizedRecord, 0, len(records))
	for _, record := range records {
		decoded, decodeErr := tcmb.DecodeObservationRecord(record)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if decoded.SeriesCode == "TP.DK.USD.A" {
			fxRecords = append(fxRecords, record)
		}
	}
	if _, err := store.PersistFXRecords(ctx, "USD", "TRY", series.ID.String(), records); err == nil {
		t.Fatal("mixed-series TCMB FX batch was accepted")
	}
	var quotesAfterMismatch int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM fx_quote_revisions WHERE base_currency = 'USD' AND quote_currency = 'TRY'`).Scan(&quotesAfterMismatch); err != nil {
		t.Fatal(err)
	}
	if quotesAfterMismatch != 0 {
		t.Fatalf("mixed-series FX batch partially inserted %d quotes", quotesAfterMismatch)
	}
	if inserted, err := store.PersistFXRecords(ctx, "USD", "TRY", series.ID.String(), fxRecords); err != nil || inserted != 5 {
		t.Fatalf("persist TCMB FX quotes and missing period: inserted=%d err=%v", inserted, err)
	}
	if inserted, err := store.PersistFXRecords(ctx, "USD", "TRY", series.ID.String(), fxRecords); err != nil || inserted != 0 {
		t.Fatalf("duplicate TCMB FX ingest was not idempotent: inserted=%d err=%v", inserted, err)
	}
	if inserted, err := store.PersistRecords(ctx, series.ID.String(), fxRecords); err != nil || inserted != 4 {
		t.Fatalf("persist TCMB observations: inserted=%d err=%v", inserted, err)
	}
	if inserted, err := store.PersistRecords(ctx, series.ID.String(), fxRecords); err != nil || inserted != 0 {
		t.Fatalf("duplicate TCMB ingest was not idempotent: inserted=%d err=%v", inserted, err)
	}
	revisionBody, err := os.ReadFile(filepath.Join("..", "..", "test", "fixtures", "tcmb", "evds-revision.json"))
	if err != nil {
		t.Fatal(err)
	}
	revisionDigest := archive.SHA256Hex(revisionBody)
	if rows, err := queries.InsertRawObject(ctx, database.InsertRawObjectParams{ContentSha256: revisionDigest, ObjectKey: archive.ObjectKey(revisionDigest), MediaType: "application/json", ByteLength: int64(len(revisionBody)), RetrievedAt: pgtype.Timestamptz{Time: retrievedAt.Add(time.Minute), Valid: true}, RequestMetadata: []byte(`{"fixture":"tcmb-revision"}`)}); err != nil || rows != 1 {
		t.Fatalf("insert TCMB revision raw object: rows=%d err=%v", rows, err)
	}
	revisionRecords, err := adapter.Normalize(ctx, tcmbRawPayload(revisionBody, revisionDigest))
	if err != nil {
		t.Fatal(err)
	}
	if inserted, err := store.PersistRecords(ctx, series.ID.String(), revisionRecords); err != nil || inserted != 1 {
		t.Fatalf("persist TCMB revised observation: inserted=%d err=%v", inserted, err)
	}
	var revisionCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM observation_revisions WHERE series_id = $1 AND observation_time = $2`, validUUID(t, series.ID), timestamp("2024-01-02T00:00:00Z")).Scan(&revisionCount); err != nil {
		t.Fatal(err)
	}
	if revisionCount != 2 {
		t.Fatalf("revised TCMB observation overwrote history: revisions=%d", revisionCount)
	}
	rows, err := queries.ListObservationsSystemAsOf(ctx, database.ListObservationsSystemAsOfParams{SeriesID: validUUID(t, series.ID), ObservationTime: timestamp("2024-01-02T00:00:00Z"), ObservationTime_2: timestamp("2024-01-09T00:00:00Z"), SystemKnownAt: timestamp("2999-01-01T00:00:00Z")})
	if err != nil || len(rows) != 5 {
		t.Fatalf("TCMB system-as-of returned %d rows err=%v", len(rows), err)
	}
	if rows[0].SourceKnownAt.Valid || rows[0].KnowledgeTimeBasis != "first_observed_by_system" {
		t.Fatalf("TCMB fabricated source clock: %+v", rows[0])
	}
	var missing bool
	if err := pool.QueryRow(ctx, `SELECT value IS NULL AND value_text = 'null' AND quality_flags->>'missing' = 'true' FROM observation_revisions WHERE series_id = $1 AND observation_time = $2`, validUUID(t, series.ID), timestamp("2024-01-03T00:00:00Z")).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if !missing {
		t.Fatal("TCMB missing period did not create quality evidence")
	}
	var missingFX bool
	if err := pool.QueryRow(ctx, `SELECT value IS NULL AND value_text = 'null' AND quality_flags->>'missing' = 'true' AND quality_flags->>'fx_missing_persisted' = 'true' FROM observation_revisions WHERE series_id = $1 AND observation_time = $2`, validUUID(t, series.ID), timestamp("2024-01-03T00:00:00Z")).Scan(&missingFX); err != nil {
		t.Fatal(err)
	}
	if !missingFX {
		t.Fatal("TCMB missing FX period was not durably retained with FX quality evidence")
	}
	var missingRawSHA string
	if err := pool.QueryRow(ctx, `SELECT ro.content_sha256 FROM observation_revisions AS o JOIN raw_objects AS ro ON ro.id = o.raw_object_id WHERE o.series_id = $1 AND o.observation_time = $2 AND o.quality_flags->>'fx_missing_persisted' = 'true'`, validUUID(t, series.ID), timestamp("2024-01-03T00:00:00Z")).Scan(&missingRawSHA); err != nil {
		t.Fatal(err)
	}
	if missingRawSHA != digest {
		t.Fatalf("missing FX provenance SHA = %q, want %q", missingRawSHA, digest)
	}
	var numericFXCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM fx_quote_revisions WHERE base_currency = 'USD' AND quote_currency = 'TRY'`).Scan(&numericFXCount); err != nil {
		t.Fatal(err)
	}
	if numericFXCount != 4 {
		t.Fatalf("missing FX period was written as a numeric quote: count=%d", numericFXCount)
	}
	if err := runStore.CompleteRun(ctx, runID, "succeeded", map[string]any{"records": len(records)}); err != nil {
		t.Fatal(err)
	}
}

func tcmbRawPayload(body []byte, digest string) ingestion.RawPayload {
	return ingestion.RawPayload{Body: body, MediaType: "application/json", Archive: archive.Reference{Key: archive.ObjectKey(digest), ContentSHA256: digest, ByteLength: int64(len(body)), MediaType: "application/json"}}
}
