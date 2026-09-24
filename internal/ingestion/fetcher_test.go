package ingestion

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testFetcherServer(t *testing.T, handler http.Handler) (*httptest.Server, *http.Client) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	return server, server.Client()
}

func TestHTTPFetcherAllowsOnlyBoundedHTTPSAndSafeMetadata(t *testing.T) {
	server, client := testFetcherServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-ID") == "" {
			t.Error("request correlation header missing")
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Authorization", "Bearer should-not-be-recorded")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	url := server.URL
	fetcher := HTTPFetcher{Client: client, AllowedHosts: map[string]struct{}{"127.0.0.1": {}}, MaxBodyBytes: 100, Timeout: time.Second}
	response, err := fetcher.Fetch(WithCorrelationID(context.Background(), "corr-test"), FetchRequest{URL: url, Headers: http.Header{"Authorization": []string{"secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	if string(response.Body) != `{"ok":true}` || response.MediaType != "application/json" || response.CorrelationID != "corr-test" {
		t.Fatalf("unexpected response: %+v", response)
	}
	if _, ok := response.Headers["authorization"]; ok || strings.Contains(response.RequestURI, "secret") {
		t.Fatalf("secret leaked into response metadata: %+v %q", response.Headers, response.RequestURI)
	}
	if _, err := (HTTPFetcher{Client: client, AllowedHosts: map[string]struct{}{"127.0.0.1": {}}, MaxBodyBytes: 2}).Fetch(context.Background(), FetchRequest{URL: url}); err == nil {
		t.Fatal("oversized response unexpectedly succeeded")
	}
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: "http://127.0.0.1/data"}); err == nil {
		t.Fatal("non-HTTPS source unexpectedly succeeded")
	}
}

func TestHTTPFetcherRetriesRateLimitAndRejectsMediaType(t *testing.T) {
	var calls atomic.Int32
	server, client := testFetcherServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		fmt.Fprint(w, "bytes")
	}))
	defer server.Close()
	fetcher := HTTPFetcher{Client: client, AllowedHosts: map[string]struct{}{"127.0.0.1": {}}, MaxBodyBytes: 10, Retry: RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}, Sleep: func(context.Context, time.Duration) error { return nil }}
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one retry, got %d calls", calls.Load())
	}
	fetcher.AllowedMediaTypes = map[string]struct{}{"application/json": {}}
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL}); err == nil {
		t.Fatal("disallowed media type unexpectedly succeeded")
	}
}

func TestHTTPFetcherRejectsUnallowlistedHost(t *testing.T) {
	server, client := testFetcherServer(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	if _, err := (HTTPFetcher{Client: client, AllowedHosts: map[string]struct{}{"example.test": {}}}).Fetch(context.Background(), FetchRequest{URL: server.URL}); err == nil {
		t.Fatal("unallowlisted host unexpectedly succeeded")
	}
}

func TestHTTPFetcherHonorsTimeout(t *testing.T) {
	server, client := testFetcherServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(100 * time.Millisecond):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"late":true}`))
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	fetcher := HTTPFetcher{Client: client, AllowedHosts: map[string]struct{}{"127.0.0.1": {}}, Timeout: 10 * time.Millisecond, Retry: RetryPolicy{MaxAttempts: 1}}
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: server.URL}); err == nil {
		t.Fatal("timed-out response unexpectedly succeeded")
	}
}

func TestHTTPFetcherValidatesEveryRedirectHop(t *testing.T) {
	downgradeTarget := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer downgradeTarget.Close()
	downgradeSource, downgradeClient := testFetcherServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, downgradeTarget.URL, http.StatusFound)
	}))
	defer downgradeSource.Close()
	fetcher := HTTPFetcher{Client: downgradeClient, AllowedHosts: map[string]struct{}{"127.0.0.1": {}}, Retry: RetryPolicy{MaxAttempts: 1}}
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: downgradeSource.URL}); err == nil {
		t.Fatal("HTTPS downgrade redirect unexpectedly succeeded")
	}

	allowlistedSource, allowlistedClient := testFetcherServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://unallowlisted.example.test/data", http.StatusFound)
	}))
	defer allowlistedSource.Close()
	fetcher.Client = allowlistedClient
	if _, err := fetcher.Fetch(context.Background(), FetchRequest{URL: allowlistedSource.URL}); err == nil {
		t.Fatal("unallowlisted redirect unexpectedly succeeded")
	}
}

type leakingTransport struct{}

func (leakingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("upstream api_key=source-secret failed for %s", request.URL.String())
}

func TestHTTPFetcherSanitizesTransportErrors(t *testing.T) {
	fetcher := HTTPFetcher{
		Client:       &http.Client{Transport: leakingTransport{}},
		AllowedHosts: map[string]struct{}{"example.test": {}},
		Retry:        RetryPolicy{MaxAttempts: 1},
	}
	_, err := fetcher.Fetch(context.Background(), FetchRequest{URL: "https://example.test/data?api_key=source-secret"})
	if err == nil || strings.Contains(err.Error(), "source-secret") || strings.Contains(err.Error(), "api_key") {
		t.Fatalf("transport error leaked request credentials: %v", err)
	}
}
