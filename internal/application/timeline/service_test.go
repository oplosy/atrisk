package timeline

import (
	"testing"
	"time"
)

func TestTimelineCursorRoundTripAndBounds(t *testing.T) {
	for _, want := range []seriesCursor{{DataSourceCode: "fred", SourceCode: "GDP", ID: "00000000-0000-0000-0000-000000000001"}, {DataSourceCode: "tcmb", SourceCode: "USDTRY", ID: "00000000-0000-0000-0000-000000000002"}} {
		got, err := decodeSeriesCursor(encodeSeriesCursor(want))
		if err != nil || got != want {
			t.Fatalf("cursor round trip: got=%+v err=%v", got, err)
		}
	}
	if _, err := decodeSeriesCursor("not-a-cursor"); err != ErrInvalidCursor {
		t.Fatalf("invalid cursor error=%v", err)
	}
	if validateLimit(1) != nil || validateLimit(200) != nil || validateLimit(0) != ErrInvalidLimit || validateLimit(201) != ErrInvalidLimit {
		t.Fatal("limit bounds changed")
	}
}

func TestObservationCursorRoundTrip(t *testing.T) {
	want := observationCursor{ObservationTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), SystemKnownAt: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), ID: "00000000-0000-0000-0000-000000000001"}
	got, err := decodeObservationCursor(encodeObservationCursor(want))
	if err != nil || !got.ObservationTime.Equal(want.ObservationTime) || !got.SystemKnownAt.Equal(want.SystemKnownAt) || got.ID != want.ID {
		t.Fatalf("cursor round trip: got=%+v err=%v", got, err)
	}
}
