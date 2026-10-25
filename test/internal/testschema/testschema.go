// Package testschema runs a test package in a fresh schema of the isolated
// test database, so fixtures with fixed identities can run again without
// resetting the database and leftovers from other packages are invisible.
package testschema

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/oplosy/atrisk/internal/platform/database"
)

// Run creates the schema, points every later connection of this process and
// its children at it through PGOPTIONS (honoured by pgx and libpq), runs the
// tests, and drops only that schema. Without a valid isolated DSN, or when the
// caller already sets PGOPTIONS, it only runs the tests.
func Run(m *testing.M, dsnEnv string) int {
	dsn := os.Getenv(dsnEnv)
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
