package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apiTimeline "github.com/oplosy/atrisk/apps/api/handlers/timeline"
	application "github.com/oplosy/atrisk/internal/application/timeline"
	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/platform/database"
)

func TestPointInTimeAPI(t *testing.T) {
	migrateTestDatabase(t)
	queries, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	fixture := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	if rows, err := queries.InsertDataSource(ctx, database.InsertDataSourceParams{Code: "fred-" + fixture, Name: "FRED fixture", AdapterVersion: "test", Metadata: []byte(`{}`)}); err != nil || rows != 1 {
		t.Fatalf("insert source: %d %v", rows, err)
	}
	source, err := queries.GetDataSourceByCode(ctx, "fred-"+fixture)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := queries.InsertDataset(ctx, database.InsertDatasetParams{SourceID: validUUID(t, source.ID), ExternalKey: "dataset-" + fixture, Name: "fixture", Metadata: []byte(`{}`)}); err != nil || rows != 1 {
		t.Fatalf("insert dataset: %d %v", rows, err)
	}
	dataset, err := queries.GetDatasetByExternalKey(ctx, database.GetDatasetByExternalKeyParams{SourceID: validUUID(t, source.ID), ExternalKey: "dataset-" + fixture})
	if err != nil {
		t.Fatal(err)
	}
	code := "SERIES-" + fixture
	if rows, err := queries.InsertSeries(ctx, database.InsertSeriesParams{DatasetID: validUUID(t, dataset.ID), SourceCode: code, Name: "Fixture series", Unit: "USD", Frequency: "daily", FreshnessPolicy: []byte(`{"max_age":"P2D"}`)}); err != nil || rows != 1 {
		t.Fatalf("insert series: %d %v", rows, err)
	}
	series, err := queries.GetSeriesBySourceCode(ctx, database.GetSeriesBySourceCodeParams{DatasetID: validUUID(t, dataset.ID), SourceCode: code})
	if err != nil {
		t.Fatal(err)
	}
	raw1, raw2, raw3 := archive.SHA256Hex([]byte("initial")), archive.SHA256Hex([]byte("revision")), archive.SHA256Hex([]byte("late-source"))
	for _, raw := range []struct {
		sha string
		at  string
	}{{raw1, "2024-01-11T00:00:00Z"}, {raw2, "2024-01-21T00:00:00Z"}, {raw3, "2024-01-12T00:00:00Z"}} {
		if rows, err := queries.InsertRawObject(ctx, database.InsertRawObjectParams{ContentSha256: raw.sha, ObjectKey: archive.ObjectKey(raw.sha), MediaType: "application/json", ByteLength: int64(len(raw.sha)), RetrievedAt: timestamp(raw.at), RequestMetadata: []byte(`{"fixture":true}`)}); err != nil || rows != 1 {
			t.Fatalf("insert raw: %d %v", rows, err)
		}
	}
	var rawID1, rawID2, rawID3 string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM raw_objects WHERE content_sha256=$1`, raw1).Scan(&rawID1); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM raw_objects WHERE content_sha256=$1`, raw2).Scan(&rawID2); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id::text FROM raw_objects WHERE content_sha256=$1`, raw3).Scan(&rawID3); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO observation_revisions (series_id, observation_time, value, source_known_at, system_known_at, knowledge_time_basis, raw_object_id, quality_flags) VALUES ($1,$2,100,$3,$4,'source_published_at',$5,'{"fixture":true}'),($1,$2,110,$6,$7,'source_published_at',$8,'{"fixture":true}'),($1,$2,90,$9,$10,'source_published_at',$11,'{"fixture":true}')`, validUUID(t, series.ID), timestamp("2024-01-01T00:00:00Z"), timestamp("2024-01-10T00:00:00Z"), timestamp("2024-01-11T00:00:00Z"), rawID1, timestamp("2024-01-20T00:00:00Z"), timestamp("2024-01-21T00:00:00Z"), rawID2, timestamp("2024-01-25T00:00:00Z"), timestamp("2024-01-12T00:00:00Z"), rawID3)
	if err != nil {
		t.Fatal(err)
	}
	secondCode := "SERIES-SECOND-" + fixture
	if rows, err := queries.InsertSeries(ctx, database.InsertSeriesParams{DatasetID: validUUID(t, dataset.ID), SourceCode: secondCode, Name: "Second fixture series", Unit: "USD", Frequency: "daily", FreshnessPolicy: []byte(`{}`)}); err != nil || rows != 1 {
		t.Fatalf("insert second series: %d %v", rows, err)
	}
	secondSeries, err := queries.GetSeriesBySourceCode(ctx, database.GetSeriesBySourceCodeParams{DatasetID: validUUID(t, dataset.ID), SourceCode: secondCode})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO observation_revisions (series_id, observation_time, value, source_known_at, system_known_at, knowledge_time_basis, raw_object_id, quality_flags) VALUES ($1,$2,200,$3,$4,'source_published_at',$5,'{"fixture":true}')`, validUUID(t, secondSeries.ID), timestamp("2024-01-02T00:00:00Z"), timestamp("2024-01-11T00:00:00Z"), timestamp("2024-01-11T00:00:00Z"), rawID1); err != nil {
		t.Fatal(err)
	}
	h := apiTimeline.New(application.Service{Queries: queries})
	get := func(path string) map[string]any {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	latest := get("/v1/series/" + series.ID.String() + "/observations?from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z")
	if got := latest["items"].([]any)[0].(map[string]any)["value"]; got != "110" {
		t.Fatalf("latest=%v want 110", got)
	}
	sourceAsOf := get("/v1/series/" + series.ID.String() + "/observations?mode=source-as-of&as_of=2024-01-15T00:00:00Z&from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z")
	if got := sourceAsOf["items"].([]any)[0].(map[string]any)["value"]; got != "100" {
		t.Fatalf("source-as-of=%v want 100", got)
	}
	systemAsOf := get("/v1/series/" + series.ID.String() + "/observations?mode=system-as-of&as_of=2024-01-15T00:00:00Z&from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z")
	if got := systemAsOf["items"].([]any)[0].(map[string]any)["value"]; got != "90" {
		t.Fatalf("system-as-of=%v want 90", got)
	}
	if got := systemAsOf["items"].([]any)[0].(map[string]any)["raw_provenance_id"]; got != rawID3 {
		t.Fatalf("provenance=%v want %s", got, rawID3)
	}
	revisions := get("/v1/series/" + series.ID.String() + "/revisions?from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z&limit=2")
	if len(revisions["items"].([]any)) != 2 || revisions["has_more"] != true || revisions["next_cursor"] == nil {
		t.Fatalf("revisions endpoint did not expose stable page: %v", revisions)
	}
	revisionsNext := get("/v1/series/" + series.ID.String() + "/revisions?from=2024-01-01T00:00:00Z&to=2024-01-02T00:00:00Z&limit=2&cursor=" + revisions["next_cursor"].(string))
	if len(revisionsNext["items"].([]any)) != 1 || revisionsNext["has_more"] != false {
		t.Fatalf("revisions cursor did not return final revision: %v", revisionsNext)
	}
	if rows, err := queries.InsertDataSource(ctx, database.InsertDataSourceParams{Code: "tcmb-" + fixture, Name: "TCMB fixture", AdapterVersion: "test", Metadata: []byte(`{}`)}); err != nil || rows != 1 {
		t.Fatalf("insert TCMB source: %d %v", rows, err)
	}
	tcmbSource, err := queries.GetDataSourceByCode(ctx, "tcmb-"+fixture)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := queries.InsertDataset(ctx, database.InsertDatasetParams{SourceID: validUUID(t, tcmbSource.ID), ExternalKey: "tcmb-dataset-" + fixture, Name: "TCMB fixture", Metadata: []byte(`{}`)}); err != nil || rows != 1 {
		t.Fatalf("insert TCMB dataset: %d %v", rows, err)
	}
	tcmbDataset, err := queries.GetDatasetByExternalKey(ctx, database.GetDatasetByExternalKeyParams{SourceID: validUUID(t, tcmbSource.ID), ExternalKey: "tcmb-dataset-" + fixture})
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := queries.InsertSeries(ctx, database.InsertSeriesParams{DatasetID: validUUID(t, tcmbDataset.ID), SourceCode: "TP.TEST." + fixture, Name: "TCMB fixture series", Unit: "TRY", Frequency: "daily", FreshnessPolicy: []byte(`{}`)}); err != nil || rows != 1 {
		t.Fatalf("insert TCMB series: %d %v", rows, err)
	}
	tcmbSeries, err := queries.GetSeriesBySourceCode(ctx, database.GetSeriesBySourceCodeParams{DatasetID: validUUID(t, tcmbDataset.ID), SourceCode: "TP.TEST." + fixture})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/series/"+tcmbSeries.ID.String()+"/observations?mode=source-as-of&as_of=2024-01-15T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("unsupported TCMB source-as-of status=%d body=%s", rec.Code, rec.Body.String())
	}
	var capability map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &capability); err != nil || capability["code"] != "SOURCE_AS_OF_UNSUPPORTED" {
		t.Fatalf("missing capability response: %s", rec.Body.String())
	}
	combinedFirst := get("/v1/timeline?series_id=" + series.ID.String() + "," + secondSeries.ID.String() + "&mode=combined&from=2024-01-01T00:00:00Z&to=2024-01-03T00:00:00Z&limit=1")
	if combinedFirst["has_more"] != true || combinedFirst["next_cursor"] == nil {
		t.Fatalf("combined first page missing stable cursor: %v", combinedFirst)
	}
	combinedSecond := get("/v1/timeline?series_id=" + series.ID.String() + "," + secondSeries.ID.String() + "&mode=combined&from=2024-01-01T00:00:00Z&to=2024-01-03T00:00:00Z&limit=1&cursor=" + combinedFirst["next_cursor"].(string))
	if len(combinedSecond["items"].([]any)) != 1 || combinedSecond["items"].([]any)[0].(map[string]any)["series_id"] != secondSeries.ID.String() {
		t.Fatalf("combined cursor did not advance to second series: %v", combinedSecond)
	}
	page := get("/v1/series?limit=1")
	if page["has_more"] != true || page["next_cursor"] == nil || len(page["items"].([]any)) != 1 {
		t.Fatalf("series page did not expose stable pagination metadata: %v", page)
	}
	nextPage := get("/v1/series?limit=1&cursor=" + page["next_cursor"].(string))
	if len(nextPage["items"].([]any)) != 1 || nextPage["items"].([]any)[0].(map[string]any)["id"] == page["items"].([]any)[0].(map[string]any)["id"] {
		t.Fatalf("series keyset cursor did not advance: first=%v next=%v", page, nextPage)
	}
}
