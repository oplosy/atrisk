package timeline

import (
	"encoding/json"
	"net/http"
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
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != application.ModeRevisions {
		t.Fatalf("revisions route mode=%q", got.Mode)
	}
}

func TestTimelineHandlerRejectsInvalidQueryInputs(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
		code string
	}{
		{name: "limit", path: "/v1/series?limit=201", code: "INVALID_LIMIT"},
		{name: "mode", path: "/v1/series/00000000-0000-0000-0000-000000000001/observations?mode=revisions", code: "INVALID_MODE"},
		{name: "series id", path: "/v1/series/not-a-uuid", code: "INVALID_SERIES_ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			New(application.Service{}).ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["code"] != tc.code {
				t.Fatalf("code=%v want %s", body["code"], tc.code)
			}
		})
	}
}
