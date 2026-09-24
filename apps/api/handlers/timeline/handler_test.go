package timeline

import (
	"net/http/httptest"
	"testing"

	application "github.com/oplosy/atrisk/internal/application/timeline"
)

func TestParseObservationRequestKeepsExplicitClockMode(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/series/id/observations?mode=system-as-of&from=2024-01-01T00:00:00Z&to=2024-02-01T00:00:00Z&as_of=2024-01-15T00:00:00Z&limit=20", nil)
	got, err := parseObservationRequest(r, "observations")
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != application.ModeSystemAsOf || got.Limit != 20 || got.AsOf.IsZero() {
		t.Fatalf("unexpected request: %+v", got)
	}
}

func TestParseObservationRequestRejectsNonRFC3339Clock(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/series/id/observations?as_of=2024-01-15", nil)
	if _, err := parseObservationRequest(r, "observations"); err == nil {
		t.Fatal("date-only as_of unexpectedly accepted")
	}
}

func TestRevisionsRouteHasExplicitRevisionMode(t *testing.T) {
	r := httptest.NewRequest("GET", "/v1/series/id/revisions?mode=latest", nil)
	got, err := parseObservationRequest(r, "revisions")
	if err != nil { t.Fatal(err) }
	if got.Mode != application.ModeRevisions { t.Fatalf("revisions route mode=%q", got.Mode) }
}
