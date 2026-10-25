package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/oplosy/atrisk/internal/quality"
	"github.com/oplosy/atrisk/internal/sources/binance"
	"github.com/oplosy/atrisk/internal/sources/fred"
	"github.com/oplosy/atrisk/internal/sources/tcmb"
)

// ErrCatalogChanged means a catalog entry differs from the row registered
// earlier. Registered identities are never rewritten from configuration.
var ErrCatalogChanged = errors.New("catalog entry differs from the registered row")

// CatalogConfig declares the sources, datasets and series that schedules
// collect into, so an installation needs no hand-written SQL.
type CatalogConfig struct {
	Sources []CatalogSource `json:"sources"`
}

type CatalogSource struct {
	Code     string           `json:"code"`
	Name     string           `json:"name"`
	Provider string           `json:"provider"`
	Datasets []CatalogDataset `json:"datasets"`
}

type CatalogDataset struct {
	ExternalKey string          `json:"external_key"`
	Name        string          `json:"name"`
	Series      []CatalogSeries `json:"series"`
}

type CatalogSeries struct {
	SourceCode      string          `json:"source_code"`
	Name            string          `json:"name"`
	Unit            string          `json:"unit"`
	Frequency       string          `json:"frequency"`
	SourceTimezone  string          `json:"source_timezone,omitempty"`
	FreshnessPolicy json.RawMessage `json:"freshness_policy"`
}

// CatalogIDs maps catalog codes to the registered row IDs.
type CatalogIDs struct {
	sources  map[string]string
	datasets map[string]string
	series   map[string]string
}

var providerAdapterVersions = map[string]string{
	"fred":    fred.AdapterVersion,
	"tcmb":    tcmb.AdapterVersion,
	"binance": binance.AdapterVersion,
}

func catalogKey(parts ...string) string { return strings.Join(parts, "\x00") }

func blank(value string) bool { return strings.TrimSpace(value) == "" }

// Validate checks the catalog without a database, including every series
// freshness policy, because a series without a usable policy cannot be healthy.
func (c CatalogConfig) Validate() error {
	seenSources := map[string]struct{}{}
	for _, source := range c.Sources {
		if blank(source.Code) || blank(source.Name) {
			return errors.New("catalog sources require code and name")
		}
		if _, ok := providerAdapterVersions[source.Provider]; !ok {
			return fmt.Errorf("catalog source %q has unsupported provider %q", source.Code, source.Provider)
		}
		if _, ok := seenSources[source.Code]; ok {
			return fmt.Errorf("catalog source %q is duplicated", source.Code)
		}
		seenSources[source.Code] = struct{}{}
		seenDatasets := map[string]struct{}{}
		for _, dataset := range source.Datasets {
			if blank(dataset.ExternalKey) || blank(dataset.Name) {
				return fmt.Errorf("catalog source %q datasets require external_key and name", source.Code)
			}
			if _, ok := seenDatasets[dataset.ExternalKey]; ok {
				return fmt.Errorf("catalog dataset %q/%q is duplicated", source.Code, dataset.ExternalKey)
			}
			seenDatasets[dataset.ExternalKey] = struct{}{}
			seenSeries := map[string]struct{}{}
			for _, series := range dataset.Series {
				if blank(series.SourceCode) || blank(series.Name) || blank(series.Unit) || blank(series.Frequency) {
					return fmt.Errorf("catalog dataset %q/%q series require source_code, name, unit and frequency", source.Code, dataset.ExternalKey)
				}
				if _, ok := seenSeries[series.SourceCode]; ok {
					return fmt.Errorf("catalog series %q/%q/%q is duplicated", source.Code, dataset.ExternalKey, series.SourceCode)
				}
				seenSeries[series.SourceCode] = struct{}{}
				if err := quality.ValidatePolicy(series.Frequency, series.SourceTimezone, series.FreshnessPolicy); err != nil {
					return fmt.Errorf("catalog series %q: %w", series.SourceCode, err)
				}
			}
		}
	}
	return nil
}

func (c CatalogConfig) provider(sourceCode string) (string, bool) {
	for _, source := range c.Sources {
		if source.Code == sourceCode {
			return source.Provider, true
		}
	}
	return "", false
}

func (c CatalogConfig) has(sourceCode, datasetKey, seriesCode string) bool {
	for _, source := range c.Sources {
		if source.Code != sourceCode {
			continue
		}
		if datasetKey == "" {
			return true
		}
		for _, dataset := range source.Datasets {
			if dataset.ExternalKey != datasetKey {
				continue
			}
			if seriesCode == "" {
				return true
			}
			for _, series := range dataset.Series {
				if series.SourceCode == seriesCode {
					return true
				}
			}
		}
	}
	return false
}

// Resolve replaces a schedule's catalog codes with registered IDs so the
// fingerprint matches an equivalent ID-based schedule.
func (ids CatalogIDs) Resolve(input ScheduleInput) (ScheduleInput, error) {
	if input.SourceCode == "" {
		return input, nil
	}
	sourceID, ok := ids.sources[input.SourceCode]
	if !ok {
		return ScheduleInput{}, fmt.Errorf("schedule %q references unregistered source %q", input.Name, input.SourceCode)
	}
	input.SourceID, input.SourceCode = sourceID, ""
	if input.DatasetKey != "" {
		datasetID, ok := ids.datasets[catalogKey(sourceID, input.DatasetKey)]
		if !ok {
			return ScheduleInput{}, fmt.Errorf("schedule %q references unregistered dataset %q", input.Name, input.DatasetKey)
		}
		input.DatasetID, input.DatasetKey = datasetID, ""
	}
	if input.SeriesCode != "" {
		seriesID, ok := ids.series[catalogKey(input.DatasetID, input.SeriesCode)]
		if !ok {
			return ScheduleInput{}, fmt.Errorf("schedule %q references unregistered series %q", input.Name, input.SeriesCode)
		}
		input.SeriesID, input.SeriesCode = seriesID, ""
	}
	return input, nil
}

// RegisterCatalog inserts missing catalog rows in one transaction and returns
// their IDs. An existing row whose declared fields differ fails closed.
func (s Store) RegisterCatalog(ctx context.Context, catalog CatalogConfig) (CatalogIDs, error) {
	ids := CatalogIDs{sources: map[string]string{}, datasets: map[string]string{}, series: map[string]string{}}
	if s.Pool == nil {
		return ids, errors.New("collector database pool is required")
	}
	if len(catalog.Sources) == 0 {
		return ids, nil
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return ids, fmt.Errorf("begin catalog registration: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, source := range catalog.Sources {
		sourceID, err := registerRow(ctx, tx, "source "+source.Code,
			`INSERT INTO data_sources (code, name, adapter_version) VALUES ($1, $2, $3) ON CONFLICT (code) DO NOTHING`,
			`SELECT id::text, name = $2 FROM data_sources WHERE code = $1`,
			[]any{source.Code, source.Name, providerAdapterVersions[source.Provider]}, 2)
		if err != nil {
			return ids, err
		}
		ids.sources[source.Code] = sourceID
		for _, dataset := range source.Datasets {
			datasetID, err := registerRow(ctx, tx, "dataset "+dataset.ExternalKey,
				`INSERT INTO datasets (source_id, external_key, name) VALUES ($1::uuid, $2, $3) ON CONFLICT (source_id, external_key) DO NOTHING`,
				`SELECT id::text, name = $3 FROM datasets WHERE source_id = $1::uuid AND external_key = $2`,
				[]any{sourceID, dataset.ExternalKey, dataset.Name}, 3)
			if err != nil {
				return ids, err
			}
			ids.datasets[catalogKey(sourceID, dataset.ExternalKey)] = datasetID
			for _, series := range dataset.Series {
				var timezone any
				if series.SourceTimezone != "" {
					timezone = series.SourceTimezone
				}
				seriesID, err := registerRow(ctx, tx, "series "+series.SourceCode,
					`INSERT INTO series (dataset_id, source_code, name, unit, frequency, source_timezone, freshness_policy)
VALUES ($1::uuid, $2, $3, $4, $5, $6, $7::jsonb) ON CONFLICT (dataset_id, source_code) DO NOTHING`,
					`SELECT id::text, name = $3 AND unit = $4 AND frequency = $5 AND source_timezone IS NOT DISTINCT FROM $6::text AND freshness_policy = $7::jsonb
FROM series WHERE dataset_id = $1::uuid AND source_code = $2`,
					[]any{datasetID, series.SourceCode, series.Name, series.Unit, series.Frequency, timezone, string(series.FreshnessPolicy)}, 7)
				if err != nil {
					return ids, err
				}
				ids.series[catalogKey(datasetID, series.SourceCode)] = seriesID
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return ids, fmt.Errorf("commit catalog registration: %w", err)
	}
	return ids, nil
}

// registerRow runs an idempotent insert, then loads the row and whether its
// declared fields match. The select uses the first selectArgs insert arguments.
func registerRow(ctx context.Context, tx pgx.Tx, label, insert, selectQuery string, args []any, selectArgs int) (string, error) {
	if _, err := tx.Exec(ctx, insert, args...); err != nil {
		return "", fmt.Errorf("register catalog %s: %w", label, err)
	}
	var id string
	var matches bool
	if err := tx.QueryRow(ctx, selectQuery, args[:selectArgs]...).Scan(&id, &matches); err != nil {
		return "", fmt.Errorf("load catalog %s: %w", label, err)
	}
	if !matches {
		return "", fmt.Errorf("%w: %s", ErrCatalogChanged, label)
	}
	return id, nil
}
