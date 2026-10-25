package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/oplosy/atrisk/internal/platform/database"
)

// TestMain runs the package in a fresh schema of the isolated database, so
// fixtures with fixed identities can run again without resetting the database.
// PGOPTIONS reaches every pgx connection and the collector child process; a
// caller that already sets PGOPTIONS keeps its own search path.
func TestMain(m *testing.M) {
	os.Exit(runInFreshSchema(m))
}

func runInFreshSchema(m *testing.M) int {
	dsn := os.Getenv(testDatabaseEnv)
	if database.ValidateIsolatedTestDatabaseURL(dsn) != nil || os.Getenv("PGOPTIONS") != "" {
		return m.Run()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := database.OpenPool(ctx, dsn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open isolated test database: %v\n", err)
		return 1
	}
	defer pool.Close()
	schema := fmt.Sprintf("itest_%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		fmt.Fprintf(os.Stderr, "create test schema: %v\n", err)
		return 1
	}
	defer func() {
		if _, err := pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			fmt.Fprintf(os.Stderr, "drop test schema %s: %v\n", schema, err)
		}
	}()
	if err := os.Setenv("PGOPTIONS", "-c search_path="+schema); err != nil {
		fmt.Fprintf(os.Stderr, "set test schema: %v\n", err)
		return 1
	}
	return m.Run()
}
