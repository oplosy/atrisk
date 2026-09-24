// Package ingestion contains bounded, secret-safe source fetching and replay.
package ingestion

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/oplosy/atrisk/internal/archive"
)

type correlationKey struct{}

func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationKey{}, strings.TrimSpace(id))
}

func CorrelationID(ctx context.Context) string {
	if value, ok := ctx.Value(correlationKey{}).(string); ok && value != "" {
		return value
	}
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	return "correlation-unavailable"
}

type FetchRequest struct {
	URL     string
	Headers http.Header
}

type FetchedResponse struct {
	StatusCode    int
	MediaType     string
	Body          []byte
	RetrievedAt   time.Time
	CorrelationID string
	RequestURI    string
	Headers       map[string]string
}

type RateLimitHook interface {
	Wait(context.Context) error
	Observe(context.Context, int, http.Header) error
}

type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func (p RetryPolicy) normalized() RetryPolicy {
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 3
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = 100 * time.Millisecond
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 2 * time.Second
	}
	return p
}

type HTTPFetcher struct {
	Client            *http.Client
	AllowedHosts      map[string]struct{}
	AllowedMediaTypes map[string]struct{}
	MaxBodyBytes      int64
	Timeout           time.Duration
	Retry             RetryPolicy
	RateLimit         RateLimitHook
	Sleep             func(context.Context, time.Duration) error
}

func (f HTTPFetcher) Fetch(ctx context.Context, request FetchRequest) (FetchedResponse, error) {
	u, err := validateRequestURL(request.URL, f.AllowedHosts)
	if err != nil {
		return FetchedResponse{}, err
	}
	maxBytes := f.MaxBodyBytes
	if maxBytes <= 0 {
		maxBytes = 10 << 20
	}
	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	clientCopy := *client
	existingRedirectPolicy := clientCopy.CheckRedirect
	clientCopy.CheckRedirect = func(redirectRequest *http.Request, via []*http.Request) error {
		// Credentials are scoped to the original request. Never forward a
		// provider key to a redirect target, even when the host is allowlisted.
		redirectRequest.Header.Del("key")
		if _, redirectErr := validateRequestURL(redirectRequest.URL.String(), f.AllowedHosts); redirectErr != nil {
			return fmt.Errorf("source redirect rejected: %w", redirectErr)
		}
		if existingRedirectPolicy != nil {
			return existingRedirectPolicy(redirectRequest, via)
		}
		return nil
	}
	client = &clientCopy
	policy := f.Retry.normalized()
	sleep := f.Sleep
	if sleep == nil {
		sleep = func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		}
	}

	id := CorrelationID(ctx)
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if f.RateLimit != nil {
			if err := f.RateLimit.Wait(ctx); err != nil {
				return FetchedResponse{}, fmt.Errorf("wait for source rate limit: %w", err)
			}
		}
		requestCtx, cancel := context.WithTimeout(ctx, timeout)
		req, reqErr := http.NewRequestWithContext(requestCtx, http.MethodGet, u.String(), nil)
		if reqErr != nil {
			cancel()
			return FetchedResponse{}, fmt.Errorf("build source request: %w", reqErr)
		}
		copySafeHeaders(req.Header, request.Headers)
		req.Header.Set("X-Request-ID", id)
		response, doErr := client.Do(req)
		if doErr != nil {
			cancel()
			if attempt == policy.MaxAttempts {
				return FetchedResponse{}, fmt.Errorf("fetch source: %w", sanitizedTransportError(doErr))
			}
			if err := sleep(ctx, retryDelay(policy, attempt)); err != nil {
				return FetchedResponse{}, err
			}
			continue
		}
		body, readErr := readBounded(response.Body, maxBytes)
		response.Body.Close()
		cancel()
		if f.RateLimit != nil {
			if err := f.RateLimit.Observe(ctx, response.StatusCode, response.Header); err != nil {
				return FetchedResponse{}, fmt.Errorf("observe source rate limit: %w", err)
			}
		}
		if readErr != nil {
			return FetchedResponse{}, readErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			if retryableStatus(response.StatusCode) && attempt < policy.MaxAttempts {
				if err := sleep(ctx, retryAfter(response.Header, policy, attempt)); err != nil {
					return FetchedResponse{}, err
				}
				continue
			}
			return FetchedResponse{}, fmt.Errorf("source returned HTTP %d", response.StatusCode)
		}
		mediaType := normalizeMediaType(response.Header.Get("Content-Type"))
		if len(f.AllowedMediaTypes) > 0 {
			if _, ok := f.AllowedMediaTypes[mediaType]; !ok {
				return FetchedResponse{}, fmt.Errorf("unsupported source media type %q", mediaType)
			}
		}
		return FetchedResponse{
			StatusCode: response.StatusCode, MediaType: mediaType, Body: body,
			RetrievedAt: time.Now().UTC(), CorrelationID: id,
			RequestURI: archive.RedactedURL(u.String()), Headers: safeHeaders(response.Header),
		}, nil
	}
	return FetchedResponse{}, errors.New("source fetch exhausted retries")
}

func sanitizedTransportError(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return errors.New("source request failed")
}

func validateRequestURL(raw string, allowed map[string]struct{}) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("source URL must be HTTPS without user information")
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if len(allowed) == 0 {
		return nil, errors.New("source host allowlist is required")
	}
	if _, ok := allowed[host]; !ok {
		return nil, fmt.Errorf("source host %q is not allowlisted", host)
	}
	return u, nil
}

func readBounded(body io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read source response: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("source response exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func retryableStatus(status int) bool { return status == http.StatusTooManyRequests || status >= 500 }

func retryDelay(policy RetryPolicy, attempt int) time.Duration {
	delay := float64(policy.BaseDelay) * math.Pow(2, float64(attempt-1))
	if delay > float64(policy.MaxDelay) {
		delay = float64(policy.MaxDelay)
	}
	return time.Duration(delay)
}

func retryAfter(headers http.Header, policy RetryPolicy, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(headers.Get("Retry-After"))); err == nil && seconds >= 0 {
		delay := time.Duration(seconds) * time.Second
		if delay > policy.MaxDelay {
			return policy.MaxDelay
		}
		return delay
	}
	return retryDelay(policy, attempt)
}

func normalizeMediaType(value string) string {
	if index := strings.IndexByte(value, ';'); index >= 0 {
		value = value[:index]
	}
	return strings.ToLower(strings.TrimSpace(value))
}

var outboundRequestHeaders = map[string]struct{}{"accept": {}, "content-type": {}, "user-agent": {}, "key": {}}
var responseMetadataHeaders = map[string]struct{}{"accept": {}, "content-type": {}, "user-agent": {}, "etag": {}, "last-modified": {}, "content-length": {}, "retry-after": {}}

func copySafeHeaders(destination, source http.Header) {
	for key, values := range source {
		if _, ok := outboundRequestHeaders[strings.ToLower(key)]; !ok {
			continue
		}
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

func safeHeaders(headers http.Header) map[string]string {
	result := make(map[string]string)
	for key, values := range headers {
		if _, ok := responseMetadataHeaders[strings.ToLower(key)]; !ok || len(values) == 0 {
			continue
		}
		result[strings.ToLower(key)] = values[0]
	}
	return archive.SanitizedMetadata(result)
}
