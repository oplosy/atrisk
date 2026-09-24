// Package tcmb provides bounded ingestion for the documented EVDS2 service
// contract. EVDS3's public service contract is not assumed here; the base URL
// is configurable so a later documented migration does not change callers.
package tcmb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oplosy/atrisk/internal/ingestion"
)

const AdapterVersion = "tcmb-evds2-v1"

var (
	ErrAPIKeyRequired            = errors.New("TCMB EVDS API key is required for network requests")
	ErrPageLimit                 = errors.New("TCMB EVDS pagination limit reached")
	ErrNoCheckpoint              = errors.New("TCMB EVDS checkpoint not found")
	ErrCheckpointRequestMismatch = errors.New("TCMB EVDS checkpoint request fingerprint mismatch")
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
		baseURL = "https://evds2.tcmb.gov.tr"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" {
		return nil, errors.New("TCMB EVDS base URL must be HTTPS without credentials or query parameters")
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
		return nil, errors.New("TCMB EVDS page size must not exceed 10000")
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
		fetcher: ingestion.HTTPFetcher{Client: config.HTTPClient, AllowedHosts: allowed, AllowedMediaTypes: mediaTypes,
			MaxBodyBytes: config.MaxBodyBytes, Timeout: config.Timeout, Retry: config.Retry, RateLimit: config.RateLimit},
	}, nil
}

func (c *Client) buildRequest(request SeriesRequest) (ingestion.FetchRequest, error) {
	if c == nil {
		return ingestion.FetchRequest{}, errors.New("TCMB EVDS client is required")
	}
	if c.apiKey == "" {
		return ingestion.FetchRequest{}, ErrAPIKeyRequired
	}
	normalized, err := request.normalized(c.pageSize)
	if err != nil {
		return ingestion.FetchRequest{}, err
	}
	// EVDS2 documents the complete parameter sequence as the service path,
	// rather than as a conventional query string.
	parts := []string{
		"series=" + url.PathEscape(strings.Join(normalized.Series, "-")),
		"startDate=" + url.QueryEscape(normalized.StartDate),
		"endDate=" + url.QueryEscape(normalized.EndDate),
		"type=json",
	}
	if normalized.AggregationTypes != "" {
		parts = append(parts, "aggregationTypes="+url.QueryEscape(normalized.AggregationTypes))
	}
	if normalized.Formulas != "" {
		parts = append(parts, "formulas="+url.QueryEscape(normalized.Formulas))
	}
	if normalized.Frequency != "" {
		parts = append(parts, "frequency="+url.QueryEscape(normalized.Frequency))
	}
	if normalized.DecimalSeparator != "" {
		// EVDS documents this parameter with the historic spelling.
		parts = append(parts, "decimalSeperator="+url.QueryEscape(normalized.DecimalSeparator))
	}
	requestURL := c.baseURL + "/service/evds/" + strings.Join(parts, "&")
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return ingestion.FetchRequest{}, errors.New("invalid TCMB EVDS request path")
	}
	headers := make(http.Header)
	headers.Set("key", c.apiKey)
	return ingestion.FetchRequest{URL: parsed.String(), Headers: headers}, nil
}

func (c *Client) request(ctx context.Context, request SeriesRequest) (ingestion.FetchedResponse, SeriesResponse, error) {
	fetchRequest, err := c.buildRequest(request)
	if err != nil {
		return ingestion.FetchedResponse{}, SeriesResponse{}, err
	}
	fetched, err := c.fetcher.Fetch(ctx, fetchRequest)
	if err != nil {
		return ingestion.FetchedResponse{}, SeriesResponse{}, err
	}
	response, err := decodeResponse(fetched.Body)
	if err != nil {
		return ingestion.FetchedResponse{}, SeriesResponse{}, err
	}
	return fetched, response, nil
}

type SeriesRequest struct {
	Series           []string
	StartDate        string
	EndDate          string
	Frequency        string
	AggregationTypes string
	Formulas         string
	DecimalSeparator string
}

func (r SeriesRequest) normalized(defaultPageSize int) (SeriesRequest, error) {
	if len(r.Series) == 0 {
		return SeriesRequest{}, errors.New("TCMB EVDS series is required")
	}
	seen := make(map[string]struct{}, len(r.Series))
	series := make([]string, 0, len(r.Series))
	for _, raw := range r.Series {
		value := strings.TrimSpace(raw)
		if value == "" || strings.ContainsAny(value, "\r\n") || !seriesCodePattern.MatchString(value) {
			return SeriesRequest{}, fmt.Errorf("invalid TCMB EVDS series code %q", raw)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		series = append(series, value)
	}
	if len(series) == 0 {
		return SeriesRequest{}, errors.New("TCMB EVDS series is required")
	}
	start, err := parseRequestDate(r.StartDate)
	if err != nil {
		return SeriesRequest{}, fmt.Errorf("invalid TCMB EVDS start date: %w", err)
	}
	end, err := parseRequestDate(r.EndDate)
	if err != nil {
		return SeriesRequest{}, fmt.Errorf("invalid TCMB EVDS end date: %w", err)
	}
	if end.Before(start) {
		return SeriesRequest{}, errors.New("TCMB EVDS end date precedes start date")
	}
	r.Series, r.StartDate, r.EndDate = series, formatRequestDate(start), formatRequestDate(end)
	if r.DecimalSeparator == "" {
		r.DecimalSeparator = "."
	}
	if r.DecimalSeparator != "." && r.DecimalSeparator != "," {
		return SeriesRequest{}, errors.New("TCMB EVDS decimal separator must be '.' or ','")
	}
	if defaultPageSize <= 0 {
		defaultPageSize = 1000
	}
	return r, nil
}

var seriesCodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type SeriesResponse struct {
	TotalCount int
	Items      []map[string]json.RawMessage
}

func decodeResponse(body []byte) (SeriesResponse, error) {
	var envelope struct {
		TotalCount json.RawMessage              `json:"totalCount"`
		Items      []map[string]json.RawMessage `json:"items"`
		Error      json.RawMessage              `json:"error"`
		Message    string                       `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return SeriesResponse{}, fmt.Errorf("decode TCMB EVDS response: %w", err)
	}
	if len(envelope.Items) == 0 && len(envelope.Error) > 0 {
		var message string
		_ = json.Unmarshal(envelope.Error, &message)
		if message == "" {
			message = envelope.Message
		}
		if message == "" {
			message = "source returned an EVDS error"
		}
		return SeriesResponse{}, errors.New(message)
	}
	total, err := decodeCount(envelope.TotalCount)
	if err != nil {
		return SeriesResponse{}, fmt.Errorf("decode TCMB EVDS totalCount: %w", err)
	}
	return SeriesResponse{TotalCount: total, Items: envelope.Items}, nil
}

func decodeCount(raw json.RawMessage) (int, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var number int
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(text))
}

func (c *Client) FetchSeries(ctx context.Context, request SeriesRequest) (SeriesResponse, ingestion.FetchedResponse, error) {
	if c == nil {
		return SeriesResponse{}, ingestion.FetchedResponse{}, errors.New("TCMB EVDS client is required")
	}
	fetched, response, err := c.request(ctx, request)
	return response, fetched, err
}

type SeriesPage struct {
	Response SeriesResponse
	Fetched  ingestion.FetchedResponse
}

func (c *Client) FetchSeriesPages(ctx context.Context, request SeriesRequest) ([]SeriesPage, error) {
	if c == nil {
		return nil, errors.New("TCMB EVDS client is required")
	}
	normalized, err := request.normalized(c.pageSize)
	if err != nil {
		return nil, err
	}
	response, fetched, err := c.FetchSeries(ctx, normalized)
	if err != nil {
		return nil, err
	}
	// EVDS2 date-window requests are not offset-paginated. Keep one bounded
	// page and expose a checkpoint for callers that split date windows.
	return []SeriesPage{{Response: response, Fetched: fetched}}, nil
}

type ObservationCheckpoint struct {
	RequestFingerprint string `json:"request_fingerprint"`
	NextStartDate      string `json:"next_start_date"`
	Frequency          string `json:"frequency,omitempty"`
	Observations       int    `json:"observations"`
	Pages              int    `json:"pages"`
	Completed          bool   `json:"completed"`
}

func NewObservationCheckpoint(request SeriesRequest, defaultPageSize int) (ObservationCheckpoint, error) {
	fingerprint, err := RequestFingerprint(request, defaultPageSize)
	if err != nil {
		return ObservationCheckpoint{}, err
	}
	normalized, err := request.normalized(defaultPageSize)
	if err != nil {
		return ObservationCheckpoint{}, err
	}
	return ObservationCheckpoint{RequestFingerprint: fingerprint, NextStartDate: normalized.StartDate, Frequency: normalized.Frequency}, nil
}

func RequestFingerprint(request SeriesRequest, defaultPageSize int) (string, error) {
	normalized, err := request.normalized(defaultPageSize)
	if err != nil {
		return "", err
	}
	series := append([]string(nil), normalized.Series...)
	sort.Strings(series)
	identity := struct {
		Series                                                                      []string
		StartDate, EndDate, Frequency, AggregationTypes, Formulas, DecimalSeparator string
	}{series, normalized.StartDate, normalized.EndDate, normalized.Frequency, normalized.AggregationTypes, normalized.Formulas, normalized.DecimalSeparator}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func (c ObservationCheckpoint) NextRequest(request SeriesRequest, defaultPageSize ...int) (SeriesRequest, error) {
	pageSize := 1000
	if len(defaultPageSize) > 0 && defaultPageSize[0] > 0 {
		pageSize = defaultPageSize[0]
	}
	fingerprint, err := RequestFingerprint(request, pageSize)
	if err != nil || c.RequestFingerprint == "" || fingerprint != c.RequestFingerprint {
		return SeriesRequest{}, ErrCheckpointRequestMismatch
	}
	normalized, err := request.normalized(pageSize)
	if err != nil {
		return SeriesRequest{}, err
	}
	if c.NextStartDate == "" || c.Completed {
		return normalized, nil
	}
	normalized.StartDate = c.NextStartDate
	return normalized, nil
}

func (c ObservationCheckpoint) Advance(page SeriesPage) ObservationCheckpoint {
	c.Pages++
	c.Observations += len(page.Response.Items)
	maxDate := time.Time{}
	for _, item := range page.Response.Items {
		var raw string
		if err := json.Unmarshal(item["Tarih"], &raw); err != nil {
			continue
		}
		parsed, err := parseEVDSDate(raw)
		if err == nil && parsed.After(maxDate) {
			maxDate = parsed
		}
	}
	if !maxDate.IsZero() {
		c.NextStartDate = formatRequestDate(advanceEVDSDate(maxDate, c.Frequency))
	}
	c.Completed = len(page.Response.Items) == 0 || (page.Response.TotalCount > 0 && c.Observations >= page.Response.TotalCount)
	return c
}

func advanceEVDSDate(value time.Time, frequency string) time.Time {
	switch strings.ToLower(strings.TrimSpace(frequency)) {
	case "2", "bdaily", "business_daily", "b":
		value = value.AddDate(0, 0, 1)
		for value.Weekday() == time.Saturday || value.Weekday() == time.Sunday {
			value = value.AddDate(0, 0, 1)
		}
		return value
	case "3", "weekly", "w":
		return value.AddDate(0, 0, 7)
	case "4", "semimonthly", "sm":
		return value.AddDate(0, 0, 15)
	case "5", "monthly", "m":
		return value.AddDate(0, 1, 0)
	case "6", "quarterly", "q":
		return value.AddDate(0, 3, 0)
	case "7", "semiyearly", "6m":
		return value.AddDate(0, 6, 0)
	case "8", "yearly", "y":
		return value.AddDate(1, 0, 0)
	default:
		return value.AddDate(0, 0, 1)
	}
}

func parseRequestDate(value string) (time.Time, error) {
	return time.ParseInLocation("02-01-2006", strings.TrimSpace(value), time.UTC)
}

func formatRequestDate(value time.Time) string { return value.UTC().Format("02-01-2006") }

func parseEVDSDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"02-01-2006", "2006-1", "2006-01", "2006-Q1", "2006"} {
		if layout == "2006-Q1" {
			if len(value) == 7 && value[4] == '-' && value[5] == 'Q' && value[6] >= '1' && value[6] <= '4' {
				month := (int(value[6]-'1') * 3) + 1
				return time.Date(atoiYear(value[:4]), time.Month(month), 1, 0, 0, 0, 0, time.UTC), nil
			}
			continue
		}
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported EVDS date %q", value)
}

func atoiYear(value string) int { year, _ := strconv.Atoi(value); return year }
