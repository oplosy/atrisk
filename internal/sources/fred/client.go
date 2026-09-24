// Package fred provides bounded FRED/ALFRED metadata and vintage observation ingestion.
package fred

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/oplosy/atrisk/internal/ingestion"
)

const AdapterVersion = "fred-alfred-v1"

var (
	ErrAPIKeyRequired            = errors.New("FRED API key is required for network requests")
	ErrPageLimit                 = errors.New("FRED pagination limit reached")
	ErrNoCheckpoint              = errors.New("FRED checkpoint not found")
	ErrCheckpointRequestMismatch = errors.New("FRED checkpoint request fingerprint mismatch")
)

type Config struct {
	BaseURL           string
	APIKey            string
	HTTPClient        *http.Client
	AllowedHosts      map[string]struct{}
	AllowedMediaTypes map[string]struct{}
	PageSize          int
	MaxPages          int
	MaxBodyBytes      int64
	Timeout           time.Duration
	Retry             ingestion.RetryPolicy
	RateLimit         ingestion.RateLimitHook
}

type Client struct {
	baseURL  string
	apiKey   string
	fetcher  ingestion.HTTPFetcher
	pageSize int
	maxPages int
}

func NewClient(config Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.stlouisfed.org"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" {
		return nil, errors.New("FRED base URL must be HTTPS without credentials or query parameters")
	}
	allowed := config.AllowedHosts
	if len(allowed) == 0 {
		allowed = map[string]struct{}{strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")): {}}
	}
	pageSize := config.PageSize
	if pageSize <= 0 {
		pageSize = 1000
	}
	if pageSize > 10000 {
		return nil, errors.New("FRED page size must not exceed 10000")
	}
	maxPages := config.MaxPages
	if maxPages <= 0 {
		maxPages = 100
	}
	mediaTypes := config.AllowedMediaTypes
	if len(mediaTypes) == 0 {
		mediaTypes = map[string]struct{}{"application/json": {}}
	}
	return &Client{
		baseURL: baseURL, apiKey: strings.TrimSpace(config.APIKey), pageSize: pageSize, maxPages: maxPages,
		fetcher: ingestion.HTTPFetcher{
			Client: config.HTTPClient, AllowedHosts: allowed, AllowedMediaTypes: mediaTypes,
			MaxBodyBytes: config.MaxBodyBytes, Timeout: config.Timeout, Retry: config.Retry, RateLimit: config.RateLimit,
		},
	}, nil
}

func (c *Client) request(ctx context.Context, endpoint string, values url.Values) (ingestion.FetchedResponse, error) {
	request, err := c.buildRequest(endpoint, values)
	if err != nil {
		return ingestion.FetchedResponse{}, err
	}
	return c.fetcher.Fetch(ctx, request)
}

func (c *Client) buildRequest(endpoint string, values url.Values) (ingestion.FetchRequest, error) {
	if c == nil {
		return ingestion.FetchRequest{}, errors.New("FRED client is required")
	}
	if c.apiKey == "" {
		return ingestion.FetchRequest{}, ErrAPIKeyRequired
	}
	values = cloneValues(values)
	values.Set("file_type", "json")
	values.Set("api_key", c.apiKey)
	requestURL := c.baseURL + "/" + strings.TrimLeft(endpoint, "/")
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return ingestion.FetchRequest{}, errors.New("invalid FRED request path")
	}
	parsed.RawQuery = values.Encode()
	return ingestion.FetchRequest{URL: parsed.String()}, nil
}

func cloneValues(values url.Values) url.Values {
	copyValues := make(url.Values, len(values))
	for key, items := range values {
		copyValues[key] = append([]string(nil), items...)
	}
	return copyValues
}

type SeriesMetadataResponse struct {
	Series []SeriesMetadata `json:"seriess"`
}

type SeriesMetadata struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Units              string `json:"units"`
	Frequency          string `json:"frequency"`
	SeasonalAdjustment string `json:"seasonal_adjustment"`
	ObservationStart   string `json:"observation_start"`
	ObservationEnd     string `json:"observation_end"`
	RealtimeStart      string `json:"realtime_start"`
	RealtimeEnd        string `json:"realtime_end"`
	LastUpdated        string `json:"last_updated"`
	Notes              string `json:"notes"`
}

func (c *Client) FetchSeriesMetadata(ctx context.Context, seriesID string) (SeriesMetadata, ingestion.FetchedResponse, error) {
	if err := validateSeriesID(seriesID); err != nil {
		return SeriesMetadata{}, ingestion.FetchedResponse{}, err
	}
	response, err := c.request(ctx, "fred/series", url.Values{"series_id": {seriesID}})
	if err != nil {
		return SeriesMetadata{}, ingestion.FetchedResponse{}, err
	}
	var envelope SeriesMetadataResponse
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return SeriesMetadata{}, ingestion.FetchedResponse{}, fmt.Errorf("decode FRED series metadata: %w", err)
	}
	if len(envelope.Series) != 1 || envelope.Series[0].ID != seriesID {
		return SeriesMetadata{}, ingestion.FetchedResponse{}, fmt.Errorf("FRED series metadata missing %q", seriesID)
	}
	return envelope.Series[0], response, nil
}

func validateSeriesID(seriesID string) error {
	if strings.TrimSpace(seriesID) == "" || strings.ContainsAny(seriesID, "\r\n") {
		return errors.New("FRED series ID is required")
	}
	return nil
}

type ObservationRequest struct {
	SeriesID         string
	RealtimeStart    string
	RealtimeEnd      string
	VintageDates     string
	ObservationStart string
	ObservationEnd   string
	Units            string
	Frequency        string
	Aggregation      string
	OutputType       int
	Limit            int
	Offset           int
}

func (r ObservationRequest) normalized(defaultPageSize int) (ObservationRequest, error) {
	if err := validateSeriesID(r.SeriesID); err != nil {
		return ObservationRequest{}, err
	}
	if r.OutputType == 0 {
		r.OutputType = 2
	}
	if r.OutputType < 1 || r.OutputType > 3 {
		return ObservationRequest{}, errors.New("FRED output type must be 1, 2, or 3")
	}
	if r.Limit <= 0 {
		r.Limit = defaultPageSize
	}
	if r.Limit > 10000 {
		return ObservationRequest{}, errors.New("FRED observation limit must not exceed 10000")
	}
	if r.Offset < 0 {
		return ObservationRequest{}, errors.New("FRED observation offset cannot be negative")
	}
	for name, value := range map[string]string{
		"realtime_start": r.RealtimeStart, "realtime_end": r.RealtimeEnd,
		"vintage_dates": r.VintageDates, "observation_start": r.ObservationStart, "observation_end": r.ObservationEnd,
	} {
		if strings.ContainsAny(value, "\r\n") {
			return ObservationRequest{}, fmt.Errorf("FRED %s contains control characters", name)
		}
	}
	return r, nil
}

type ObservationResponse struct {
	RealtimeStart    string        `json:"realtime_start"`
	RealtimeEnd      string        `json:"realtime_end"`
	ObservationStart string        `json:"observation_start"`
	ObservationEnd   string        `json:"observation_end"`
	Units            string        `json:"units"`
	Frequency        string        `json:"frequency"`
	OutputType       int           `json:"output_type"`
	Count            int           `json:"count"`
	Limit            int           `json:"limit"`
	Offset           int           `json:"offset"`
	Observations     []Observation `json:"observations"`
}

type Observation struct {
	RealtimeStart string `json:"realtime_start"`
	RealtimeEnd   string `json:"realtime_end"`
	Date          string `json:"date"`
	Value         string `json:"value"`
}

func (c *Client) FetchObservationPage(ctx context.Context, request ObservationRequest) (ObservationResponse, ingestion.FetchedResponse, error) {
	if c == nil {
		return ObservationResponse{}, ingestion.FetchedResponse{}, errors.New("FRED client is required")
	}
	normalized, err := request.normalized(c.pageSize)
	if err != nil {
		return ObservationResponse{}, ingestion.FetchedResponse{}, err
	}
	values := url.Values{
		"series_id": {normalized.SeriesID}, "limit": {strconv.Itoa(normalized.Limit)}, "offset": {strconv.Itoa(normalized.Offset)},
		"output_type": {strconv.Itoa(normalized.OutputType)},
	}
	for key, value := range map[string]string{
		"realtime_start": normalized.RealtimeStart, "realtime_end": normalized.RealtimeEnd,
		"vintage_dates": normalized.VintageDates, "observation_start": normalized.ObservationStart,
		"observation_end": normalized.ObservationEnd, "units": normalized.Units,
		"frequency": normalized.Frequency, "aggregation_method": normalized.Aggregation,
	} {
		if value != "" {
			values.Set(key, value)
		}
	}
	response, err := c.request(ctx, "fred/series/observations", values)
	if err != nil {
		return ObservationResponse{}, ingestion.FetchedResponse{}, err
	}
	var envelope ObservationResponse
	if err := json.Unmarshal(response.Body, &envelope); err != nil {
		return ObservationResponse{}, ingestion.FetchedResponse{}, fmt.Errorf("decode FRED observations: %w", err)
	}
	if envelope.Limit == 0 {
		envelope.Limit = normalized.Limit
	}
	if envelope.Offset == 0 && normalized.Offset != 0 {
		envelope.Offset = normalized.Offset
	}
	return envelope, response, nil
}

// FetchObservationPages retrieves a bounded observation backfill. FRED's
// offset is part of the request, so a caller can persist the returned
// checkpoint after each page and resume without changing source semantics.
func (c *Client) FetchObservationPages(ctx context.Context, request ObservationRequest) ([]ObservationPage, error) {
	if c == nil {
		return nil, errors.New("FRED client is required")
	}
	normalized, err := request.normalized(c.pageSize)
	if err != nil {
		return nil, err
	}
	pages := make([]ObservationPage, 0, minInt(c.maxPages, 8))
	for page := 0; page < c.maxPages; page++ {
		response, fetched, err := c.FetchObservationPage(ctx, normalized)
		if err != nil {
			return nil, err
		}
		pages = append(pages, ObservationPage{Response: response, Fetched: fetched})
		if len(response.Observations) == 0 || len(response.Observations) < normalized.Limit || response.Offset+len(response.Observations) >= response.Count {
			return pages, nil
		}
		nextOffset := response.Offset + len(response.Observations)
		if nextOffset <= normalized.Offset {
			return nil, errors.New("FRED pagination did not advance")
		}
		normalized.Offset = nextOffset
	}
	return nil, ErrPageLimit
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

type ObservationPage struct {
	Response ObservationResponse
	Fetched  ingestion.FetchedResponse
}

// ObservationCheckpoint is safe to persist as ingestion_runs.coverage. The
// fingerprint binds the offset to every request filter and effective page
// limit, so a checkpoint cannot be resumed against a different data window.
type ObservationCheckpoint struct {
	SeriesID           string `json:"series_id"`
	RequestFingerprint string `json:"request_fingerprint"`
	NextOffset         int    `json:"next_offset"`
	Pages              int    `json:"pages"`
	Observations       int    `json:"observations"`
	Completed          bool   `json:"completed"`
	RealtimeStart      string `json:"realtime_start,omitempty"`
	RealtimeEnd        string `json:"realtime_end,omitempty"`
}

// NewObservationCheckpoint creates a checkpoint identity from the normalized
// request. Offset is deliberately excluded from the identity because it is the
// cursor that changes as pages are consumed.
func NewObservationCheckpoint(request ObservationRequest, defaultPageSize int) (ObservationCheckpoint, error) {
	normalized, err := request.normalized(defaultPageSize)
	if err != nil {
		return ObservationCheckpoint{}, err
	}
	fingerprint, err := RequestFingerprint(normalized, defaultPageSize)
	if err != nil {
		return ObservationCheckpoint{}, err
	}
	return ObservationCheckpoint{SeriesID: normalized.SeriesID, RequestFingerprint: fingerprint}, nil
}

// RequestFingerprint returns a stable SHA-256 identity for every FRED query
// parameter except offset and the API key. Credentials are never included in
// persisted checkpoint state.
func RequestFingerprint(request ObservationRequest, defaultPageSize int) (string, error) {
	normalized, err := request.normalized(defaultPageSize)
	if err != nil {
		return "", err
	}
	identity := struct {
		SeriesID         string `json:"series_id"`
		RealtimeStart    string `json:"realtime_start"`
		RealtimeEnd      string `json:"realtime_end"`
		VintageDates     string `json:"vintage_dates"`
		ObservationStart string `json:"observation_start"`
		ObservationEnd   string `json:"observation_end"`
		Units            string `json:"units"`
		Frequency        string `json:"frequency"`
		Aggregation      string `json:"aggregation_method"`
		OutputType       int    `json:"output_type"`
		Limit            int    `json:"limit"`
	}{
		SeriesID: normalized.SeriesID, RealtimeStart: normalized.RealtimeStart, RealtimeEnd: normalized.RealtimeEnd,
		VintageDates: normalized.VintageDates, ObservationStart: normalized.ObservationStart, ObservationEnd: normalized.ObservationEnd,
		Units: normalized.Units, Frequency: normalized.Frequency, Aggregation: normalized.Aggregation,
		OutputType: normalized.OutputType, Limit: normalized.Limit,
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("encode FRED request identity: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (c ObservationCheckpoint) NextRequest(request ObservationRequest, defaultPageSize ...int) (ObservationRequest, error) {
	pageSize := 1000
	if len(defaultPageSize) > 0 && defaultPageSize[0] > 0 {
		pageSize = defaultPageSize[0]
	}
	fingerprint, err := RequestFingerprint(request, pageSize)
	if err != nil {
		return ObservationRequest{}, err
	}
	if c.RequestFingerprint == "" || fingerprint != c.RequestFingerprint {
		return ObservationRequest{}, ErrCheckpointRequestMismatch
	}
	normalized, err := request.normalized(pageSize)
	if err != nil {
		return ObservationRequest{}, err
	}
	normalized.SeriesID = c.SeriesID
	normalized.Offset = c.NextOffset
	return normalized, nil
}

func (c ObservationCheckpoint) Advance(page ObservationPage) ObservationCheckpoint {
	c.Pages++
	c.Observations += len(page.Response.Observations)
	c.NextOffset = page.Response.Offset + len(page.Response.Observations)
	c.Completed = len(page.Response.Observations) == 0 ||
		(page.Response.Count > 0 && c.NextOffset >= page.Response.Count)
	if page.Response.RealtimeStart != "" {
		c.RealtimeStart = page.Response.RealtimeStart
	}
	if page.Response.RealtimeEnd != "" {
		c.RealtimeEnd = page.Response.RealtimeEnd
	}
	return c
}
