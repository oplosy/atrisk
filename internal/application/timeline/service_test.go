package timeline

import (
	"testing"
	"time"
)

func TestTimelineCursorRoundTripAndBounds(t *testing.T) {
	for _, offset := range []int{0, 1, 50, 200} {
		got, err := decodeCursor(encodeCursor(offset))
		if err != nil || got != offset {
			t.Fatalf("cursor round trip: got=%d err=%v", got, err)
		}
	}
	if _, err := decodeCursor("not-a-cursor"); err != ErrInvalidCursor {
		t.Fatalf("invalid cursor error=%v", err)
	}
	if normalizeLimit(0) != 50 || normalizeLimit(201) != 200 {
		t.Fatal("limit bounds changed")
	}
}


func TestObservationCursorRoundTrip(t *testing.T) {
	want := observationCursor{ObservationTime: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), SystemKnownAt: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), ID: "00000000-0000-0000-0000-000000000001"}
	got, err := decodeObservationCursor(encodeObservationCursor(want))
	if err != nil || !got.ObservationTime.Equal(want.ObservationTime) || !got.SystemKnownAt.Equal(want.SystemKnownAt) || got.ID != want.ID { t.Fatalf("cursor round trip: got=%+v err=%v", got, err) }
}
