package database

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Port 1 on loopback refuses connections, so these tests never reach a server.
const unreachableReleaseDatabase = "postgres://atrisk:secret@127.0.0.1:1/atrisk?sslmode=disable&connect_timeout=2"

func TestApplyMigrationsAcceptsReleaseTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := ApplyMigrations(ctx, unreachableReleaseDatabase, "db/migrations")
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), "refusing migration target") {
		t.Fatalf("release migrations must not apply the isolated-test guard: %v", err)
	}
	if !strings.Contains(err.Error(), "ping migration database") {
		t.Fatalf("expected the connection to be attempted, got: %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatalf("error leaked the database password: %v", err)
	}
}

func TestMigrateStillRefusesNonIsolatedTargets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for name, migrate := range map[string]func() error{
		"Migrate":         func() error { return Migrate(ctx, unreachableReleaseDatabase, "db/migrations") },
		"MigrateInSchema": func() error { return MigrateInSchema(ctx, unreachableReleaseDatabase, "db/migrations", "previous") },
	} {
		if err := migrate(); err == nil || !strings.Contains(err.Error(), "refusing migration target") {
			t.Fatalf("%s must refuse a non-isolated target, got: %v", name, err)
		}
	}
}

func TestApplyMigrationsRequiresDirectory(t *testing.T) {
	err := ApplyMigrations(context.Background(), unreachableReleaseDatabase, "")
	if err == nil || !strings.Contains(err.Error(), "migrations directory is required") {
		t.Fatalf("expected a missing-directory error, got: %v", err)
	}
}
