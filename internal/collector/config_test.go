package collector

import (
	"encoding/json"
	"strings"
	"testing"
)

func validScheduleInput() ScheduleInput {
	return ScheduleInput{
		Name: "fred-cpi", Provider: "fred", SourceID: "11111111-1111-4111-8111-111111111111", CredentialEnv: "FRED_API_KEY",
		DatasetID: "22222222-2222-4222-8222-222222222222", SeriesID: "33333333-3333-4333-8333-333333333333",
		Request: json.RawMessage(`{"series_id":"CPIAUCSL","limit":100}`), IntervalSeconds: 3600, MaxAttempts: 3, LeaseSeconds: 120,
	}
}

func TestNormalizeScheduleBindsRequestFingerprint(t *testing.T) {
	first, err := NormalizeSchedule(validScheduleInput())
	if err != nil {
		t.Fatal(err)
	}
	changed := validScheduleInput()
	changed.Request = json.RawMessage(`{"series_id":"UNRATE","limit":100}`)
	second, err := NormalizeSchedule(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.RequestFingerprint == second.RequestFingerprint {
		t.Fatal("changed request reused the old fingerprint")
	}
}

func TestFileConfigRejectsUnknownFieldsAndInlineCredentials(t *testing.T) {
	config := FileConfig{DatabaseURLEnv: "ATLASRISK_TEST_DATABASE_URL", Archive: ArchiveConfig{
		Endpoint: "http://127.0.0.1:52252", Region: "garage", Bucket: "atlasrisk-raw", AccessKeyEnv: "AWS_ACCESS_KEY_ID", SecretKeyEnv: "AWS_SECRET_ACCESS_KEY",
	}, Schedules: []ScheduleInput{validScheduleInput()}}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	config.Archive.Endpoint = "http://user:secret@127.0.0.1:52252"
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("inline archive credentials accepted: %v", err)
	}
}

func TestNormalizeScheduleRejectsInlineProviderCredential(t *testing.T) {
	input := validScheduleInput()
	input.Request = json.RawMessage(`{"series_id":"CPIAUCSL","api_key":"source-secret"}`)
	if _, err := NormalizeSchedule(input); err == nil || !strings.Contains(err.Error(), "credential") {
		t.Fatalf("inline provider credential accepted: %v", err)
	}
}

func TestNormalizeScheduleFingerprintIncludesIdentityAndLease(t *testing.T) {
	first, err := NormalizeSchedule(validScheduleInput())
	if err != nil {
		t.Fatal(err)
	}
	changed := validScheduleInput()
	changed.SourceID = "44444444-4444-4444-8444-444444444444"
	changed.LeaseSeconds = 180
	second, err := NormalizeSchedule(changed)
	if err != nil {
		t.Fatal(err)
	}
	if first.RequestFingerprint == second.RequestFingerprint {
		t.Fatal("schedule identity change reused the old fingerprint")
	}
}

func TestNormalizeScheduleValidatesFXPair(t *testing.T) {
	tcmb := validScheduleInput()
	tcmb.Provider, tcmb.Request = "tcmb", json.RawMessage(`{"series":["TP.DK.USD.A"]}`)
	plain, err := NormalizeSchedule(tcmb)
	if err != nil {
		t.Fatal(err)
	}
	tcmb.FXPair = &FXPair{Base: "USD", Quote: "TRY"}
	paired, err := NormalizeSchedule(tcmb)
	if err != nil {
		t.Fatal(err)
	}
	if plain.RequestFingerprint == paired.RequestFingerprint {
		t.Fatal("fx_pair did not change the fingerprint")
	}
	if !strings.Contains(string(paired.Configuration), `"fx_pair":{"base":"USD","quote":"TRY"}`) {
		t.Fatalf("configuration lost fx_pair: %s", paired.Configuration)
	}
	for name, pair := range map[string]FXPair{"lowercase": {"usd", "TRY"}, "same": {"TRY", "TRY"}, "ticker": {"USDT", "TRY"}} {
		tcmb.FXPair = &pair
		if _, err := NormalizeSchedule(tcmb); err == nil {
			t.Fatalf("%s fx_pair accepted", name)
		}
	}
	fred := validScheduleInput()
	fred.FXPair = &FXPair{Base: "USD", Quote: "TRY"}
	if _, err := NormalizeSchedule(fred); err == nil || !strings.Contains(err.Error(), "only for tcmb") {
		t.Fatalf("fred fx_pair accepted: %v", err)
	}
}
