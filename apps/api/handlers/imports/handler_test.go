package imports

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestWriteErrorIncludesRequestID(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeError(recorder, 415, "UNSUPPORTED_MEDIA_TYPE", "file must use a CSV media type")
	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["request_id"] == "" || body["code"] != "UNSUPPORTED_MEDIA_TYPE" {
		t.Fatalf("error body=%v", body)
	}
}
