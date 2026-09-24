package database

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrate applies forward-only Goose SQL migrations from migrationsDir. The
// caller must supply an explicitly isolated database URL for test migrations.
func Migrate(ctx context.Context, databaseURL, migrationsDir string) error {
	if databaseURL == "" {
		return fmt.Errorf("database URL is required")
	}
	if migrationsDir == "" {
		return fmt.Errorf("migrations directory is required")
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping migration database: %w", err)
	}

	goose.SetDialect("postgres")
	if err := goose.UpContext(ctx, db, migrationsDir); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
