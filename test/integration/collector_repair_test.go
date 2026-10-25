package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/collector"
)

// Collector schedules use the maximum interval so a completed schedule from an
// earlier run in a reused test database is not due again during later runs.
const collectorTestInterval = 31 * 24 * 60 * 60

type collectorArchive struct {
	mu      sync.Mutex
	objects map[string][]byte
	fail    error
}

func (a *collectorArchive) Put(_ context.Context, object archive.Object) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fail != nil {
		return a.fail
	}
	if a.objects == nil {
		a.objects = map[string][]byte{}
	}
	a.objects[object.Key] = append([]byte(nil), object.Body...)
	return nil
}

func (a *collectorArchive) Get(_ context.Context, key string) (io.ReadCloser, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	data, ok := a.objects[key]
	if !ok {
		return nil, errors.New("object missing")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

type collectorFixture struct {
	sourceID, datasetID, seriesID string
}

func newCollectorSeries(t *testing.T, pool *pgxpool.Pool, provider, code, sourceCode string) collectorFixture {
	t.Helper()
	ctx := context.Background()
	var f collectorFixture
	if err := pool.QueryRow(ctx, `INSERT INTO data_sources (code, name, adapter_version) VALUES ($1, $2, 'collector-test') RETURNING id::text`, code, provider).Scan(&f.sourceID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO datasets (source_id, external_key, name) VALUES ($1::uuid, 'collector', 'collector') RETURNING id::text`, f.sourceID).Scan(&f.datasetID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO series (dataset_id, source_code, name, unit, frequency) VALUES ($1::uuid, $2, $2, 'lin', 'daily') RETURNING id::text`, f.datasetID, sourceCode).Scan(&f.seriesID); err != nil {
		t.Fatal(err)
	}
	return f
}

func collectorSchedule(t *testing.T, input collector.ScheduleInput) collector.Schedule {
	t.Helper()
	if input.IntervalSeconds == 0 {
		input.IntervalSeconds = collectorTestInterval
	}
	if input.LeaseSeconds == 0 {
		input.LeaseSeconds = 120
	}
	if input.MaxAttempts == 0 {
		input.MaxAttempts = 1
	}
	if input.NextRunAt == nil {
		due := time.Now().UTC().Add(-time.Hour)
		input.NextRunAt = &due
	}
	schedule, err := collector.NormalizeSchedule(input)
	if err != nil {
		t.Fatal(err)
	}
	return schedule
}

func scheduleState(t *testing.T, pool *pgxpool.Pool, id string) (active bool, leaseOwner, lastRunID, lastError string, attempts int, nextRun time.Time) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), `
SELECT active, COALESCE(lease_owner, ''), COALESCE(last_run_id::text, ''), COALESCE(last_error_code, ''), attempt_count, next_run_at
FROM collector_schedules WHERE id = $1::uuid`, id).Scan(&active, &leaseOwner, &lastRunID, &lastError, &attempts, &nextRun); err != nil {
		t.Fatal(err)
	}
	return
}

func latestRunForSource(t *testing.T, pool *pgxpool.Pool, sourceID string) (id, status, errorCode string) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), `
SELECT id::text, status, COALESCE(error_code, '') FROM ingestion_runs
WHERE source_id = $1::uuid ORDER BY started_at DESC, id DESC LIMIT 1`, sourceID).Scan(&id, &status, &errorCode); err != nil {
		t.Fatal(err)
	}
	return
}

func countRows(t *testing.T, pool *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestCollectorRepair(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	stamp := time.Now().UTC().Format("20060102150405.000000000")

	read := func(parts ...string) []byte {
		body, err := os.ReadFile(filepath.Join(append([]string{"..", "..", "test", "fixtures"}, parts...)...))
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	// A trailing newline gives these bodies their own content address, so the
	// fixed raw-object inserts in TestFREDVintage and TestTCMBIngestion still apply.
	fredBody := append(read("fred", "vintage-observations.json"), '\n')
	// Same observations, but the provider reports more rows than one page holds.
	pagedBody := bytes.Replace(bytes.Replace(fredBody, []byte(`"count": 4`), []byte(`"count": 8`), 1), []byte(`"limit": 1000`), []byte(`"limit": 4`), 1)
	tcmbBody := append(read("tcmb", "evds-observations.json"), '\n')
	// The fixture's third candle is deliberately misaligned; serve the two closed, aligned days.
	var klines []json.RawMessage
	if err := json.Unmarshal(read("binance", "daily-klines.json"), &klines); err != nil || len(klines) < 2 {
		t.Fatalf("decode Binance fixture: rows=%d err=%v", len(klines), err)
	}
	klineBody, err := json.Marshal(klines[:2])
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/fred/series/observations" && r.URL.Query().Get("series_id") == "PAGED":
			_, _ = w.Write(pagedBody)
		case r.URL.Path == "/fred/series/observations":
			_, _ = w.Write(fredBody)
		case strings.HasPrefix(r.URL.Path, "/service/evds/"):
			_, _ = w.Write(tcmbBody)
		case r.URL.Path == "/api/v3/klines":
			_, _ = w.Write(klineBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("ATLASRISK_TEST_FRED_KEY", "fixture-secret")
	t.Setenv("ATLASRISK_TEST_TCMB_KEY", "fixture-secret")

	// Route every provider host to the fixture so default provider endpoints,
	// which Binance requires, are exercised over TLS without network access.
	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig.ServerName = "example.com"
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	fixtureClient := &http.Client{Transport: transport}

	objects := &collectorArchive{}
	store := collector.Store{Pool: pool}
	runner := collector.Runner{Pool: pool, Archive: objects, HTTPClient: fixtureClient}
	worker := func(owner string) collector.Worker {
		return collector.Worker{Store: store, Owner: owner, Lease: time.Minute, Handler: runner.Run}
	}

	fredSeries := newCollectorSeries(t, pool, "fred", "collector-fred-"+stamp, "CPIAUCSL")
	fredInput := collector.ScheduleInput{
		Name: "collector-fred-" + stamp, Provider: "fred", SourceID: fredSeries.sourceID, DatasetID: fredSeries.datasetID,
		SeriesID: fredSeries.seriesID, CredentialEnv: "ATLASRISK_TEST_FRED_KEY", BaseURL: server.URL,
		Request: json.RawMessage(`{"output_type":2}`), MaxAttempts: 3,
	}
	fredID, err := store.UpsertSchedule(ctx, collectorSchedule(t, fredInput))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("changed request cannot reuse schedule checkpoint", func(t *testing.T) {
		changed := fredInput
		changed.Request = json.RawMessage(`{"output_type":2,"vintage_dates":"2025-02-01"}`)
		if _, err := store.UpsertSchedule(ctx, collectorSchedule(t, changed)); !errors.Is(err, collector.ErrRequestChanged) {
			t.Fatalf("changed request reused schedule: %v", err)
		}
		if again, err := store.UpsertSchedule(ctx, collectorSchedule(t, fredInput)); err != nil || again != fredID {
			t.Fatalf("unchanged request did not resolve existing schedule: id=%q err=%v", again, err)
		}
	})

	t.Run("competing claims, expired lease and stale owner fencing", func(t *testing.T) {
		claims := make(chan collector.Claim, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, owner := range []string{"owner-a", "owner-b"} {
			wg.Add(1)
			go func(owner string) {
				defer wg.Done()
				claim, err := store.ClaimDue(ctx, owner, time.Minute)
				if err != nil {
					errs <- err
					return
				}
				claims <- claim
			}(owner)
		}
		wg.Wait()
		close(claims)
		close(errs)
		if len(claims) != 1 {
			t.Fatalf("competing workers claimed %d schedules, want 1", len(claims))
		}
		for err := range errs {
			if !errors.Is(err, collector.ErrNoDueSchedule) {
				t.Fatalf("losing claim returned %v", err)
			}
		}
		stale := <-claims
		if stale.ID != fredID {
			t.Fatalf("claimed schedule %q, want %q", stale.Name, fredInput.Name)
		}
		// Simulate a crashed owner whose lease ran out.
		if _, err := pool.Exec(ctx, `UPDATE collector_schedules SET lease_expires_at = clock_timestamp() - interval '1 second' WHERE id = $1::uuid`, fredID); err != nil {
			t.Fatal(err)
		}
		if err := worker("owner-c").RunOnce(ctx); err != nil {
			t.Fatalf("restarted worker did not recover expired schedule: %v", err)
		}
		if err := store.Complete(ctx, stale, "", nil, time.Now().Add(time.Hour)); !errors.Is(err, collector.ErrLeaseLost) {
			t.Fatalf("stale owner completed a newer lease: %v", err)
		}
		if err := store.Fail(ctx, stale, "stale", time.Now()); !errors.Is(err, collector.ErrLeaseLost) {
			t.Fatalf("stale owner failed a newer lease: %v", err)
		}
		active, leaseOwner, lastRunID, lastError, attempts, nextRun := scheduleState(t, pool, fredID)
		if !active || leaseOwner != "" || lastRunID == "" || lastError != "" || attempts != 0 || !nextRun.After(time.Now()) {
			t.Fatalf("completed schedule state active=%v lease=%q run=%q error=%q attempts=%d next=%s", active, leaseOwner, lastRunID, lastError, attempts, nextRun)
		}
		runID, status, _ := latestRunForSource(t, pool, fredSeries.sourceID)
		if runID != lastRunID || status != "succeeded" {
			t.Fatalf("schedule run %q status %q, latest run %q", lastRunID, status, runID)
		}
		if got := countRows(t, pool, `SELECT count(*)::int FROM observation_revisions WHERE series_id = $1::uuid`, fredSeries.seriesID); got != 4 {
			t.Fatalf("persisted %d FRED revisions, want 4", got)
		}
		if got := countRows(t, pool, `SELECT count(*)::int FROM raw_object_occurrences WHERE ingestion_run_id = $1::uuid`, runID); got != 1 {
			t.Fatalf("recorded %d raw occurrences, want 1", got)
		}
		if got := countRows(t, pool, `SELECT count(*)::int FROM observation_revisions r JOIN raw_objects o ON o.id = r.raw_object_id WHERE r.series_id = $1::uuid AND o.content_sha256 = $2`, fredSeries.seriesID, archive.SHA256Hex(fredBody)); got != 4 {
			t.Fatalf("%d FRED revisions reference the archived response, want 4", got)
		}
		if _, ok := objects.objects[archive.ObjectKey(archive.SHA256Hex(fredBody))]; !ok {
			t.Fatal("FRED response was not archived")
		}
	})

	t.Run("identical re-fetch keeps one raw object and first system time", func(t *testing.T) {
		var firstKnown time.Time
		if err := pool.QueryRow(ctx, `SELECT max(system_known_at) FROM observation_revisions WHERE series_id = $1::uuid`, fredSeries.seriesID).Scan(&firstKnown); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE collector_schedules SET next_run_at = clock_timestamp() - interval '1 minute' WHERE id = $1::uuid`, fredID); err != nil {
			t.Fatal(err)
		}
		if err := worker("owner-d").RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		digest := archive.SHA256Hex(fredBody)
		if got := countRows(t, pool, `SELECT count(*)::int FROM raw_objects WHERE content_sha256 = $1`, digest); got != 1 {
			t.Fatalf("identical response produced %d raw objects", got)
		}
		if got := countRows(t, pool, `
SELECT count(DISTINCT occ.ingestion_run_id)::int FROM raw_object_occurrences occ
JOIN raw_objects o ON o.id = occ.raw_object_id
JOIN ingestion_runs r ON r.id = occ.ingestion_run_id
WHERE o.content_sha256 = $1 AND r.source_id = $2::uuid AND r.status = 'succeeded'`, digest, fredSeries.sourceID); got != 2 {
			t.Fatalf("identical re-fetch kept %d succeeded retrieval occurrences, want 2", got)
		}
		var revisions int
		var lastKnown time.Time
		if err := pool.QueryRow(ctx, `SELECT count(*)::int, max(system_known_at) FROM observation_revisions WHERE series_id = $1::uuid`, fredSeries.seriesID).Scan(&revisions, &lastKnown); err != nil {
			t.Fatal(err)
		}
		if revisions != 4 || !lastKnown.Equal(firstKnown) {
			t.Fatalf("re-fetch changed revision history: revisions=%d known %s -> %s", revisions, firstKnown, lastKnown)
		}
		if got := countRows(t, pool, `
SELECT count(*)::int FROM raw_object_occurrences occ JOIN ingestion_runs r ON r.id = occ.ingestion_run_id
WHERE r.source_id = $1::uuid AND occ.request_uri LIKE '%fixture-secret%'`, fredSeries.sourceID); got != 0 {
			t.Fatalf("%d raw occurrences stored the provider credential", got)
		}
	})

	t.Run("persistence failure leaves run failed and schedule inactive", func(t *testing.T) {
		// The upstream series does not match the local series source code.
		input := fredInput
		input.Name = "collector-fred-mismatch-" + stamp
		input.Request = json.RawMessage(`{"series_id":"GDP","output_type":2}`)
		mismatch := newCollectorSeries(t, pool, "fred", "collector-fred-mismatch-"+stamp, "CPIAUCSL")
		input.SourceID, input.DatasetID, input.SeriesID, input.MaxAttempts = mismatch.sourceID, mismatch.datasetID, mismatch.seriesID, 1
		id, err := store.UpsertSchedule(ctx, collectorSchedule(t, input))
		if err != nil {
			t.Fatal(err)
		}
		if err := worker("owner-e").RunOnce(ctx); err == nil {
			t.Fatal("mismatched FRED identity completed")
		}
		if _, status, code := latestRunForSource(t, pool, mismatch.sourceID); status != "failed" || code != "persistence_failed" {
			t.Fatalf("mismatched FRED run status=%q code=%q", status, code)
		}
		if got := countRows(t, pool, `SELECT count(*)::int FROM observation_revisions WHERE series_id = $1::uuid`, mismatch.seriesID); got != 0 {
			t.Fatalf("mismatched FRED identity wrote %d revisions", got)
		}
		active, _, lastRunID, lastError, _, _ := scheduleState(t, pool, id)
		if active || lastRunID != "" || lastError != "handler_failed" {
			t.Fatalf("exhausted schedule active=%v run=%q error=%q", active, lastRunID, lastError)
		}
		if _, err := store.ClaimDue(ctx, "owner-e", time.Minute); !errors.Is(err, collector.ErrNoDueSchedule) {
			t.Fatalf("exhausted schedule is claimable: %v", err)
		}
	})

	t.Run("archive failure cannot succeed", func(t *testing.T) {
		archiveFailure := newCollectorSeries(t, pool, "fred", "collector-fred-archive-"+stamp, "CPIAUCSL")
		input := fredInput
		input.Name = "collector-fred-archive-" + stamp
		input.SourceID, input.DatasetID, input.SeriesID = archiveFailure.sourceID, archiveFailure.datasetID, archiveFailure.seriesID
		if _, err := store.UpsertSchedule(ctx, collectorSchedule(t, input)); err != nil {
			t.Fatal(err)
		}
		failing := collector.Runner{Pool: pool, Archive: &collectorArchive{fail: errors.New("archive unavailable")}, HTTPClient: fixtureClient}
		if err := (collector.Worker{Store: store, Owner: "owner-f", Handler: failing.Run}).RunOnce(ctx); err == nil {
			t.Fatal("archive failure completed")
		}
		if _, status, code := latestRunForSource(t, pool, archiveFailure.sourceID); status != "failed" || code != "archive_failed" {
			t.Fatalf("archive failure run status=%q code=%q", status, code)
		}
		if got := countRows(t, pool, `SELECT count(*)::int FROM observation_revisions WHERE series_id = $1::uuid`, archiveFailure.seriesID); got != 0 {
			t.Fatalf("archive failure wrote %d revisions", got)
		}
	})

	t.Run("truncated pagination is explicit, not complete", func(t *testing.T) {
		paged := newCollectorSeries(t, pool, "fred", "collector-fred-paged-"+stamp, "PAGED")
		input := fredInput
		input.Name = "collector-fred-paged-" + stamp
		input.Request = json.RawMessage(`{"output_type":2,"limit":4,"max_pages":1}`)
		input.SourceID, input.DatasetID, input.SeriesID = paged.sourceID, paged.datasetID, paged.seriesID
		id, err := store.UpsertSchedule(ctx, collectorSchedule(t, input))
		if err != nil {
			t.Fatal(err)
		}
		if err := worker("owner-g").RunOnce(ctx); err == nil {
			t.Fatal("truncated pagination completed")
		}
		if _, status, code := latestRunForSource(t, pool, paged.sourceID); status != "failed" || code != "incomplete_coverage" {
			t.Fatalf("truncated run status=%q code=%q", status, code)
		}
		if _, _, lastRunID, _, _, _ := scheduleState(t, pool, id); lastRunID != "" {
			t.Fatalf("truncated schedule recorded completed run %q", lastRunID)
		}
	})

	t.Run("TCMB observations persist through the pipeline", func(t *testing.T) {
		tcmbSeries := newCollectorSeries(t, pool, "tcmb", "collector-tcmb-"+stamp, "TP.DK.USD.A")
		id, err := store.UpsertSchedule(ctx, collectorSchedule(t, collector.ScheduleInput{
			Name: "collector-tcmb-" + stamp, Provider: "tcmb", SourceID: tcmbSeries.sourceID, DatasetID: tcmbSeries.datasetID,
			SeriesID: tcmbSeries.seriesID, CredentialEnv: "ATLASRISK_TEST_TCMB_KEY", BaseURL: server.URL,
			Request: json.RawMessage(`{"series":["TP.DK.USD.A"],"startdate":"02-01-2024","enddate":"08-01-2024","frequency":"1","aggregationtypes":"avg","decimalseparator":","}`),
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err := worker("owner-h").RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if _, _, lastRunID, _, _, _ := scheduleState(t, pool, id); lastRunID == "" {
			t.Fatal("TCMB schedule did not record its run")
		}
		if _, status, _ := latestRunForSource(t, pool, tcmbSeries.sourceID); status != "succeeded" {
			t.Fatalf("TCMB run status %q", status)
		}
		if got := countRows(t, pool, `SELECT count(*)::int FROM observation_revisions WHERE series_id = $1::uuid`, tcmbSeries.seriesID); got == 0 {
			t.Fatal("TCMB run persisted no observations")
		}
	})

	t.Run("TCMB FX schedule writes fx quote revisions", func(t *testing.T) {
		// Regression: no production path wrote fx_quote_revisions, so valuations
		// could never convert between fiat currencies.
		fxSeries := newCollectorSeries(t, pool, "tcmb", "collector-tcmb-fx-"+stamp, "TP.DK.USD.A")
		id, err := store.UpsertSchedule(ctx, collectorSchedule(t, collector.ScheduleInput{
			Name: "collector-tcmb-fx-" + stamp, Provider: "tcmb", SourceID: fxSeries.sourceID, DatasetID: fxSeries.datasetID,
			SeriesID: fxSeries.seriesID, CredentialEnv: "ATLASRISK_TEST_TCMB_KEY", BaseURL: server.URL,
			Request: json.RawMessage(`{"series":["TP.DK.USD.A"],"startdate":"02-01-2024","enddate":"08-01-2024","frequency":"1","aggregationtypes":"avg","decimalseparator":","}`),
			FXPair:  &collector.FXPair{Base: "USD", Quote: "TRY"},
		}))
		if err != nil {
			t.Fatal(err)
		}
		if err := worker("owner-fx").RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		runID, status, _ := latestRunForSource(t, pool, fxSeries.sourceID)
		if status != "succeeded" {
			t.Fatalf("TCMB FX run status %q", status)
		}
		if _, _, lastRunID, _, _, _ := scheduleState(t, pool, id); lastRunID != runID {
			t.Fatalf("schedule last run %q, want %q", lastRunID, runID)
		}
		// The fixture has four numeric USD/TRY days and one missing day.
		fxRows := countRows(t, pool, `SELECT count(*)::int FROM fx_quote_revisions WHERE base_currency = 'USD' AND quote_currency = 'TRY'
AND raw_object_id IN (SELECT raw_object_id FROM raw_object_occurrences WHERE ingestion_run_id = $1::uuid)`, runID)
		if fxRows != 4 {
			t.Fatalf("fx quote rows = %d, want 4", fxRows)
		}
		if got := countRows(t, pool, `SELECT count(*)::int FROM fx_quote_revisions WHERE base_currency = 'USD' AND quote_currency = 'TRY' AND observation_time = '2024-01-08T00:00:00Z' AND rate = 29.8214`); got == 0 {
			t.Fatal("08-01-2024 USD/TRY rate was not persisted exactly")
		}
		if got := countRows(t, pool, `SELECT count(*)::int FROM observation_revisions WHERE series_id = $1::uuid`, fxSeries.seriesID); got != 5 {
			t.Fatalf("FX series observations = %d, want 5 including the missing day", got)
		}
	})

	t.Run("Binance closed daily prices persist with checkpoint", func(t *testing.T) {
		binanceSeries := newCollectorSeries(t, pool, "binance", "collector-binance-"+stamp, "BTCUSDT")
		if _, err := pool.Exec(ctx, `INSERT INTO instruments (canonical_symbol, instrument_type, native_currency, external_ids) VALUES ('BTCUSDT', 'crypto_spot', 'USDT', '{"provider":"binance"}') ON CONFLICT (canonical_symbol) DO NOTHING`); err != nil {
			t.Fatal(err)
		}
		var instrumentID string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM instruments WHERE canonical_symbol = 'BTCUSDT'`).Scan(&instrumentID); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpsertSchedule(ctx, collectorSchedule(t, collector.ScheduleInput{
			Name: "collector-binance-" + stamp, Provider: "binance", SourceID: binanceSeries.sourceID, DatasetID: binanceSeries.datasetID,
			InstrumentID: instrumentID, Request: json.RawMessage(`{"symbol":"BTCUSDT","limit":1000}`),
		})); err != nil {
			t.Fatal(err)
		}
		if err := worker("owner-i").RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		runID, status, _ := latestRunForSource(t, pool, binanceSeries.sourceID)
		if status != "succeeded" {
			t.Fatalf("Binance run status %q", status)
		}
		if got := countRows(t, pool, `
SELECT count(*)::int FROM price_revisions p JOIN raw_objects o ON o.id = p.raw_object_id
WHERE p.instrument_id = $1::uuid AND o.content_sha256 = $2`, instrumentID, archive.SHA256Hex(klineBody)); got == 0 {
			t.Fatal("Binance run persisted no closed daily prices")
		}
		if got := countRows(t, pool, `SELECT count(*)::int FROM ingestion_runs WHERE id = $1::uuid AND coverage ? 'request_fingerprint'`, runID); got != 1 {
			t.Fatal("Binance run did not persist its checkpoint")
		}
	})

	t.Run("collector executable ingests a TLS fixture once", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the executable trusts the fixture CA through SSL_CERT_FILE, which Go ignores on Windows")
		}
		endpoint := os.Getenv("ATLASRISK_S3_ENDPOINT")
		if endpoint == "" || os.Getenv("AWS_ACCESS_KEY_ID") == "" || os.Getenv("AWS_SECRET_ACCESS_KEY") == "" {
			t.Skip("ATLASRISK_S3_ENDPOINT and AWS credentials are required for the executable archive")
		}
		dir := t.TempDir()
		binary := filepath.Join(dir, "collector")
		build := exec.Command("go", "build", "-o", binary, "./apps/collector/cmd/collector")
		build.Dir = filepath.Join("..", "..")
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build collector: %v\n%s", err, output)
		}
		caFile := filepath.Join(dir, "fixture-ca.pem")
		if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600); err != nil {
			t.Fatal(err)
		}
		executableSeries := newCollectorSeries(t, pool, "fred", "collector-exec-"+stamp, "CPIAUCSL")
		// The oldest due time makes this the first schedule the executable claims.
		due := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		config := map[string]any{
			"database_url_env": "ATLASRISK_TEST_DATABASE_URL",
			"archive": map[string]any{
				"endpoint": endpoint, "region": os.Getenv("ATLASRISK_S3_REGION"), "bucket": os.Getenv("ATLASRISK_S3_BUCKET"),
				"access_key_env": "AWS_ACCESS_KEY_ID", "secret_key_env": "AWS_SECRET_ACCESS_KEY",
			},
			"schedules": []map[string]any{{
				"name": "collector-exec-" + stamp, "provider": "fred", "source_id": executableSeries.sourceID,
				"dataset_id": executableSeries.datasetID, "series_id": executableSeries.seriesID,
				"credential_env": "ATLASRISK_TEST_FRED_KEY", "base_url": server.URL, "request": map[string]any{"output_type": 2},
				"interval_seconds": collectorTestInterval, "max_attempts": 1, "lease_seconds": 120, "next_run_at": due,
			}},
		}
		data, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		configFile := filepath.Join(dir, "collector.json")
		if err := os.WriteFile(configFile, data, 0o600); err != nil {
			t.Fatal(err)
		}
		run := exec.Command(binary, "-config", configFile, "-once")
		run.Env = append(os.Environ(), "SSL_CERT_FILE="+caFile)
		output, err := run.CombinedOutput()
		if err != nil {
			t.Fatalf("collector -once failed: %v\n%s", err, output)
		}
		if strings.Contains(string(output), "fixture-secret") {
			t.Fatalf("collector output exposed the provider credential: %s", output)
		}
		if _, status, _ := latestRunForSource(t, pool, executableSeries.sourceID); status != "succeeded" {
			t.Fatalf("executable run status %q", status)
		}
		if got := countRows(t, pool, `SELECT count(*)::int FROM observation_revisions WHERE series_id = $1::uuid`, executableSeries.seriesID); got != 4 {
			t.Fatalf("executable persisted %d FRED revisions, want 4", got)
		}
	})
}
