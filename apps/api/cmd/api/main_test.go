package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

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

func TestAPIServerDefaultsLoopbackAndFiniteDeadlines(t *testing.T) {
	server := newAPIServer("127.0.0.1:8080", http.NewServeMux())
	if server.Addr != "127.0.0.1:8080" {
		t.Fatalf("default address=%q", server.Addr)
	}
	if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Fatalf("server deadlines must be finite: %+v", server)
	}
}

func TestAPIServerClosesStalledBody(t *testing.T) {
	server := startTestAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	conn, err := net.Dial("tcp", server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 4\r\nConnection: close\r\n\r\nx"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("stalled request did not terminate: %v", err)
	}
}

func TestAPIServerAcceptsCompleteBody(t *testing.T) {
	server := startTestAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			http.Error(w, "body", http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	response, err := http.Post("http://"+server.Addr, "text/plain", strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("complete request status=%d", response.StatusCode)
	}
}

func startTestAPIServer(t *testing.T, handler http.Handler) *http.Server {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newAPIServerWithTimeouts(listener.Addr().String(), handler, 100*time.Millisecond, 250*time.Millisecond, time.Second, time.Second)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	return server
}
