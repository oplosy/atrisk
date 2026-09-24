package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/oplosy/atrisk/internal/archive"
	"github.com/oplosy/atrisk/internal/ingestion"
)

const (
	InstrumentRecordKind = "binance_instrument"
	PriceRecordKind      = "binance_daily_price"
	firstObservedBasis   = "first_observed_by_system"
)

var decimalPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

type Adapter struct {
	Client           *Client
	RequestedSymbols []string
	Kline            KlineRequest
}

func NewAdapter(client *Client, requestedSymbols []string, kline KlineRequest) (*Adapter, error) {
	if client == nil {
		return nil, errors.New("Binance client is required")
	}
	seen := make(map[string]struct{}, len(requestedSymbols))
	normalized := make([]string, 0, len(requestedSymbols))
	for _, symbol := range requestedSymbols {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if !validSymbol(symbol) {
			return nil, fmt.Errorf("invalid Binance requested symbol %q", symbol)
		}
		if _, exists := seen[symbol]; !exists {
			seen[symbol] = struct{}{}
			normalized = append(normalized, symbol)
		}
	}
	kline.Symbol = strings.ToUpper(strings.TrimSpace(kline.Symbol))
	if kline.Symbol != "" {
		if _, err := kline.normalized(client.pageSize); err != nil {
			return nil, err
		}
	}
	return &Adapter{Client: client, RequestedSymbols: normalized, Kline: kline}, nil
}

func (a *Adapter) BuildRequest(context.Context, ingestion.RunSpec) (ingestion.FetchRequest, error) {
	if a == nil || a.Client == nil {
		return ingestion.FetchRequest{}, errors.New("Binance adapter client is required")
	}
	if a.Kline.Symbol != "" {
		return a.Client.BuildKlineRequest(a.Kline)
	}
	return a.Client.BuildExchangeInfoRequest()
}

type ExchangeInfoResponse struct {
	Symbols []ExchangeSymbol `json:"symbols"`
}

type ExchangeSymbol struct {
	Symbol               string   `json:"symbol"`
	Status               string   `json:"status"`
	BaseAsset            string   `json:"baseAsset"`
	QuoteAsset           string   `json:"quoteAsset"`
	Permissions          []string `json:"permissions"`
	IsSpotTradingAllowed bool     `json:"isSpotTradingAllowed"`
}

type InstrumentRecord struct {
	Symbol             string            `json:"symbol"`
	BaseAsset          string            `json:"base_asset,omitempty"`
	QuoteAsset         string            `json:"quote_asset,omitempty"`
	UpstreamStatus     string            `json:"upstream_status"`
	LifecycleStatus    string            `json:"lifecycle_status"`
	RequestedSymbol    bool              `json:"requested_symbol"`
	MissingFromCatalog bool              `json:"missing_from_catalog"`
	KnowledgeTimeBasis string            `json:"knowledge_time_basis"`
	QualityFlags       map[string]any    `json:"quality_flags"`
	RawObject          archive.Reference `json:"raw_object"`
}

func (a *Adapter) Normalize(ctx context.Context, payload ingestion.RawPayload) ([]ingestion.NormalizedRecord, error) {
	if a == nil {
		return nil, errors.New("Binance adapter is required")
	}
	if payload.MediaType != "" && payload.MediaType != "application/json" {
		return nil, fmt.Errorf("Binance response media type must be application/json, got %q", payload.MediaType)
	}
	var envelope struct {
		Symbols json.RawMessage `json:"symbols"`
	}
	if err := json.Unmarshal(payload.Body, &envelope); err == nil && envelope.Symbols != nil {
		return a.normalizeExchangeInfo(payload, envelope.Symbols)
	}
	if a.Kline.Symbol == "" {
		return nil, errors.New("Binance kline symbol is required to normalize a kline response")
	}
	return a.normalizeKlines(ctx, a.Kline.Symbol, payload)
}

func (a *Adapter) normalizeExchangeInfo(payload ingestion.RawPayload, symbolsJSON []byte) ([]ingestion.NormalizedRecord, error) {
	var symbols []ExchangeSymbol
	if err := json.Unmarshal(symbolsJSON, &symbols); err != nil {
		return nil, fmt.Errorf("decode Binance exchange symbols: %w", err)
	}
	requested := make(map[string]struct{}, len(a.RequestedSymbols))
	for _, symbol := range a.RequestedSymbols {
		requested[symbol] = struct{}{}
	}
	records := make([]ingestion.NormalizedRecord, 0, len(symbols)+len(requested))
	seen := make(map[string]struct{}, len(symbols))
	for index, symbol := range symbols {
		normalized, err := normalizeExchangeSymbol(symbol, payload.Archive, requested)
		if err != nil {
			return nil, fmt.Errorf("normalize Binance symbol %d: %w", index, err)
		}
		seen[normalized.Symbol] = struct{}{}
		records = append(records, encodeInstrumentRecord(normalized, payload.Archive))
	}
	for _, symbol := range a.RequestedSymbols {
		if _, exists := seen[symbol]; exists {
			continue
		}
		record := InstrumentRecord{Symbol: symbol, RequestedSymbol: true, MissingFromCatalog: true, UpstreamStatus: "missing", LifecycleStatus: "unknown", KnowledgeTimeBasis: firstObservedBasis, QualityFlags: map[string]any{"requested_symbol_missing": true}, RawObject: payload.Archive}
		records = append(records, encodeInstrumentRecord(record, payload.Archive))
	}
	return records, nil
}

func normalizeExchangeSymbol(symbol ExchangeSymbol, raw archive.Reference, requested map[string]struct{}) (InstrumentRecord, error) {
	symbol.Symbol = strings.ToUpper(strings.TrimSpace(symbol.Symbol))
	symbol.BaseAsset = strings.ToUpper(strings.TrimSpace(symbol.BaseAsset))
	symbol.QuoteAsset = strings.ToUpper(strings.TrimSpace(symbol.QuoteAsset))
	symbol.Status = strings.ToUpper(strings.TrimSpace(symbol.Status))
	if !validSymbol(symbol.Symbol) || !validSymbol(symbol.BaseAsset) || !validSymbol(symbol.QuoteAsset) || symbol.Status == "" {
		return InstrumentRecord{}, errors.New("exchange symbol has invalid identity or status")
	}
	lifecycle := "inactive"
	if symbol.Status == "TRADING" {
		lifecycle = "active"
	}
	flags := map[string]any{}
	if symbol.Status != "TRADING" {
		flags["upstream_status_changed"] = true
	}
	_, isRequested := requested[symbol.Symbol]
	return InstrumentRecord{Symbol: symbol.Symbol, BaseAsset: symbol.BaseAsset, QuoteAsset: symbol.QuoteAsset, UpstreamStatus: symbol.Status, LifecycleStatus: lifecycle, RequestedSymbol: isRequested, KnowledgeTimeBasis: firstObservedBasis, QualityFlags: flags, RawObject: raw}, nil
}

type Kline struct {
	OpenTime       time.Time
	Open           string
	High           string
	Low            string
	Close          string
	Volume         string
	CloseTime      time.Time
	NumberOfTrades int64
}

type KlinePage struct {
	Request KlineRequest
	Klines  []Kline
	Fetched ingestion.FetchedResponse
}

type PriceRecord struct {
	Symbol             string            `json:"symbol"`
	BaseAsset          string            `json:"base_asset"`
	QuoteAsset         string            `json:"quote_asset"`
	ObservationTime    time.Time         `json:"observation_time"`
	Price              string            `json:"price"`
	SourceKnownAt      *time.Time        `json:"source_known_at,omitempty"`
	KnowledgeTimeBasis string            `json:"knowledge_time_basis"`
	QualityFlags       map[string]any    `json:"quality_flags"`
	RawObject          archive.Reference `json:"raw_object"`
}

func (a *Adapter) NormalizeKlines(ctx context.Context, symbol string, payload ingestion.RawPayload) ([]ingestion.NormalizedRecord, error) {
	if a == nil {
		return nil, errors.New("Binance adapter is required")
	}
	return a.normalizeKlines(ctx, symbol, payload)
}

func (a *Adapter) normalizeKlines(_ context.Context, symbol string, payload ingestion.RawPayload) ([]ingestion.NormalizedRecord, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if !validSymbol(symbol) {
		return nil, errors.New("Binance kline symbol is required")
	}
	klines, err := decodeKlines(payload.Body)
	if err != nil {
		return nil, fmt.Errorf("decode Binance klines: %w", err)
	}
	result := make([]ingestion.NormalizedRecord, 0, len(klines))
	now := a.Client.nowUTC()
	for index, kline := range klines {
		if !kline.OpenTime.Before(kline.CloseTime) {
			return nil, fmt.Errorf("normalize Binance kline %d: close time precedes open time", index)
		}
		if !kline.CloseTime.Before(now) {
			continue
		}
		if kline.OpenTime.Hour() != 0 || kline.OpenTime.Minute() != 0 || kline.OpenTime.Second() != 0 || kline.OpenTime.Nanosecond() != 0 {
			return nil, fmt.Errorf("normalize Binance kline %d: daily candle is not UTC period-aligned", index)
		}
		if !decimalPattern.MatchString(kline.Close) || !decimalPattern.MatchString(kline.Open) || !decimalPattern.MatchString(kline.High) || !decimalPattern.MatchString(kline.Low) {
			return nil, fmt.Errorf("normalize Binance kline %d: non-decimal price field", index)
		}
		record := PriceRecord{Symbol: symbol, ObservationTime: kline.OpenTime.UTC(), Price: kline.Close, KnowledgeTimeBasis: firstObservedBasis, QualityFlags: map[string]any{"daily_candle": true, "source_publication_time_unknown": true}, RawObject: payload.Archive}
		encoded, err := json.Marshal(record)
		if err != nil {
			return nil, fmt.Errorf("encode Binance kline %d: %w", index, err)
		}
		result = append(result, ingestion.NormalizedRecord{Kind: PriceRecordKind, RawObjectKey: payload.Archive.Key, RawObjectSHA256: payload.Archive.ContentSHA256, Payload: encoded})
	}
	return result, nil
}

func encodeInstrumentRecord(record InstrumentRecord, raw archive.Reference) ingestion.NormalizedRecord {
	encoded, _ := json.Marshal(record)
	return ingestion.NormalizedRecord{Kind: InstrumentRecordKind, RawObjectKey: raw.Key, RawObjectSHA256: raw.ContentSHA256, Payload: encoded}
}

func DecodeInstrumentRecord(record ingestion.NormalizedRecord) (InstrumentRecord, error) {
	if record.Kind != InstrumentRecordKind {
		return InstrumentRecord{}, fmt.Errorf("unexpected Binance instrument record kind %q", record.Kind)
	}
	var decoded InstrumentRecord
	if err := json.Unmarshal(record.Payload, &decoded); err != nil {
		return InstrumentRecord{}, fmt.Errorf("decode Binance instrument record: %w", err)
	}
	if decoded.RawObject.Key != record.RawObjectKey || decoded.RawObject.ContentSHA256 != record.RawObjectSHA256 {
		return InstrumentRecord{}, errors.New("Binance instrument provenance mismatch")
	}
	return decoded, nil
}

func DecodePriceRecord(record ingestion.NormalizedRecord) (PriceRecord, error) {
	if record.Kind != PriceRecordKind {
		return PriceRecord{}, fmt.Errorf("unexpected Binance price record kind %q", record.Kind)
	}
	var decoded PriceRecord
	if err := json.Unmarshal(record.Payload, &decoded); err != nil {
		return PriceRecord{}, fmt.Errorf("decode Binance price record: %w", err)
	}
	if decoded.RawObject.Key != record.RawObjectKey || decoded.RawObject.ContentSHA256 != record.RawObjectSHA256 {
		return PriceRecord{}, errors.New("Binance price provenance mismatch")
	}
	if !decimalPattern.MatchString(decoded.Price) || decoded.SourceKnownAt != nil || decoded.KnowledgeTimeBasis != firstObservedBasis {
		return PriceRecord{}, errors.New("Binance price has invalid exact decimal or source clock")
	}
	return decoded, nil
}

func decodeKlines(data []byte) ([]Kline, error) {
	var rows [][]json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, err
	}
	result := make([]Kline, 0, len(rows))
	for index, row := range rows {
		if len(row) < 9 {
			return nil, fmt.Errorf("row %d has %d fields, want at least 9", index, len(row))
		}
		openMillis, err := rawInt64(row[0])
		if err != nil {
			return nil, fmt.Errorf("row %d open time: %w", index, err)
		}
		closeMillis, err := rawInt64(row[6])
		if err != nil {
			return nil, fmt.Errorf("row %d close time: %w", index, err)
		}
		open, close := time.UnixMilli(openMillis).UTC(), time.UnixMilli(closeMillis).UTC()
		values := make([]string, 5)
		for field, raw := range row[1:6] {
			if err := json.Unmarshal(raw, &values[field]); err != nil || !decimalPattern.MatchString(values[field]) {
				return nil, fmt.Errorf("row %d decimal field %d is invalid", index, field+1)
			}
		}
		trades, err := rawInt64(row[8])
		if err != nil {
			return nil, fmt.Errorf("row %d trade count: %w", index, err)
		}
		result = append(result, Kline{OpenTime: open, Open: values[0], High: values[1], Low: values[2], Close: values[3], Volume: values[4], CloseTime: close, NumberOfTrades: trades})
	}
	return result, nil
}

func rawInt64(raw json.RawMessage) (int64, error) {
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return 0, err
	}
	return strconv.ParseInt(string(number), 10, 64)
}
