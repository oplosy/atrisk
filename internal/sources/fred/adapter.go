package fred

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/ingestion"
)

const (
	ObservationRecordKind = "fred_observation"
	MissingValueMarker    = "."
)

var decimalPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// Adapter implements the common ingestion.Adapter contract for one FRED
// series. The client owns transport bounds and the adapter owns source
// request semantics and normalization.
type Adapter struct {
	Client      *Client
	SeriesID    string
	Observation ObservationRequest
}

func NewAdapter(client *Client, seriesID string, request ObservationRequest) (*Adapter, error) {
	if client == nil {
		return nil, errors.New("FRED client is required")
	}
	if err := validateSeriesID(seriesID); err != nil {
		return nil, err
	}
	request.SeriesID = seriesID
	if _, err := request.normalized(client.pageSize); err != nil {
		return nil, err
	}
	return &Adapter{Client: client, SeriesID: seriesID, Observation: request}, nil
}

func (a *Adapter) BuildRequest(_ context.Context, _ ingestion.RunSpec) (ingestion.FetchRequest, error) {
	if a == nil || a.Client == nil {
		return ingestion.FetchRequest{}, errors.New("FRED adapter client is required")
	}
	request := a.Observation
	request.SeriesID = a.SeriesID
	normalized, err := request.normalized(a.Client.pageSize)
	if err != nil {
		return ingestion.FetchRequest{}, err
	}
	values := observationQueryValues(normalized)
	return a.Client.buildRequest("fred/series/observations", values)
}

func observationQueryValues(request ObservationRequest) url.Values {
	values := url.Values{
		"series_id":   {request.SeriesID},
		"limit":       {fmt.Sprintf("%d", request.Limit)},
		"offset":      {fmt.Sprintf("%d", request.Offset)},
		"output_type": {fmt.Sprintf("%d", request.OutputType)},
	}
	for key, value := range map[string]string{
		"realtime_start": request.RealtimeStart, "realtime_end": request.RealtimeEnd,
		"vintage_dates": request.VintageDates, "observation_start": request.ObservationStart,
		"observation_end": request.ObservationEnd, "units": request.Units,
		"frequency": request.Frequency, "aggregation_method": request.Aggregation,
	} {
		if value != "" {
			values.Set(key, value)
		}
	}
	return values
}

func (a *Adapter) Normalize(_ context.Context, payload ingestion.RawPayload) ([]ingestion.NormalizedRecord, error) {
	if a == nil {
		return nil, errors.New("FRED adapter is required")
	}
	if payload.MediaType != "" && payload.MediaType != "application/json" {
		return nil, fmt.Errorf("FRED response media type must be application/json, got %q", payload.MediaType)
	}
	var response ObservationResponse
	if err := json.Unmarshal(payload.Body, &response); err != nil {
		return nil, fmt.Errorf("decode FRED observations: %w", err)
	}
	seriesID := a.SeriesID
	if seriesID == "" {
		seriesID = a.Observation.SeriesID
	}
	if err := validateSeriesID(seriesID); err != nil {
		return nil, err
	}
	records := make([]ingestion.NormalizedRecord, 0, len(response.Observations))
	for index, observation := range response.Observations {
		record, err := normalizeObservation(seriesID, response, observation, payload.Archive)
		if err != nil {
			return nil, fmt.Errorf("normalize FRED observation %d: %w", index, err)
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			return nil, fmt.Errorf("encode FRED observation %d: %w", index, err)
		}
		records = append(records, ingestion.NormalizedRecord{
			Kind:            ObservationRecordKind,
			RawObjectKey:    payload.Archive.Key,
			RawObjectSHA256: payload.Archive.ContentSHA256,
			Payload:         encoded,
		})
	}
	return records, nil
}

type ObservationRecord struct {
	SeriesID           string            `json:"series_id"`
	ObservationTime    time.Time         `json:"observation_time"`
	Value              *string           `json:"value,omitempty"`
	ValueText          *string           `json:"value_text,omitempty"`
	SourceKnownAt      *time.Time        `json:"source_known_at,omitempty"`
	KnowledgeTimeBasis string            `json:"knowledge_time_basis"`
	RealtimeStart      string            `json:"realtime_start,omitempty"`
	RealtimeEnd        string            `json:"realtime_end,omitempty"`
	OutputType         int               `json:"output_type"`
	QualityFlags       map[string]any    `json:"quality_flags"`
	RawObject          archive.Reference `json:"raw_object"`
}

func normalizeObservation(seriesID string, response ObservationResponse, observation Observation, raw archive.Reference) (ObservationRecord, error) {
	observationTime, err := parseFREDDate(observation.Date)
	if err != nil {
		return ObservationRecord{}, fmt.Errorf("invalid observation date %q: %w", observation.Date, err)
	}
	if strings.TrimSpace(observation.Value) == "" {
		return ObservationRecord{}, errors.New("observation value is empty")
	}
	realtimeStart := observation.RealtimeStart
	if realtimeStart == "" {
		realtimeStart = response.RealtimeStart
	}
	realtimeEnd := observation.RealtimeEnd
	if realtimeEnd == "" {
		realtimeEnd = response.RealtimeEnd
	}
	record := ObservationRecord{
		SeriesID: seriesID, ObservationTime: observationTime, RealtimeStart: realtimeStart,
		RealtimeEnd: realtimeEnd, OutputType: response.OutputType,
		KnowledgeTimeBasis: "first_observed_by_system", QualityFlags: map[string]any{}, RawObject: raw,
	}
	if realtimeStart != "" {
		sourceKnownAt, parseErr := parseFREDDate(realtimeStart)
		if parseErr != nil {
			return ObservationRecord{}, fmt.Errorf("invalid realtime_start %q: %w", realtimeStart, parseErr)
		}
		record.SourceKnownAt = &sourceKnownAt
		record.KnowledgeTimeBasis = "source_published_at"
	}
	if realtimeEnd != "" {
		if _, parseErr := parseFREDDate(realtimeEnd); parseErr != nil {
			return ObservationRecord{}, fmt.Errorf("invalid realtime_end %q: %w", realtimeEnd, parseErr)
		}
	}
	if observation.Value == MissingValueMarker {
		marker := MissingValueMarker
		record.ValueText = &marker
		record.QualityFlags["missing"] = true
		record.QualityFlags["missing_marker"] = MissingValueMarker
	} else {
		value := strings.TrimSpace(observation.Value)
		if !decimalPattern.MatchString(value) {
			return ObservationRecord{}, fmt.Errorf("invalid numeric value %q", observation.Value)
		}
		record.Value = &value
	}
	if response.OutputType == 2 || response.OutputType == 3 {
		record.QualityFlags["vintage_aware"] = true
	}
	return record, nil
}

func parseFREDDate(value string) (time.Time, error) {
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func DecodeObservationRecord(record ingestion.NormalizedRecord) (ObservationRecord, error) {
	if record.Kind != ObservationRecordKind {
		return ObservationRecord{}, fmt.Errorf("unexpected FRED record kind %q", record.Kind)
	}
	var decoded ObservationRecord
	if err := json.Unmarshal(record.Payload, &decoded); err != nil {
		return ObservationRecord{}, fmt.Errorf("decode FRED normalized record: %w", err)
	}
	if decoded.RawObject.Key != record.RawObjectKey || decoded.RawObject.ContentSHA256 != record.RawObjectSHA256 {
		return ObservationRecord{}, errors.New("FRED normalized record raw provenance mismatch")
	}
	return decoded, nil
}
