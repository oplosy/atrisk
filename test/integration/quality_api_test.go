package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apiquality "github.com/oplosy/atrisk/apps/api/handlers/quality"
	application "github.com/oplosy/atrisk/internal/application/quality"
	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/platform/database"
)

func TestQualityAPI(t *testing.T) {
	migrateTestDatabase(t)
	queries, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	fixture := fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())
	sourceCode := "quality-" + fixture
	if rows, err := queries.InsertDataSource(ctx, database.InsertDataSourceParams{Code: sourceCode, Name: "Quality fixture", AdapterVersion: "test", Metadata: []byte(`{}`)}); err != nil || rows != 1 {
		t.Fatalf("insert data source: rows=%d err=%v", rows, err)
	}
	source, err := queries.GetDataSourceByCode(ctx, sourceCode)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := queries.InsertDataset(ctx, database.InsertDatasetParams{SourceID: validUUID(t, source.ID), ExternalKey: fixture, Name: "Quality fixture", Metadata: []byte(`{}`)}); err != nil || rows != 1 {
		t.Fatalf("insert dataset: rows=%d err=%v", rows, err)
	}
	dataset, err := queries.GetDatasetByExternalKey(ctx, database.GetDatasetByExternalKeyParams{SourceID: validUUID(t, source.ID), ExternalKey: fixture})
	if err != nil {
		t.Fatal(err)
	}
	eventAt := time.Now().UTC().Truncate(time.Second)
	cutoffBeforeRevision := eventAt.Add(24 * time.Hour)
	cutoffAfterRevision := eventAt.Add(36 * time.Hour)
	cutoffAfterLaterRevision := eventAt.Add(48 * time.Hour)
	observationAt := cutoffBeforeRevision.Add(-48 * time.Hour)
	policy := []byte(`{"version":"fixture-v1","expected":"irregular","max_age":"240h"}`)
	seriesCode := "QUALITY-" + fixture
	if rows, err := queries.InsertSeries(ctx, database.InsertSeriesParams{DatasetID: validUUID(t, dataset.ID), SourceCode: seriesCode, Name: "Quality fixture series", Unit: "USD", Frequency: "irregular", FreshnessPolicy: policy}); err != nil || rows != 1 {
		t.Fatalf("insert series: rows=%d err=%v", rows, err)
	}
	series, err := queries.GetSeriesBySourceCode(ctx, database.GetSeriesBySourceCodeParams{DatasetID: validUUID(t, dataset.ID), SourceCode: seriesCode})
	if err != nil {
		t.Fatal(err)
	}
	optionalCode := seriesCode + "-OPTIONAL"
	if rows, err := queries.InsertSeries(ctx, database.InsertSeriesParams{DatasetID: validUUID(t, dataset.ID), SourceCode: optionalCode, Name: "Optional quality fixture", Unit: "USD", Frequency: "irregular", FreshnessPolicy: policy}); err != nil || rows != 1 {
		t.Fatalf("insert optional series: rows=%d err=%v", rows, err)
	}
	optionalSeries, err := queries.GetSeriesBySourceCode(ctx, database.GetSeriesBySourceCodeParams{DatasetID: validUUID(t, dataset.ID), SourceCode: optionalCode})
	if err != nil {
		t.Fatal(err)
	}
	rawSHA := archive.SHA256Hex([]byte(fixture + "-raw"))
	if rows, err := queries.InsertRawObject(ctx, database.InsertRawObjectParams{ContentSha256: rawSHA, ObjectKey: archive.ObjectKey(rawSHA), MediaType: "application/json", ByteLength: int64(len(fixture)), RetrievedAt: timestamp(eventAt.Format(time.RFC3339Nano)), RequestMetadata: []byte(`{"fixture":true}`)}); err != nil || rows != 1 {
		t.Fatalf("insert raw object: rows=%d err=%v", rows, err)
	}
	var rawID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM raw_objects WHERE content_sha256=$1`, rawSHA).Scan(&rawID); err != nil {
		t.Fatal(err)
	}
	seriesID := validUUID(t, series.ID)
	_, err = pool.Exec(ctx, `INSERT INTO observation_revisions (series_id, observation_time, value, system_known_at, knowledge_time_basis, raw_object_id, quality_flags) VALUES
($1,$2,20,$3,'first_observed_by_system',$6,'{}'),
($1,$2,21,$4,'first_observed_by_system',$6,'{}'),
($1,$2,22,$5,'first_observed_by_system',$6,'{}')`, seriesID, observationAt, eventAt.Add(time.Hour), eventAt.Add(26*time.Hour), eventAt.Add(40*time.Hour), rawID)
	if err != nil {
		t.Fatal(err)
	}
	h := apiquality.New(application.Service{Queries: queries})
	request := map[string]any{
		"from":  observationAt.Add(-time.Second).Format(time.RFC3339Nano),
		"to":    observationAt.Add(time.Second).Format(time.RFC3339Nano),
		"as_of": cutoffBeforeRevision.Format(time.RFC3339Nano),
		"inputs": []map[string]any{
			{"series_id": series.ID.String(), "required": true},
			{"series_id": optionalSeries.ID.String(), "required": false},
		},
	}
	body, _ := json.Marshal(request)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/quality/evaluate", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		State string `json:"state"`
		Items []struct {
			Classification string    `json:"classification"`
			State          string    `json:"state"`
			PolicyVersion  string    `json:"policy_version"`
			EvaluatedAt    time.Time `json:"evaluated_at"`
			Reasons        []struct {
				Code string `json:"code"`
			} `json:"reasons"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.State != "degraded" || len(response.Items) != 2 {
		t.Fatalf("unexpected result: %+v", response)
	}
	item := response.Items[0]
	if item.Classification != "fresh" || item.State != "valid" || item.PolicyVersion != "fixture-v1" || !item.EvaluatedAt.Equal(cutoffBeforeRevision) {
		t.Fatalf("unexpected as-of quality item: %+v", item)
	}
	foundRevision := false
	for _, reason := range item.Reasons {
		if reason.Code == "OBSERVATION_REVISED" {
			foundRevision = true
		}
	}
	if foundRevision {
		t.Fatalf("future revision leaked across the system-as-of cutoff: %+v", item.Reasons)
	}
	if response.Items[1].Classification != "missing" || response.Items[1].State != "degraded" {
		t.Fatalf("optional missing input did not degrade aggregate: %+v", response.Items[1])
	}

	request["as_of"] = cutoffAfterRevision.Format(time.RFC3339Nano)
	body, _ = json.Marshal(request)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/quality/evaluate", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("second status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Items[0].Classification != "revised" {
		t.Fatalf("as-of evaluation did not include known revision: %+v", response.Items[0])
	}
	foundRevision = false
	for _, reason := range response.Items[0].Reasons {
		if reason.Code == "OBSERVATION_REVISED" {
			foundRevision = true
		}
	}
	if !foundRevision {
		t.Fatalf("revision reason missing: %+v", response.Items[0].Reasons)
	}
	request["as_of"] = cutoffAfterLaterRevision.Format(time.RFC3339Nano)
	body, _ = json.Marshal(request)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/quality/evaluate", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("third status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Items[0].Classification != "revised" {
		t.Fatalf("latest known revision did not preserve revision classification: %+v", response.Items[0])
	}
}
