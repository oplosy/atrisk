package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/ingestion"
	"github.com/oplosy/atrisk/internal/sources/binance"
	"github.com/oplosy/atrisk/internal/sources/fred"
	"github.com/oplosy/atrisk/internal/sources/tcmb"
)

type Runner struct {
	Pool    *pgxpool.Pool
	Archive archive.Store
}

type claimConfiguration struct {
	Request       json.RawMessage `json:"request"`
	CredentialEnv string          `json:"credential_env"`
	BaseURL       string          `json:"base_url"`
}

type fredRequest struct {
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
	Offset           int    `json:"offset"`
	MaxPages         int    `json:"max_pages"`
}

func (r Runner) Run(ctx context.Context, claim Claim) (RunOutcome, error) {
	if r.Pool == nil || r.Archive == nil {
		return RunOutcome{}, errors.New("collector runner database and archive are required")
	}
	var configuration claimConfiguration
	if err := json.Unmarshal(claim.Configuration, &configuration); err != nil {
		return RunOutcome{}, fmt.Errorf("decode schedule configuration: %w", err)
	}
	var runSpec ingestion.RunSpec
	runSpec.SourceID = claim.SourceID
	runSpec.IdempotencyKey = claim.OccurrenceKey
	requestBody := configuration.Request
	switch claim.Provider {
	case "fred":
		return r.runFRED(ctx, claim, configuration, requestBody, runSpec)
	case "tcmb":
		return r.runTCMB(ctx, claim, configuration, requestBody, runSpec)
	case "binance":
		return r.runBinance(ctx, claim, configuration, requestBody, runSpec)
	default:
		return RunOutcome{}, fmt.Errorf("unsupported provider %q", claim.Provider)
	}
}

func (r Runner) runFRED(ctx context.Context, claim Claim, config claimConfiguration, body []byte, spec ingestion.RunSpec) (RunOutcome, error) {
	var request fredRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return RunOutcome{}, fmt.Errorf("decode FRED request: %w", err)
	}
	if request.SeriesID == "" && claim.SeriesID != nil {
		if err := r.Pool.QueryRow(ctx, `SELECT source_code FROM series WHERE id = $1::uuid`, *claim.SeriesID).Scan(&request.SeriesID); err != nil {
			return RunOutcome{}, fmt.Errorf("resolve FRED series source code: %w", err)
		}
	}
	apiKey := os.Getenv(config.CredentialEnv)
	if apiKey == "" {
		return RunOutcome{}, errors.New("FRED credential environment variable is empty")
	}
	client, err := fred.NewClient(fred.Config{BaseURL: config.BaseURL, APIKey: apiKey})
	if err != nil {
		return RunOutcome{}, err
	}
	observationRequest := fred.ObservationRequest{
		SeriesID: request.SeriesID, RealtimeStart: request.RealtimeStart, RealtimeEnd: request.RealtimeEnd,
		VintageDates: request.VintageDates, ObservationStart: request.ObservationStart, ObservationEnd: request.ObservationEnd,
		Units: request.Units, Frequency: request.Frequency, Aggregation: request.Aggregation, OutputType: request.OutputType,
		Limit: request.Limit, Offset: request.Offset,
	}
	adapter, err := fred.NewAdapter(client, request.SeriesID, observationRequest)
	if err != nil {
		return RunOutcome{}, err
	}
	seriesID := claim.SeriesID
	if seriesID == nil || *seriesID == "" {
		return RunOutcome{}, errors.New("FRED schedule series_id is required")
	}
	spec.DatasetID = stringPointer(claim.DatasetID)
	spec.AdapterVersion = fred.AdapterVersion
	store := ingestion.DatabaseStore{Pool: r.Pool}
	runID, duplicate, err := store.StartRun(ctx, spec)
	if err != nil {
		return RunOutcome{}, err
	}
	if duplicate {
		return r.resumeFRED(ctx, claim, runID, request, observationRequest, adapter, client)
	}
	return r.resumeFRED(ctx, claim, runID, request, observationRequest, adapter, client)
}

func (r Runner) resumeFRED(ctx context.Context, claim Claim, runID string, rawRequest fredRequest, request fred.ObservationRequest, adapter *fred.Adapter, client *fred.Client) (RunOutcome, error) {
	store := ingestion.DatabaseStore{Pool: r.Pool}
	fredStore := fred.Store{Pool: r.Pool}
	checkpoint, err := fred.NewObservationCheckpoint(request, request.Limit)
	if err != nil {
		_ = store.FailRun(ctx, runID, "checkpoint_invalid")
		return RunOutcome{}, err
	}
	if existing, loadErr := fredStore.LoadCheckpoint(ctx, runID); loadErr == nil {
		checkpoint = existing
	}
	if checkpoint.Completed {
		status, statusErr := store.LoadRunStatus(ctx, runID)
		if statusErr != nil {
			return RunOutcome{}, statusErr
		}
		if status != "succeeded" {
			if err := store.CompleteRun(ctx, runID, "succeeded", map[string]any{"pages": checkpoint.Pages, "observations": checkpoint.Observations}); err != nil {
				return RunOutcome{}, err
			}
		}
		return RunOutcome{RunID: runID, Checkpoint: mustJSON(checkpoint), Complete: true}, nil
	}
	maxPages := rawRequest.MaxPages
	if maxPages <= 0 {
		maxPages = 100
	}
	for pageNo := checkpoint.Pages; pageNo < maxPages; pageNo++ {
		current, err := checkpoint.NextRequest(request, request.Limit)
		if err != nil {
			_ = store.FailRun(ctx, runID, "checkpoint_mismatch")
			return RunOutcome{}, err
		}
		response, fetched, err := client.FetchObservationPage(ctx, current)
		if err != nil {
			_ = store.FailRun(ctx, runID, "fetch_failed")
			return RunOutcome{}, err
		}
		page := fred.ObservationPage{Response: response, Fetched: fetched}
		payload, err := r.archiveAndRegister(ctx, store, runID, claim.OccurrenceKey, fetched, pageNo)
		if err != nil {
			_ = store.FailRun(ctx, runID, "archive_failed")
			return RunOutcome{}, err
		}
		records, err := adapter.Normalize(ctx, payload)
		if err != nil {
			_ = store.FailRun(ctx, runID, "normalize_failed")
			return RunOutcome{}, err
		}
		if _, err = fredStore.PersistRecords(ctx, *claim.SeriesID, records); err != nil {
			_ = store.FailRun(ctx, runID, "persistence_failed")
			return RunOutcome{}, err
		}
		checkpoint = checkpoint.Advance(fred.ObservationPage{Response: page.Response, Fetched: page.Fetched})
		if err = fredStore.SaveCheckpoint(ctx, runID, checkpoint); err != nil {
			_ = store.FailRun(ctx, runID, "checkpoint_failed")
			return RunOutcome{}, err
		}
		if checkpoint.Completed {
			if err = store.CompleteRun(ctx, runID, "succeeded", map[string]any{"pages": checkpoint.Pages, "observations": checkpoint.Observations}); err != nil {
				return RunOutcome{}, err
			}
			return RunOutcome{RunID: runID, Checkpoint: mustJSON(checkpoint), Complete: true}, nil
		}
	}
	_ = store.FailRun(ctx, runID, "incomplete_coverage")
	return RunOutcome{}, errors.New("FRED page limit reached; scheduled run is incomplete")
}

func (r Runner) runTCMB(ctx context.Context, claim Claim, config claimConfiguration, body []byte, spec ingestion.RunSpec) (RunOutcome, error) {
	var request tcmb.SeriesRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return RunOutcome{}, fmt.Errorf("decode TCMB request: %w", err)
	}
	client, err := tcmb.NewClient(tcmb.Config{BaseURL: config.BaseURL, APIKey: os.Getenv(config.CredentialEnv)})
	if err != nil {
		return RunOutcome{}, err
	}
	adapter, err := tcmb.NewAdapter(client, request)
	if err != nil {
		return RunOutcome{}, err
	}
	if claim.SeriesID == nil || *claim.SeriesID == "" {
		return RunOutcome{}, errors.New("TCMB schedule series_id is required")
	}
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://evds2.tcmb.gov.tr"
	}
	pipeline, err := r.pipeline(baseURL)
	if err != nil {
		return RunOutcome{}, err
	}
	pipeline.Persist = func(ctx context.Context, _ string, records []ingestion.NormalizedRecord) error {
		_, err := (tcmb.Store{Pool: r.Pool}).PersistRecords(ctx, *claim.SeriesID, records)
		return err
	}
	pipeline.Coverage = tcmbCoverage
	spec.DatasetID, spec.AdapterVersion = stringPointer(claim.DatasetID), tcmb.AdapterVersion
	result, err := pipeline.RunAdapter(ctx, spec, adapter)
	if err != nil {
		return RunOutcome{}, err
	}
	return RunOutcome{RunID: result.RunID, Complete: true}, nil
}

func (r Runner) runBinance(ctx context.Context, claim Claim, config claimConfiguration, body []byte, spec ingestion.RunSpec) (RunOutcome, error) {
	var request struct {
		Symbol    string   `json:"symbol"`
		Symbols   []string `json:"symbols"`
		StartTime *string  `json:"start_time"`
		EndTime   *string  `json:"end_time"`
		Limit     int      `json:"limit"`
		MaxPages  int      `json:"max_pages"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return RunOutcome{}, fmt.Errorf("decode Binance request: %w", err)
	}
	client, err := binance.NewClient(binance.Config{BaseURL: config.BaseURL})
	if err != nil {
		return RunOutcome{}, err
	}
	startTime, err := parseOptionalTime(request.StartTime)
	if err != nil {
		return RunOutcome{}, err
	}
	endTime, err := parseOptionalTime(request.EndTime)
	if err != nil {
		return RunOutcome{}, err
	}
	klineRequest := binance.KlineRequest{Symbol: request.Symbol, StartTime: startTime, EndTime: endTime, Limit: request.Limit}
	adapter, err := binance.NewAdapter(client, request.Symbols, klineRequest)
	if err != nil {
		return RunOutcome{}, err
	}
	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = "https://data-api.binance.vision"
	}
	pipeline, err := r.pipeline(baseURL)
	if err != nil {
		return RunOutcome{}, err
	}
	pipeline.Persist = func(ctx context.Context, runID string, records []ingestion.NormalizedRecord) error {
		store := binance.Store{Pool: r.Pool}
		if request.Symbol == "" {
			_, err := store.PersistInstrumentRecords(ctx, records)
			return err
		}
		_, err := store.PersistPriceRecords(ctx, request.Symbol, records)
		return err
	}
	if request.Symbol != "" {
		spec.DatasetID, spec.AdapterVersion = stringPointer(claim.DatasetID), binance.AdapterVersion
		store := ingestion.DatabaseStore{Pool: r.Pool}
		runID, duplicate, err := store.StartRun(ctx, spec)
		if err != nil {
			return RunOutcome{}, err
		}
		if duplicate {
			return r.resumeBinance(ctx, claim, runID, klineRequest, request.MaxPages, client, adapter)
		}
		return r.resumeBinance(ctx, claim, runID, klineRequest, request.MaxPages, client, adapter)
	}
	pipeline.Coverage = func(payload ingestion.RawPayload) error {
		var rows [][]json.RawMessage
		if err := json.Unmarshal(payload.Body, &rows); err != nil {
			return fmt.Errorf("decode Binance coverage: %w", err)
		}
		limit := request.Limit
		if limit <= 0 {
			limit = 1000
		}
		if len(rows) >= limit {
			return errors.New("Binance response reached page limit; scheduled run is incomplete")
		}
		return nil
	}
	spec.DatasetID, spec.AdapterVersion = stringPointer(claim.DatasetID), binance.AdapterVersion
	result, err := pipeline.RunAdapter(ctx, spec, adapter)
	if err != nil {
		return RunOutcome{}, err
	}
	return RunOutcome{RunID: result.RunID, Complete: true}, nil
}

func (r Runner) resumeBinance(ctx context.Context, claim Claim, runID string, request binance.KlineRequest, maxPages int, client *binance.Client, adapter *binance.Adapter) (RunOutcome, error) {
	store := ingestion.DatabaseStore{Pool: r.Pool}
	priceStore := binance.Store{Pool: r.Pool}
	checkpoint := binance.NewKlineCheckpoint(request)
	if existing, loadErr := priceStore.LoadCheckpoint(ctx, runID); loadErr == nil {
		checkpoint = existing
	}
	if err := checkpoint.ValidateForSymbol(request.Symbol); err != nil {
		_ = store.FailRun(ctx, runID, "checkpoint_mismatch")
		return RunOutcome{}, err
	}
	if checkpoint.Completed {
		status, statusErr := store.LoadRunStatus(ctx, runID)
		if statusErr != nil {
			return RunOutcome{}, statusErr
		}
		if status != "succeeded" {
			if err := store.CompleteRun(ctx, runID, "succeeded", map[string]any{"pages": checkpoint.Pages, "candles": checkpoint.Candles}); err != nil {
				return RunOutcome{}, err
			}
		}
		return RunOutcome{RunID: runID, Checkpoint: mustJSON(checkpoint), Complete: true}, nil
	}
	if maxPages <= 0 {
		maxPages = 100
	}
	for pageNo := checkpoint.Pages; pageNo < maxPages; pageNo++ {
		current, err := checkpoint.NextRequest(request, request.Limit)
		if err != nil {
			_ = store.FailRun(ctx, runID, "checkpoint_mismatch")
			return RunOutcome{}, err
		}
		page, err := client.FetchKlinePage(ctx, current)
		if err != nil {
			_ = store.FailRun(ctx, runID, "fetch_failed")
			return RunOutcome{}, err
		}
		payload, err := r.archiveAndRegister(ctx, store, runID, claim.OccurrenceKey, page.Fetched, pageNo)
		if err != nil {
			_ = store.FailRun(ctx, runID, "archive_failed")
			return RunOutcome{}, err
		}
		records, err := adapter.NormalizeKlines(ctx, request.Symbol, payload)
		if err != nil {
			_ = store.FailRun(ctx, runID, "normalize_failed")
			return RunOutcome{}, err
		}
		checkpoint = checkpoint.AdvanceComplete(page, time.Now().UTC())
		if _, err = priceStore.PersistPriceRecordsAndCheckpoint(ctx, runID, request.Symbol, records, checkpoint); err != nil {
			_ = store.FailRun(ctx, runID, "persistence_failed")
			return RunOutcome{}, err
		}
		if checkpoint.Completed {
			if err = store.CompleteRun(ctx, runID, "succeeded", map[string]any{"pages": checkpoint.Pages, "candles": checkpoint.Candles}); err != nil {
				return RunOutcome{}, err
			}
			return RunOutcome{RunID: runID, Checkpoint: mustJSON(checkpoint), Complete: true}, nil
		}
	}
	_ = store.FailRun(ctx, runID, "incomplete_coverage")
	return RunOutcome{}, errors.New("Binance page limit reached; scheduled run is incomplete")
}

func (r Runner) archiveAndRegister(ctx context.Context, store ingestion.DatabaseStore, runID, occurrence string, fetched ingestion.FetchedResponse, page int) (ingestion.RawPayload, error) {
	mediaType := fetched.MediaType
	if mediaType == "" {
		mediaType = "application/json"
	}
	ref, err := archive.ArchivePayload(ctx, r.Archive, fetched.Body, mediaType, map[string]string{"request-uri": fetched.RequestURI, "correlation-id": fetched.CorrelationID})
	if err != nil {
		return ingestion.RawPayload{}, err
	}
	if _, err = store.RegisterRawObject(ctx, ingestion.RawObjectRegistration{Reference: ref, RetrievedAt: fetched.RetrievedAt, RequestURI: fetched.RequestURI, RequestHeaders: fetched.Headers, IngestionRunID: &runID, OccurrenceKey: occurrence + ":page:" + strconv.Itoa(page)}); err != nil {
		return ingestion.RawPayload{}, err
	}
	return ingestion.RawPayload{Body: fetched.Body, MediaType: mediaType, RetrievedAt: fetched.RetrievedAt, CorrelationID: fetched.CorrelationID, RequestURI: fetched.RequestURI, Headers: fetched.Headers, Archive: ref}, nil
}

func parseOptionalTime(value *string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil, fmt.Errorf("invalid Binance time filter")
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func mustJSON(value any) []byte { data, _ := json.Marshal(value); return data }

func (r Runner) pipeline(baseURL string) (ingestion.Pipeline, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Hostname() == "" {
		return ingestion.Pipeline{}, errors.New("provider base URL is invalid")
	}
	return ingestion.Pipeline{Fetcher: &ingestion.HTTPFetcher{AllowedHosts: map[string]struct{}{strings.ToLower(parsed.Hostname()): {}}, MaxBodyBytes: 10 << 20}, Archive: r.Archive, Runs: ingestion.DatabaseStore{Pool: r.Pool}}, nil
}

func stringPointer(value *string) *string { return value }

func fredCoverage(payload ingestion.RawPayload) error {
	var response struct {
		Count        int               `json:"count"`
		Offset       int               `json:"offset"`
		Observations []json.RawMessage `json:"observations"`
	}
	if err := json.Unmarshal(payload.Body, &response); err != nil {
		return fmt.Errorf("decode FRED coverage: %w", err)
	}
	if response.Count > 0 && response.Offset+len(response.Observations) < response.Count {
		return errors.New("FRED response reached page limit; scheduled run is incomplete")
	}
	return nil
}

func tcmbCoverage(payload ingestion.RawPayload) error {
	var response struct {
		TotalCount int               `json:"totalCount"`
		Items      []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(payload.Body, &response); err != nil {
		return fmt.Errorf("decode TCMB coverage: %w", err)
	}
	if response.TotalCount > len(response.Items) {
		return errors.New("TCMB response contains more items than the bounded page; scheduled run is incomplete")
	}
	return nil
}
