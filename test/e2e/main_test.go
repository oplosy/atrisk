package e2e

import (
	"os"
	"testing"

	"github.com/oplosy/atrisk/test/internal/testschema"
)

// TestMain isolates the journey from queued jobs and rows left in other
// schemas; the Python risk worker inherits the same schema via PGOPTIONS.
func TestMain(m *testing.M) {
	os.Exit(testschema.Run(m, "ATLASRISK_TEST_DATABASE_URL"))
}
