package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrate applies forward-only Goose SQL migrations from migrationsDir. The
// caller must supply an explicitly isolated database URL for test migrations.
func Migrate(ctx context.Context, databaseURL, migrationsDir string) error {
	return migrate(ctx, databaseURL, migrationsDir, "")
}

// MigrateInSchema applies migrations to a validated isolated database schema.
// It exists for previous-version upgrade tests that must leave other schemas
// in the isolated database untouched; normal migrations use Migrate.
func MigrateInSchema(ctx context.Context, databaseURL, migrationsDir, schemaName string) error {
	quotedSchema, err := quoteMigrationSchema(schemaName)
	if err != nil {
		return err
	}
	goose.SetTableName(quotedSchema + "." + goose.DefaultTablename)
	defer goose.SetTableName(goose.DefaultTablename)
	return migrate(ctx, databaseURL, migrationsDir, quotedSchema)
}

func migrate(ctx context.Context, databaseURL, migrationsDir, quotedSchema string) error {
	if err := ValidateIsolatedTestDatabaseURL(databaseURL); err != nil {
		return fmt.Errorf("refusing migration target: %w", err)
	}
	if migrationsDir == "" {
		return fmt.Errorf("migrations directory is required")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping migration database: %w", err)
	}
	if quotedSchema != "" {
		if _, err := db.ExecContext(ctx, "SET search_path TO "+quotedSchema+", public"); err != nil {
			return fmt.Errorf("set migration schema: %w", err)
		}
	}

	goose.SetDialect("postgres")
	if err := goose.UpContext(ctx, db, migrationsDir); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

func quoteMigrationSchema(schemaName string) (string, error) {
	if schemaName == "" || len(schemaName) > 63 {
		return "", fmt.Errorf("migration schema name must be a non-empty PostgreSQL identifier")
	}
	if schemaName[0] != '_' && (schemaName[0] < 'a' || schemaName[0] > 'z') {
		return "", fmt.Errorf("migration schema name must be a lowercase PostgreSQL identifier")
	}
	for _, r := range schemaName {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return "", fmt.Errorf("migration schema name must be a lowercase PostgreSQL identifier")
		}
	}
	return `"` + strings.ReplaceAll(schemaName, `"`, `""`) + `"`, nil
}
