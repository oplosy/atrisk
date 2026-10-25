package collector

import (
	"encoding/json"
	"strings"
	"testing"
)

const testPolicy = `{"version":"v1","expected":"business_daily","max_age":"P4D"}`

func validCatalogConfig() FileConfig {
	return FileConfig{DatabaseURLEnv: "ATLASRISK_DATABASE_URL", Archive: ArchiveConfig{
		Endpoint: "http://127.0.0.1:53900", Region: "garage", Bucket: "atlasrisk-raw", AccessKeyEnv: "AWS_ACCESS_KEY_ID", SecretKeyEnv: "AWS_SECRET_ACCESS_KEY",
	}, Catalog: CatalogConfig{Sources: []CatalogSource{{
		Code: "tcmb", Name: "TCMB EVDS", Provider: "tcmb",
		Datasets: []CatalogDataset{{ExternalKey: "evds-fx", Name: "EVDS exchange rates", Series: []CatalogSeries{{
			SourceCode: "TP.DK.USD.A", Name: "USD/TRY buying", Unit: "TRY", Frequency: "business_daily",
			SourceTimezone: "Europe/Istanbul", FreshnessPolicy: json.RawMessage(testPolicy),
		}}}},
	}}}, Schedules: []ScheduleInput{{
		Name: "tcmb-usd", Provider: "tcmb", SourceCode: "tcmb", DatasetKey: "evds-fx", SeriesCode: "TP.DK.USD.A",
		CredentialEnv: "TCMB_API_KEY", Request: json.RawMessage(`{"series":["TP.DK.USD.A"]}`),
		IntervalSeconds: 86400, MaxAttempts: 3, LeaseSeconds: 120,
	}}}
}

func TestFileConfigAcceptsCatalogCodeSchedules(t *testing.T) {
	if err := validCatalogConfig().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFileConfigRejectsInvalidCatalog(t *testing.T) {
	cases := map[string]func(*FileConfig){
		"missing policy": func(c *FileConfig) { c.Catalog.Sources[0].Datasets[0].Series[0].FreshnessPolicy = nil },
		"bad timezone":   func(c *FileConfig) { c.Catalog.Sources[0].Datasets[0].Series[0].SourceTimezone = "Mars/Olympus" },
		"blank unit":     func(c *FileConfig) { c.Catalog.Sources[0].Datasets[0].Series[0].Unit = " " },
		"provider":       func(c *FileConfig) { c.Catalog.Sources[0].Provider = "yahoo" },
		"duplicate source": func(c *FileConfig) {
			c.Catalog.Sources = append(c.Catalog.Sources, c.Catalog.Sources[0])
		},
		"unknown series":     func(c *FileConfig) { c.Schedules[0].SeriesCode = "TP.DK.EUR.A" },
		"provider mismatch":  func(c *FileConfig) { c.Catalog.Sources[0].Provider = "fred" },
		"code and id":        func(c *FileConfig) { c.Schedules[0].SourceID = "11111111-1111-4111-8111-111111111111" },
		"series without set": func(c *FileConfig) { c.Schedules[0].DatasetKey = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			config := validCatalogConfig()
			mutate(&config)
			if err := config.Validate(); err == nil {
				t.Fatal("invalid catalog configuration accepted")
			}
		})
	}
}

func TestCatalogResolveMatchesIDSchedules(t *testing.T) {
	ids := CatalogIDs{
		sources:  map[string]string{"tcmb": "11111111-1111-4111-8111-111111111111"},
		datasets: map[string]string{catalogKey("11111111-1111-4111-8111-111111111111", "evds-fx"): "22222222-2222-4222-8222-222222222222"},
		series:   map[string]string{catalogKey("22222222-2222-4222-8222-222222222222", "TP.DK.USD.A"): "33333333-3333-4333-8333-333333333333"},
	}
	byCode := validCatalogConfig().Schedules[0]
	resolved, err := ids.Resolve(byCode)
	if err != nil {
		t.Fatal(err)
	}
	byID := byCode
	byID.SourceCode, byID.DatasetKey, byID.SeriesCode = "", "", ""
	byID.SourceID, byID.DatasetID, byID.SeriesID = "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"
	first, err := NormalizeSchedule(resolved)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NormalizeSchedule(byID)
	if err != nil {
		t.Fatal(err)
	}
	if first.RequestFingerprint != second.RequestFingerprint {
		t.Fatal("resolved catalog schedule fingerprint differs from the ID schedule")
	}
	byCode.SeriesCode = "TP.DK.EUR.A"
	if _, err := ids.Resolve(byCode); err == nil || !strings.Contains(err.Error(), "unregistered series") {
		t.Fatalf("unknown series resolved: %v", err)
	}
}
