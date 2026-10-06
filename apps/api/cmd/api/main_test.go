package main

import (
	"strings"
	"testing"

	"github.com/oplosy/atrisk/internal/buildinfo"
)

func TestVersionFormat(t *testing.T) {
	got := buildinfo.Format("api", "test", "go1.27.0")
	if got != "atlasrisk api version test runtime go1.27.0" {
		t.Fatalf("unexpected version output: %q", got)
	}
}

func TestMigrateRequiresDatabaseURL(t *testing.T) {
	var stdout, stderr strings.Builder
	code := runMigrate(nil, func(string) string { return "" }, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "ATLASRISK_DATABASE_URL is required") {
		t.Fatalf("expected exit 2 and a missing-URL message, got %d: %q", code, stderr.String())
	}
}

func TestMigrateRejectsUnexpectedArguments(t *testing.T) {
	var stdout, stderr strings.Builder
	code := runMigrate([]string{"down"}, func(string) string { return "postgres://unused" }, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), `unexpected argument "down"`) {
		t.Fatalf("expected exit 2 for an unexpected argument, got %d: %q", code, stderr.String())
	}
}

func TestMigrateReportsDatabaseFailure(t *testing.T) {
	var stdout, stderr strings.Builder
	env := map[string]string{"ATLASRISK_DATABASE_URL": "postgres://atrisk:secret@127.0.0.1:1/atrisk?sslmode=disable&connect_timeout=2"}
	code := runMigrate([]string{"-dir", "../../../../db/migrations"}, func(key string) string { return env[key] }, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "migrate: ping migration database") {
		t.Fatalf("expected exit 1 with the database failure, got %d: %q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "secret") {
		t.Fatalf("output leaked the database password: %q", stderr.String())
	}
}
