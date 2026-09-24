package database

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	expectedPort, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil || expectedPort == 0 {
		return fmt.Errorf("test database URL must use a valid explicit port")
	}
	if u.Path != "/"+isolatedTestDatabaseName {
		return fmt.Errorf("test database URL must target the exact isolated database %s", isolatedTestDatabaseName)
	}
	if u.User == nil || u.User.Username() == "" {
		return fmt.Errorf("test database URL must include an explicit database user")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fmt.Errorf("test database URL contains an invalid query")
	}
	if values, ok := query["sslmode"]; ok {
		if len(values) != 1 || values[0] != "disable" {
			return fmt.Errorf("test database URL only permits sslmode=disable")
		}
	}
	for key := range query {
		if key != "sslmode" {
			return fmt.Errorf("test database URL contains an unsupported connection setting")
		}
	}

	// Parse the effective pgx configuration as well as the URI itself. This
	// catches service-file and environment defaults that could otherwise alter
	// the connection target after structural URI validation.
	config, err := pgx.ParseConfigWithOptions(dsn, pgx.ParseConfigOptions{
		ParseConfigOptions: pgconn.ParseConfigOptions{ConnStringAllowedKeys: []string{"host", "port", "database", "user", "password", "sslmode"}},
	})
	if err != nil {
		return fmt.Errorf("test database URL effective target could not be parsed")
	}
	if config.Host != isolatedTestDatabaseHost || config.Port != uint16(expectedPort) ||
		config.Database != isolatedTestDatabaseName || config.User != u.User.Username() {
		return fmt.Errorf("test database URL effective target is not the isolated loopback database")
	}
	for _, fallback := range config.Fallbacks {
		if fallback.Host != isolatedTestDatabaseHost || fallback.Port != uint16(expectedPort) {
			return fmt.Errorf("test database URL contains an unsafe connection fallback")
		}
	}
	return nil
}
