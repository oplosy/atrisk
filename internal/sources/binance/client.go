// Package binance provides bounded Binance Spot public market-data ingestion.
package binance

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

const (
	AdapterVersion = "binance-spot-v1"
	DataAPIBaseURL = "https://data-api.binance.vision"
	maxKlineLimit  = 1000
)

var (
	ErrPageLimit              = errors.New("Binance kline pagination limit reached")
	ErrCheckpointMismatch     = errors.New("Binance kline checkpoint request fingerprint mismatch")
	ErrNoCheckpoint           = errors.New("Binance kline checkpoint not found")
	ErrUnsupportedEndpoint    = errors.New("Binance endpoint is outside the public market-data contract")
	ErrUnsupportedKlineFilter = errors.New("Binance kline request must use the daily interval")
)

type Config struct {
	BaseURL           string
	HTTPClient        *http.Client
	AllowedHosts      map[string]struct{}
	AllowedMediaTypes map[string]struct{}
	PageSize          int
	MaxPages          int
	MaxBodyBytes      int64
	Timeout           time.Duration
	Retry             ingestion.RetryPolicy
	RateLimit         ingestion.RateLimitHook
	Now               func() time.Time
}

type Client struct {
	baseURL  string
	fetcher  ingestion.HTTPFetcher
	pageSize int
	maxPages int
	now      func() time.Time
}

func NewClient(config Config) (*Client, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	if baseURL == "" {
		baseURL = DataAPIBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Path != "" {
		return nil, errors.New("Binance base URL must be HTTPS without credentials, path, or query parameters")
	}
	if !strings.EqualFold(strings.TrimSuffix(parsed.Hostname(), "."), "data-api.binance.vision") && len(config.AllowedHosts) == 0 {
		return nil, errors.New("Binance base URL must use data-api.binance.vision unless an explicit test host allowlist is supplied")
	}
	allowed := config.AllowedHosts
	if len(allowed) == 0 {
		allowed = map[string]struct{}{strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")): {}}
	}
	pageSize := config.PageSize
	if pageSize <= 0 {
		pageSize = maxKlineLimit
	}
	if pageSize > maxKlineLimit {
		return nil, fmt.Errorf("Binance kline page size must not exceed %d", maxKlineLimit)
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
		baseURL: baseURL, pageSize: pageSize, maxPages: maxPages,
		now: config.Now,
		fetcher: ingestion.HTTPFetcher{
			Client: config.HTTPClient, AllowedHosts: allowed, AllowedMediaTypes: mediaTypes,
			MaxBodyBytes: config.MaxBodyBytes, Timeout: config.Timeout, Retry: config.Retry, RateLimit: config.RateLimit,
		},
	}, nil
}

func (c *Client) nowUTC() time.Time {
	if c != nil && c.now != nil {
		return c.now().UTC()
	}
	return time.Now().UTC()
}

func (c *Client) buildRequest(path string, values url.Values) (ingestion.FetchRequest, error) {
	if c == nil {
		return ingestion.FetchRequest{}, errors.New("Binance client is required")
	}
	if path != "/api/v3/exchangeInfo" && path != "/api/v3/klines" {
		return ingestion.FetchRequest{}, ErrUnsupportedEndpoint
	}
	parsed, err := url.Parse(c.baseURL + path)
	if err != nil {
		return ingestion.FetchRequest{}, errors.New("invalid Binance request path")
	}
	parsed.RawQuery = values.Encode()
	return ingestion.FetchRequest{URL: parsed.String()}, nil
}

func (c *Client) BuildExchangeInfoRequest() (ingestion.FetchRequest, error) {
	return c.buildRequest("/api/v3/exchangeInfo", url.Values{"permissions": {"SPOT"}})
}

type KlineRequest struct {
	Symbol    string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
}

func (r KlineRequest) normalized(defaultLimit int) (KlineRequest, error) {
	r.Symbol = strings.ToUpper(strings.TrimSpace(r.Symbol))
	if !validSymbol(r.Symbol) {
		return KlineRequest{}, errors.New("Binance symbol is required and must contain only letters or digits")
	}
	if r.Limit <= 0 {
		r.Limit = defaultLimit
		if r.Limit <= 0 {
			r.Limit = maxKlineLimit
		}
	}
	if r.Limit > maxKlineLimit {
		return KlineRequest{}, fmt.Errorf("Binance kline limit must not exceed %d", maxKlineLimit)
	}
	if r.StartTime != nil {
		start := r.StartTime.UTC()
		r.StartTime = &start
	}
	if r.EndTime != nil {
		end := r.EndTime.UTC()
		r.EndTime = &end
	}
	if r.StartTime != nil && r.EndTime != nil && r.EndTime.Before(*r.StartTime) {
		return KlineRequest{}, errors.New("Binance kline end time precedes start time")
	}
	return r, nil
}

func (c *Client) BuildKlineRequest(request KlineRequest) (ingestion.FetchRequest, error) {
	if c == nil {
		return ingestion.FetchRequest{}, errors.New("Binance client is required")
	}
	normalized, err := request.normalized(c.pageSize)
	if err != nil {
		return ingestion.FetchRequest{}, err
	}
	values := url.Values{"interval": {"1d"}, "symbol": {normalized.Symbol}, "limit": {strconv.Itoa(normalized.Limit)}}
	if normalized.StartTime != nil {
		values.Set("startTime", strconv.FormatInt(normalized.StartTime.UnixMilli(), 10))
	}
	if normalized.EndTime != nil {
		values.Set("endTime", strconv.FormatInt(normalized.EndTime.UnixMilli(), 10))
	}
	return c.buildRequest("/api/v3/klines", values)
}

func (c *Client) fetch(ctx context.Context, request ingestion.FetchRequest) (ingestion.FetchedResponse, error) {
	return c.fetcher.Fetch(ctx, request)
}

func (c *Client) FetchExchangeInfo(ctx context.Context) (ExchangeInfoResponse, ingestion.FetchedResponse, error) {
	request, err := c.BuildExchangeInfoRequest()
	if err != nil {
		return ExchangeInfoResponse{}, ingestion.FetchedResponse{}, err
	}
	fetched, err := c.fetch(ctx, request)
	if err != nil {
		return ExchangeInfoResponse{}, ingestion.FetchedResponse{}, err
	}
	var response ExchangeInfoResponse
	if err := json.Unmarshal(fetched.Body, &response); err != nil {
		return ExchangeInfoResponse{}, ingestion.FetchedResponse{}, fmt.Errorf("decode Binance exchange info: %w", err)
	}
	return response, fetched, nil
}

func (c *Client) FetchKlinePage(ctx context.Context, request KlineRequest) (KlinePage, error) {
	if c == nil {
		return KlinePage{}, errors.New("Binance client is required")
	}
	normalized, err := request.normalized(c.pageSize)
	if err != nil {
		return KlinePage{}, err
	}
	built, err := c.BuildKlineRequest(normalized)
	if err != nil {
		return KlinePage{}, err
	}
	fetched, err := c.fetch(ctx, built)
	if err != nil {
		return KlinePage{}, err
	}
	klines, err := decodeKlines(fetched.Body)
	if err != nil {
		return KlinePage{}, fmt.Errorf("decode Binance klines for %s: %w", normalized.Symbol, err)
	}
	return KlinePage{Request: normalized, Klines: klines, Fetched: fetched}, nil
}

func (c *Client) FetchKlinePages(ctx context.Context, request KlineRequest) ([]KlinePage, error) {
	if c == nil {
		return nil, errors.New("Binance client is required")
	}
	normalized, err := request.normalized(c.pageSize)
	if err != nil {
		return nil, err
	}
	pages := make([]KlinePage, 0, minInt(c.maxPages, 8))
	for page := 0; page < c.maxPages; page++ {
		current, err := c.FetchKlinePage(ctx, normalized)
		if err != nil {
			return nil, err
		}
		pages = append(pages, current)
		checkpoint := NewKlineCheckpoint(request)
		for _, previous := range pages {
			checkpoint = checkpoint.Advance(previous)
		}
		if len(current.Klines) == 0 || len(current.Klines) < normalized.Limit || checkpoint.Completed {
			return pages, nil
		}
		if checkpoint.NextStartTime == nil {
			return nil, errors.New("Binance kline pagination did not advance")
		}
		normalized.StartTime = checkpoint.NextStartTime
	}
	return nil, ErrPageLimit
}

func RequestFingerprint(request KlineRequest, defaultLimit int) (string, error) {
	normalized, err := request.normalized(defaultLimit)
	if err != nil {
		return "", err
	}
	identity := struct {
		Symbol string `json:"symbol"`
		Start  int64  `json:"start_time,omitempty"`
		End    int64  `json:"end_time,omitempty"`
		Limit  int    `json:"limit"`
	}{Symbol: normalized.Symbol, Limit: normalized.Limit}
	if normalized.StartTime != nil {
		identity.Start = normalized.StartTime.UnixMilli()
	}
	if normalized.EndTime != nil {
		identity.End = normalized.EndTime.UnixMilli()
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

type KlineCheckpoint struct {
	Symbol             string     `json:"symbol"`
	RequestFingerprint string     `json:"request_fingerprint"`
	NextStartTime      *time.Time `json:"next_start_time,omitempty"`
	Pages              int        `json:"pages"`
	Candles            int        `json:"candles"`
	Completed          bool       `json:"completed"`
}

func NewKlineCheckpoint(request KlineRequest) KlineCheckpoint {
	defaultLimit := request.Limit
	if defaultLimit <= 0 {
		defaultLimit = maxKlineLimit
	}
	normalized, err := request.normalized(defaultLimit)
	if err != nil {
		return KlineCheckpoint{}
	}
	fingerprint, _ := RequestFingerprint(normalized, defaultLimit)
	return KlineCheckpoint{Symbol: normalized.Symbol, RequestFingerprint: fingerprint, NextStartTime: normalized.StartTime}
}

func (c KlineCheckpoint) NextRequest(request KlineRequest, defaultLimit ...int) (KlineRequest, error) {
	limit := maxKlineLimit
	if len(defaultLimit) > 0 && defaultLimit[0] > 0 {
		limit = defaultLimit[0]
	}
	fingerprint, err := RequestFingerprint(request, limit)
	if err != nil || c.RequestFingerprint == "" || fingerprint != c.RequestFingerprint {
		return KlineRequest{}, ErrCheckpointMismatch
	}
	normalized, err := request.normalized(limit)
	if err != nil {
		return KlineRequest{}, err
	}
	if normalized.Symbol != c.Symbol {
		return KlineRequest{}, ErrCheckpointMismatch
	}
	normalized.StartTime = c.NextStartTime
	return normalized, nil
}

func (c KlineCheckpoint) Advance(page KlinePage) KlineCheckpoint {
	c.Pages++
	c.Candles += len(page.Klines)
	if len(page.Klines) == 0 || len(page.Klines) < page.Request.Limit {
		c.Completed = true
		return c
	}
	last := page.Klines[len(page.Klines)-1]
	next := last.OpenTime.UTC().AddDate(0, 0, 1)
	c.NextStartTime = &next
	return c
}

func validSymbol(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
