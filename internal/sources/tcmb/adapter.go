package tcmb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/ingestion"
)

const ObservationRecordKind = "tcmb_observation"

var decimalPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

type Adapter struct {
	Client  *Client
	Request SeriesRequest
}

func NewAdapter(client *Client, request SeriesRequest) (*Adapter, error) {
	if client == nil {
		return nil, errors.New("TCMB EVDS client is required")
	}
	if _, err := request.normalized(client.pageSize); err != nil {
		return nil, err
	}
	return &Adapter{Client: client, Request: request}, nil
}

func (a *Adapter) BuildRequest(_ context.Context, _ ingestion.RunSpec) (ingestion.FetchRequest, error) {
	if a == nil || a.Client == nil {
		return ingestion.FetchRequest{}, errors.New("TCMB EVDS adapter client is required")
	}
	return a.Client.buildRequest(a.Request)
}

type ObservationRecord struct {
	SeriesCode         string            `json:"series_code"`
	ObservationTime    time.Time         `json:"observation_time"`
	Value              *string           `json:"value,omitempty"`
	ValueText          *string           `json:"value_text,omitempty"`
	SourceKnownAt      *time.Time        `json:"source_known_at,omitempty"`
	KnowledgeTimeBasis string            `json:"knowledge_time_basis"`
	Frequency          string            `json:"frequency,omitempty"`
	AggregationTypes   string            `json:"aggregation_types,omitempty"`
	Formulas           string            `json:"formulas,omitempty"`
	DecimalSeparator   string            `json:"decimal_separator"`
	QualityFlags       map[string]any    `json:"quality_flags"`
	RawObject          archive.Reference `json:"raw_object"`
}

func (a *Adapter) Normalize(_ context.Context, payload ingestion.RawPayload) ([]ingestion.NormalizedRecord, error) {
	if a == nil {
		return nil, errors.New("TCMB EVDS adapter is required")
	}
	if payload.MediaType != "" && payload.MediaType != "application/json" {
		return nil, fmt.Errorf("TCMB EVDS response media type must be application/json, got %q", payload.MediaType)
	}
	response, err := decodeResponse(payload.Body)
	if err != nil {
		return nil, err
	}
	request, err := a.Request.normalized(a.Client.pageSize)
	if err != nil {
		return nil, err
	}
	requestedEnd, _ := parseRequestDate(request.EndDate)
	latestObservation := time.Time{}
	for _, item := range response.Items {
		if observationTime, parseErr := observationDate(item); parseErr == nil && observationTime.After(latestObservation) {
			latestObservation = observationTime
		}
	}
	late := !latestObservation.IsZero() && latestObservation.Before(requestedEnd)
	records := make([]ingestion.NormalizedRecord, 0, len(response.Items)*len(request.Series))
	for index, item := range response.Items {
		observationTime, err := observationDate(item)
		if err != nil {
			return nil, fmt.Errorf("normalize TCMB EVDS item %d: %w", index, err)
		}
		for _, seriesCode := range request.Series {
			rawValue, present := seriesValue(item, seriesCode)
			record, err := normalizeObservation(seriesCode, observationTime, rawValue, present, request, payload.Archive)
			if err != nil {
				return nil, fmt.Errorf("normalize TCMB EVDS item %d series %s: %w", index, seriesCode, err)
			}
			if late {
				record.QualityFlags["late"] = true
				record.QualityFlags["late_basis"] = "latest_observation_before_requested_end"
				record.QualityFlags["requested_end"] = requestedEnd.Format("2006-01-02")
				record.QualityFlags["latest_observation"] = latestObservation.Format("2006-01-02")
			}
			encoded, err := json.Marshal(record)
			if err != nil {
				return nil, fmt.Errorf("encode TCMB EVDS observation %d: %w", index, err)
			}
			records = append(records, ingestion.NormalizedRecord{Kind: ObservationRecordKind, RawObjectKey: payload.Archive.Key, RawObjectSHA256: payload.Archive.ContentSHA256, Payload: encoded})
		}
	}
	return records, nil
}

func observationDate(item map[string]json.RawMessage) (time.Time, error) {
	var value string
	if err := json.Unmarshal(item["Tarih"], &value); err != nil {
		return time.Time{}, errors.New("EVDS item has no valid Tarih")
	}
	return parseEVDSDate(value)
}

func seriesValue(item map[string]json.RawMessage, seriesCode string) (string, bool) {
	keys := []string{seriesCode, strings.ReplaceAll(seriesCode, ".", "_")}
	for _, key := range keys {
		raw, ok := item[key]
		if !ok {
			continue
		}
		if string(raw) == "null" {
			return "", true
		}
		var value string
		if err := json.Unmarshal(raw, &value); err == nil {
			return value, true
		}
		return string(raw), true
	}
	return "", false
}

func normalizeObservation(seriesCode string, observationTime time.Time, rawValue string, present bool, request SeriesRequest, raw archive.Reference) (ObservationRecord, error) {
	flags := map[string]any{
		"source_publication_time":        "unavailable_in_evds2_response",
		"retrieval_time_not_publication": true,
		"frequency":                      request.Frequency,
		"aggregation_types":              request.AggregationTypes,
		"decimal_separator":              request.DecimalSeparator,
	}
	record := ObservationRecord{SeriesCode: seriesCode, ObservationTime: observationTime, KnowledgeTimeBasis: "first_observed_by_system", Frequency: request.Frequency, AggregationTypes: request.AggregationTypes, Formulas: request.Formulas, DecimalSeparator: request.DecimalSeparator, QualityFlags: flags, RawObject: raw}
	if !present || strings.TrimSpace(rawValue) == "" || strings.EqualFold(strings.TrimSpace(rawValue), "null") || strings.TrimSpace(rawValue) == "-" {
		marker := strings.TrimSpace(rawValue)
		if marker == "" {
			marker = "null"
		}
		record.ValueText = &marker
		flags["missing"] = true
		flags["missing_marker"] = marker
		flags["quality"] = "missing_expected_period"
		return record, nil
	}
	normalized, err := normalizeDecimal(rawValue, request.DecimalSeparator)
	if err != nil {
		return ObservationRecord{}, err
	}
	record.Value = &normalized
	return record, nil
}

func normalizeDecimal(value, separator string) (string, error) {
	value = strings.TrimSpace(value)
	if separator == "," {
		if strings.Count(value, ",") != 1 || strings.Contains(value, ".") {
			return "", fmt.Errorf("invalid numeric value %q for comma decimal separator", value)
		}
		value = strings.Replace(value, ",", ".", 1)
	} else if strings.Contains(value, ",") {
		return "", fmt.Errorf("invalid numeric value %q for period decimal separator", value)
	}
	if !decimalPattern.MatchString(value) {
		return "", fmt.Errorf("invalid numeric value %q", value)
	}
	return value, nil
}

func DecodeObservationRecord(record ingestion.NormalizedRecord) (ObservationRecord, error) {
	if record.Kind != ObservationRecordKind {
		return ObservationRecord{}, fmt.Errorf("unexpected TCMB EVDS record kind %q", record.Kind)
	}
	var decoded ObservationRecord
	if err := json.Unmarshal(record.Payload, &decoded); err != nil {
		return ObservationRecord{}, fmt.Errorf("decode TCMB EVDS normalized record: %w", err)
	}
	if decoded.RawObject.Key != record.RawObjectKey || decoded.RawObject.ContentSHA256 != record.RawObjectSHA256 {
		return ObservationRecord{}, errors.New("TCMB EVDS normalized record raw provenance mismatch")
	}
	return decoded, nil
}
