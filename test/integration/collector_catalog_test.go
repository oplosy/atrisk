package integration

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/oplosy/atrisk/internal/collector"
)

func TestCollectorCatalog(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	stamp := time.Now().UTC().Format("20060102150405.000000000")
	store := collector.Store{Pool: pool}
	sourceCode := "catalog-tcmb-" + stamp
	catalog := collector.CatalogConfig{Sources: []collector.CatalogSource{{
		Code: sourceCode, Name: "TCMB EVDS", Provider: "tcmb",
		Datasets: []collector.CatalogDataset{{ExternalKey: "evds-fx", Name: "EVDS exchange rates", Series: []collector.CatalogSeries{{
			SourceCode: "TP.DK.USD.A", Name: "USD/TRY buying", Unit: "TRY", Frequency: "business_daily", SourceTimezone: "Europe/Istanbul",
			FreshnessPolicy: json.RawMessage(`{"version":"v1","expected":"business_daily","max_age":"P4D"}`),
		}}}},
	}}}

	first, err := store.RegisterCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	// Registration is idempotent: a restarted collector reuses the same rows.
	again, err := store.RegisterCatalog(ctx, catalog)
	if err != nil {
		t.Fatal(err)
	}
	input := collector.ScheduleInput{
		Name: "catalog-schedule-" + stamp, Provider: "tcmb", SourceCode: sourceCode, DatasetKey: "evds-fx", SeriesCode: "TP.DK.USD.A",
		CredentialEnv: "ATLASRISK_TEST_TCMB_KEY", Request: json.RawMessage(`{"series":["TP.DK.USD.A"]}`),
		IntervalSeconds: 86400, MaxAttempts: 3, LeaseSeconds: 120, NextRunAt: timePointer(time.Now().Add(24 * time.Hour)),
	}
	resolvedFirst, err := first.Resolve(input)
	if err != nil {
		t.Fatal(err)
	}
	resolvedAgain, err := again.Resolve(input)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedFirst.SeriesID == "" || resolvedFirst.SeriesID != resolvedAgain.SeriesID {
		t.Fatalf("re-registration resolved series %q, first %q", resolvedAgain.SeriesID, resolvedFirst.SeriesID)
	}
	if got := countRows(t, pool, `SELECT count(*)::int FROM series s JOIN datasets d ON d.id = s.dataset_id JOIN data_sources src ON src.id = d.source_id
WHERE src.code = $1 AND s.source_timezone = 'Europe/Istanbul' AND s.freshness_policy->>'max_age' = 'P4D'`, sourceCode); got != 1 {
		t.Fatalf("registered series rows = %d, want 1", got)
	}
	schedule, err := collector.NormalizeSchedule(resolvedFirst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertSchedule(ctx, schedule); err != nil {
		t.Fatalf("catalog schedule registration failed: %v", err)
	}
	if _, err := store.UpsertSchedule(ctx, collector.Schedule{ScheduleInput: input}); err == nil {
		t.Fatal("unresolved catalog codes were registered")
	}

	// A changed declaration fails closed and rolls back the whole catalog.
	changed := catalog
	changed.Sources = []collector.CatalogSource{catalog.Sources[0], {Code: "catalog-new-" + stamp, Name: "New source", Provider: "fred"}}
	changed.Sources[0].Datasets = []collector.CatalogDataset{catalog.Sources[0].Datasets[0]}
	changed.Sources[0].Datasets[0].Series = []collector.CatalogSeries{catalog.Sources[0].Datasets[0].Series[0]}
	changed.Sources[0].Datasets[0].Series[0].Unit = "USD"
	if _, err := store.RegisterCatalog(ctx, changed); !errors.Is(err, collector.ErrCatalogChanged) {
		t.Fatalf("changed series unit returned %v", err)
	}
	if got := countRows(t, pool, `SELECT count(*)::int FROM data_sources WHERE code = $1`, "catalog-new-"+stamp); got != 0 {
		t.Fatal("failed catalog registration left a new source behind")
	}
	if got := countRows(t, pool, `SELECT count(*)::int FROM series WHERE id = $1::uuid AND unit = 'TRY'`, resolvedFirst.SeriesID); got != 1 {
		t.Fatal("failed catalog registration changed the registered series")
	}
}

func timePointer(value time.Time) *time.Time { return &value }
