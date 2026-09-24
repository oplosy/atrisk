package database

import (
	"fmt"
	"net/url"
)

const isolatedTestDatabaseName = "atrisk_test"
const isolatedTestDatabaseHost = "127.0.0.1"

// ValidateIsolatedTestDatabaseURL accepts only the local, explicitly named
// database used by migration/integration tests. It intentionally does not
// include the DSN in errors, because a DSN may contain credentials.
func ValidateIsolatedTestDatabaseURL(dsn string) error {
	if dsn == "" {
		return fmt.Errorf("test database URL is required")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return fmt.Errorf("test database URL must use a PostgreSQL scheme")
	}
	if u.Hostname() != isolatedTestDatabaseHost || u.Port() == "" {
		return fmt.Errorf("test database URL must use loopback host %s and an explicit port", isolatedTestDatabaseHost)
	}
	if u.Path != "/"+isolatedTestDatabaseName {
		return fmt.Errorf("test database URL must target the exact isolated database %s", isolatedTestDatabaseName)
	}
	if u.User == nil || u.User.Username() == "" {
		return fmt.Errorf("test database URL must include an explicit database user")
	}
	return nil
}
