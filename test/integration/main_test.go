package integration

import (
	"os"
	"testing"

	"github.com/oplosy/atrisk/test/internal/testschema"
)

func TestMain(m *testing.M) {
	os.Exit(testschema.Run(m, testDatabaseEnv))
}
