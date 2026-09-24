// Package timeline exposes explicit point-in-time projections over immutable revisions.
package timeline

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oplosy/atrisk/internal/platform/database"
)

type Mode string

const (
	ModeLatest     Mode = "latest"
	ModeSourceAsOf Mode = "source-as-of"
	ModeSystemAsOf Mode = "system-as-of"
	ModeRevisions  Mode = "revisions"
	ModeCombined   Mode = "combined"
)

var (
	ErrNotFound               = errors.New("timeline resource not found")
	ErrInvalidCursor          = errors.New("invalid timeline cursor")
	ErrInvalidWindow          = errors.New("invalid timeline observation window")
	ErrInvalidLimit           = errors.New("invalid timeline limit")
	ErrInvalidMode            = errors.New("invalid timeline mode")
	ErrInvalidSeriesID        = errors.New("invalid timeline series id")
	ErrInvalidSeriesSelection = errors.New("invalid timeline series selection")
	ErrSourceAsOfUnsupported  = errors.New("source-as-of is unsupported for this source")
)

type Service struct{ Queries *database.Queries }

type Page[T any] struct {
	Items      []T    `json:"items"`
	Limit      int    `json:"limit"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

type Series struct {
	ID                 string          `json:"id"`
	DatasetID          string          `json:"dataset_id"`
	SourceCode         string          `json:"source_code"`
	SourceName         string          `json:"source_name"`
	DatasetExternalKey string          `json:"dataset_external_key"`
	Name               string          `json:"name"`
	Unit               string          `json:"unit"`
	Frequency          string          `json:"frequency"`
	SeasonalAdjustment *string         `json:"seasonal_adjustment,omitempty"`
	SourceTimezone     *string         `json:"source_timezone,omitempty"`
	FreshnessPolicy    json.RawMessage `json:"freshness_policy"`
	Capabilities       Capabilities    `json:"capabilities"`
}

type Capabilities struct {
	Latest     bool `json:"latest"`
	SourceAsOf bool `json:"source_as_of"`
	SystemAsOf bool `json:"system_as_of"`
	Revisions  bool `json:"revisions"`
}

type Clocks struct {
	SourceKnownAt      *time.Time `json:"source_known_at,omitempty"`
	SystemKnownAt      time.Time  `json:"system_known_at"`
	KnowledgeTimeBasis string     `json:"knowledge_time_basis"`
}

type Observation struct {
	ID              string          `json:"id"`
	SeriesID        string          `json:"series_id"`
	ObservationTime time.Time       `json:"observation_time"`
	Value           *string         `json:"value,omitempty"`
	ValueText       *string         `json:"value_text,omitempty"`
	Unit            string          `json:"unit"`
	Frequency       string          `json:"frequency"`
	DataSourceCode  string          `json:"data_source_code"`
	Clocks          Clocks          `json:"clocks"`
	Quality         json.RawMessage `json:"quality"`
	RawProvenanceID string          `json:"raw_provenance_id"`
	RawObjectSHA256 string          `json:"raw_object_sha256"`
}

type ObservationRequest struct {
	Mode   Mode
	From   time.Time
	To     time.Time
	AsOf   time.Time
	Limit  int
	Cursor string
}

type observationCursor struct {
	ObservationTime time.Time `json:"observation_time"`
	SystemKnownAt   time.Time `json:"system_known_at,omitempty"`
	SeriesID        string    `json:"series_id,omitempty"`
	ID              string    `json:"id"`
}

type seriesCursor struct {
	DataSourceCode string `json:"data_source_code"`
	SourceCode     string `json:"source_code"`
	ID             string `json:"id"`
}

func (s Service) ListSeries(ctx context.Context, limit int, cursor string) (Page[Series], error) {
	if err := validateLimit(limit); err != nil {
		return Page[Series]{}, err
	}
	pageCursor, err := decodeSeriesCursor(cursor)
	if err != nil {
		return Page[Series]{}, err
	}
	if s.Queries == nil {
		return Page[Series]{}, errors.New("timeline database is required")
	}
	cursorID := pgtype.UUID{}
	if pageCursor.ID != "" {
		cursorID, err = parseUUID(pageCursor.ID)
		if err != nil {
			return Page[Series]{}, ErrInvalidCursor
		}
	}
	rows, err := s.Queries.ListTimelineSeries(ctx, database.ListTimelineSeriesParams{Column1: cursor != "", Code: pageCursor.DataSourceCode, SourceCode: pageCursor.SourceCode, Column4: cursorID, Limit: int32(limit + 1)})
	if err != nil {
		return Page[Series]{}, fmt.Errorf("list timeline series: %w", err)
	}
	page := Page[Series]{Limit: limit}
	for _, row := range rows {
		page.Items = append(page.Items, seriesFromListRow(row))
	}
	page.HasMore = len(page.Items) > limit
	if page.HasMore {
		page.Items = page.Items[:limit]
		last := rows[limit-1]
		page.NextCursor = encodeSeriesCursor(seriesCursor{DataSourceCode: last.DataSourceCode, SourceCode: last.SourceCode, ID: last.ID.String()})
	}
	if page.Items == nil {
		page.Items = []Series{}
	}
	return page, nil
}

func (s Service) GetSeries(ctx context.Context, rawID string) (Series, error) {
	id, err := parseUUID(rawID)
	if err != nil {
		return Series{}, err
	}
	if s.Queries == nil {
		return Series{}, errors.New("timeline database is required")
	}
	row, err := s.Queries.GetTimelineSeries(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Series{}, ErrNotFound
	}
	if err != nil {
		return Series{}, fmt.Errorf("get timeline series: %w", err)
	}
	return seriesFromRow(row), nil
}

func (s Service) Observations(ctx context.Context, rawID string, request ObservationRequest) (Page[Observation], error) {
	if err := validateLimit(request.Limit); err != nil {
		return Page[Observation]{}, err
	}
	if request.Mode != "" && request.Mode != ModeLatest && request.Mode != ModeSourceAsOf && request.Mode != ModeSystemAsOf && request.Mode != ModeRevisions {
		return Page[Observation]{}, ErrInvalidMode
	}
	seriesID, err := parseUUID(rawID)
	if err != nil {
		return Page[Observation]{}, err
	}
	if s.Queries == nil {
		return Page[Observation]{}, errors.New("timeline database is required")
	}
	series, err := s.Queries.GetTimelineSeries(ctx, seriesID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page[Observation]{}, ErrNotFound
	}
	if err != nil {
		return Page[Observation]{}, fmt.Errorf("get timeline series: %w", err)
	}
	if request.Mode == "" {
		request.Mode = ModeLatest
	}
	if request.Mode == ModeSourceAsOf && !series.SourceAsOfSupported {
		return Page[Observation]{}, ErrSourceAsOfUnsupported
	}
	if request.From.IsZero() {
		request.From = time.Unix(0, 0).UTC()
	}
	if request.To.IsZero() {
		request.To = time.Now().UTC().Add(24 * time.Hour)
	}
	if !request.From.Before(request.To) {
		return Page[Observation]{}, ErrInvalidWindow
	}
	if request.Mode != ModeLatest && request.Mode != ModeRevisions && request.Mode != ModeCombined && request.AsOf.IsZero() {
		return Page[Observation]{}, ErrInvalidWindow
	}
	limit := request.Limit
	rowsLimit := int32(limit + 1)
	cursor, err := decodeObservationCursor(request.Cursor)
	if err != nil {
		return Page[Observation]{}, err
	}
	cursorID := pgtype.UUID{}
	if cursor.ID != "" {
		cursorID, err = parseUUID(cursor.ID)
		if err != nil {
			return Page[Observation]{}, ErrInvalidCursor
		}
	}
	var items []Observation
	switch request.Mode {
	case ModeLatest:
		rows, queryErr := s.Queries.ListTimelineObservationsLatest(ctx, database.ListTimelineObservationsLatestParams{SeriesID: seriesID, ObservationTime: timestamptz(request.From), ObservationTime_2: timestamptz(request.To), Limit: rowsLimit, Column5: cursor.ID != "", ObservationTime_3: timestamptz(cursor.ObservationTime), Column7: cursorID})
		if queryErr != nil {
			return Page[Observation]{}, fmt.Errorf("list latest timeline observations: %w", queryErr)
		}
		items = observationsFromLatest(rows)
	case ModeSystemAsOf:
		rows, queryErr := s.Queries.ListTimelineObservationsSystemAsOf(ctx, database.ListTimelineObservationsSystemAsOfParams{SeriesID: seriesID, ObservationTime: timestamptz(request.From), ObservationTime_2: timestamptz(request.To), Column4: timestamptz(request.AsOf), Limit: rowsLimit, Column6: cursor.ID != "", ObservationTime_3: timestamptz(cursor.ObservationTime), Column8: cursorID})
		if queryErr != nil {
			return Page[Observation]{}, fmt.Errorf("list system-as-of observations: %w", queryErr)
		}
		items = observationsFromSystem(rows)
	case ModeSourceAsOf:
		rows, queryErr := s.Queries.ListTimelineObservationsSourceAsOf(ctx, database.ListTimelineObservationsSourceAsOfParams{SeriesID: seriesID, ObservationTime: timestamptz(request.From), ObservationTime_2: timestamptz(request.To), Column4: timestamptz(request.AsOf), Limit: rowsLimit, Column6: cursor.ID != "", ObservationTime_3: timestamptz(cursor.ObservationTime), Column8: cursorID})
		if queryErr != nil {
			return Page[Observation]{}, fmt.Errorf("list source-as-of observations: %w", queryErr)
		}
		items = observationsFromSource(rows)
	case ModeRevisions:
		rows, queryErr := s.Queries.ListTimelineObservationRevisions(ctx, database.ListTimelineObservationRevisionsParams{SeriesID: seriesID, ObservationTime: timestamptz(request.From), ObservationTime_2: timestamptz(request.To), Limit: rowsLimit, Column5: cursor.ID != "", ObservationTime_3: timestamptz(cursor.ObservationTime), SystemKnownAt: timestamptz(cursor.SystemKnownAt), Column8: cursorID})
		if queryErr != nil {
			return Page[Observation]{}, fmt.Errorf("list timeline revisions: %w", queryErr)
		}
		items = observationsFromRevisions(rows)
	case ModeCombined:
		return Page[Observation]{}, ErrInvalidMode
	default:
		return Page[Observation]{}, ErrInvalidMode
	}
	page := Page[Observation]{Limit: limit, Items: items}
	page.HasMore = len(page.Items) > limit
	if page.HasMore {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeObservationCursor(observationCursor{ObservationTime: last.ObservationTime, SystemKnownAt: last.Clocks.SystemKnownAt, ID: last.ID})
	}
	if page.Items == nil {
		page.Items = []Observation{}
	}
	return page, nil
}

// CrossSource merges explicitly selected series without falling back between
// clocks. Each source keeps its own provenance and quality fields.
func (s Service) CrossSource(ctx context.Context, ids []string, request ObservationRequest) (Page[Observation], error) {
	if len(ids) == 0 {
		return Page[Observation]{Items: []Observation{}, Limit: request.Limit}, ErrInvalidSeriesSelection
	}
	if err := validateLimit(request.Limit); err != nil {
		return Page[Observation]{}, err
	}
	if request.Mode == "" || request.Mode == ModeCombined {
		request.Mode = ModeLatest
	}
	if request.Mode != ModeLatest && request.Mode != ModeSourceAsOf && request.Mode != ModeSystemAsOf {
		return Page[Observation]{}, ErrInvalidMode
	}
	if request.From.IsZero() {
		request.From = time.Unix(0, 0).UTC()
	}
	if request.To.IsZero() {
		request.To = time.Now().UTC().Add(24 * time.Hour)
	}
	if !request.From.Before(request.To) {
		return Page[Observation]{}, ErrInvalidWindow
	}
	if request.Mode != ModeLatest && request.AsOf.IsZero() {
		return Page[Observation]{}, ErrInvalidWindow
	}
	seriesIDs := make([]pgtype.UUID, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		parsed, err := parseUUID(id)
		if err != nil {
			return Page[Observation]{}, err
		}
		key := parsed.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		seriesIDs = append(seriesIDs, parsed)
	}
	if s.Queries == nil {
		return Page[Observation]{}, errors.New("timeline database is required")
	}
	for _, parsed := range seriesIDs {
		series, err := s.Queries.GetTimelineSeries(ctx, parsed)
		if errors.Is(err, pgx.ErrNoRows) {
			return Page[Observation]{}, ErrNotFound
		}
		if err != nil {
			return Page[Observation]{}, fmt.Errorf("get combined timeline series: %w", err)
		}
		if request.Mode == ModeSourceAsOf && !series.SourceAsOfSupported {
			return Page[Observation]{}, ErrSourceAsOfUnsupported
		}
	}
	limit := request.Limit
	rowsLimit := int32(limit + 1)
	cursor, err := decodeObservationCursor(request.Cursor)
	if err != nil {
		return Page[Observation]{}, err
	}
	cursorID := pgtype.UUID{}
	cursorSeriesID := pgtype.UUID{}
	if cursor.ID != "" {
		cursorID, err = parseUUID(cursor.ID)
		if err != nil {
			return Page[Observation]{}, ErrInvalidCursor
		}
	}
	if cursor.SeriesID != "" {
		cursorSeriesID, err = parseUUID(cursor.SeriesID)
		if err != nil {
			return Page[Observation]{}, ErrInvalidCursor
		}
	}
	var items []Observation
	switch request.Mode {
	case ModeLatest:
		rows, queryErr := s.Queries.ListTimelineObservationsCombinedLatest(ctx, database.ListTimelineObservationsCombinedLatestParams{Column1: seriesIDs, ObservationTime: timestamptz(request.From), ObservationTime_2: timestamptz(request.To), Limit: rowsLimit, Column5: cursor.ID != "", ObservationTime_3: timestamptz(cursor.ObservationTime), Column7: cursorSeriesID, Column8: cursorID})
		if queryErr != nil {
			return Page[Observation]{}, fmt.Errorf("list combined latest timeline: %w", queryErr)
		}
		items = observationsFromCombinedLatest(rows)
	case ModeSourceAsOf:
		rows, queryErr := s.Queries.ListTimelineObservationsCombinedSourceAsOf(ctx, database.ListTimelineObservationsCombinedSourceAsOfParams{Column1: seriesIDs, ObservationTime: timestamptz(request.From), ObservationTime_2: timestamptz(request.To), Column4: timestamptz(request.AsOf), Limit: rowsLimit, Column6: cursor.ID != "", ObservationTime_3: timestamptz(cursor.ObservationTime), Column8: cursorSeriesID, Column9: cursorID})
		if queryErr != nil {
			return Page[Observation]{}, fmt.Errorf("list combined source-as-of timeline: %w", queryErr)
		}
		items = observationsFromCombinedSource(rows)
	case ModeSystemAsOf:
		rows, queryErr := s.Queries.ListTimelineObservationsCombinedSystemAsOf(ctx, database.ListTimelineObservationsCombinedSystemAsOfParams{Column1: seriesIDs, ObservationTime: timestamptz(request.From), ObservationTime_2: timestamptz(request.To), Column4: timestamptz(request.AsOf), Limit: rowsLimit, Column6: cursor.ID != "", ObservationTime_3: timestamptz(cursor.ObservationTime), Column8: cursorSeriesID, Column9: cursorID})
		if queryErr != nil {
			return Page[Observation]{}, fmt.Errorf("list combined system-as-of timeline: %w", queryErr)
		}
		items = observationsFromCombinedSystem(rows)
	}
	page := Page[Observation]{Items: items, Limit: limit}
	page.HasMore = len(page.Items) > limit
	if page.HasMore {
		page.Items = page.Items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeObservationCursor(observationCursor{ObservationTime: last.ObservationTime, SeriesID: last.SeriesID, ID: last.ID})
	}
	if page.Items == nil {
		page.Items = []Observation{}
	}
	return page, nil
}

func validateLimit(value int) error {
	if value < 1 || value > 200 {
		return ErrInvalidLimit
	}
	return nil
}

func encodeSeriesCursor(cursor seriesCursor) string {
	b, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeSeriesCursor(raw string) (seriesCursor, error) {
	if raw == "" {
		return seriesCursor{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return seriesCursor{}, ErrInvalidCursor
	}
	var cursor seriesCursor
	if err := json.Unmarshal(b, &cursor); err != nil || cursor.DataSourceCode == "" || cursor.SourceCode == "" || cursor.ID == "" {
		return seriesCursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

func encodeObservationCursor(cursor observationCursor) string {
	b, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeObservationCursor(raw string) (observationCursor, error) {
	if raw == "" {
		return observationCursor{}, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return observationCursor{}, ErrInvalidCursor
	}
	var cursor observationCursor
	if err := json.Unmarshal(b, &cursor); err != nil || cursor.ID == "" || cursor.ObservationTime.IsZero() {
		return observationCursor{}, ErrInvalidCursor
	}
	return cursor, nil
}
func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(strings.TrimSpace(value)); err != nil {
		return id, fmt.Errorf("%w: %v", ErrInvalidSeriesID, err)
	}
	return id, nil
}
func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}

func seriesFromRow(row database.GetTimelineSeriesRow) Series {
	return Series{ID: row.ID.String(), DatasetID: row.DatasetID.String(), SourceCode: row.SourceCode, SourceName: row.DataSourceName, DatasetExternalKey: row.DatasetExternalKey, Name: row.Name, Unit: row.Unit, Frequency: row.Frequency, SeasonalAdjustment: row.SeasonalAdjustment, SourceTimezone: row.SourceTimezone, FreshnessPolicy: json.RawMessage(row.FreshnessPolicy), Capabilities: Capabilities{Latest: true, SourceAsOf: row.SourceAsOfSupported, SystemAsOf: true, Revisions: true}}
}
func seriesFromListRow(row database.ListTimelineSeriesRow) Series {
	return Series{ID: row.ID.String(), DatasetID: row.DatasetID.String(), SourceCode: row.SourceCode, SourceName: row.DataSourceName, DatasetExternalKey: row.DatasetExternalKey, Name: row.Name, Unit: row.Unit, Frequency: row.Frequency, SeasonalAdjustment: row.SeasonalAdjustment, SourceTimezone: row.SourceTimezone, FreshnessPolicy: json.RawMessage(row.FreshnessPolicy), Capabilities: Capabilities{Latest: true, SourceAsOf: row.SourceAsOfSupported, SystemAsOf: true, Revisions: true}}
}

func observation(id pgtype.UUID, sid pgtype.UUID, at, source, system pgtype.Timestamptz, value pgtype.Numeric, valueText *string, basis string, raw pgtype.UUID, sha, unit, frequency, sourceCode string, quality []byte) Observation {
	var numeric *string
	if value.Valid && !value.NaN && value.InfinityModifier == 0 {
		v := "0"
		if value.Int != nil {
			v = value.Int.String()
		}
		sign := ""
		if strings.HasPrefix(v, "-") || strings.HasPrefix(v, "+") {
			sign, v = v[:1], v[1:]
		}
		if value.Exp >= 0 {
			v += strings.Repeat("0", int(value.Exp))
		} else {
			digits := int(-value.Exp)
			if len(v) <= digits {
				v = strings.Repeat("0", digits-len(v)+1) + v
			}
			v = v[:len(v)-digits] + "." + v[len(v)-digits:]
		}
		numericValue := sign + v
		numeric = &numericValue
	}
	if valueText != nil {
		numeric = nil
	}
	if !json.Valid(quality) {
		quality = []byte(`{}`)
	}
	clocks := Clocks{KnowledgeTimeBasis: basis}
	if system.Valid {
		clocks.SystemKnownAt = system.Time.UTC()
	}
	if source.Valid {
		t := source.Time.UTC()
		clocks.SourceKnownAt = &t
	}
	return Observation{ID: id.String(), SeriesID: sid.String(), ObservationTime: at.Time.UTC(), Value: numeric, ValueText: valueText, Unit: unit, Frequency: frequency, DataSourceCode: sourceCode, Clocks: clocks, Quality: json.RawMessage(quality), RawProvenanceID: raw.String(), RawObjectSHA256: sha}
}

func observationsFromLatest(rows []database.ListTimelineObservationsLatestRow) []Observation {
	out := make([]Observation, 0, len(rows))
	for _, r := range rows {
		out = append(out, observation(r.ID, r.SeriesID, r.ObservationTime, r.SourceKnownAt, r.SystemKnownAt, r.Value, r.ValueText, r.KnowledgeTimeBasis, r.RawObjectID, r.RawObjectSha256, r.Unit, r.Frequency, r.DataSourceCode, r.QualityFlags))
	}
	return out
}
func observationsFromSystem(rows []database.ListTimelineObservationsSystemAsOfRow) []Observation {
	out := make([]Observation, 0, len(rows))
	for _, r := range rows {
		out = append(out, observation(r.ID, r.SeriesID, r.ObservationTime, r.SourceKnownAt, r.SystemKnownAt, r.Value, r.ValueText, r.KnowledgeTimeBasis, r.RawObjectID, r.RawObjectSha256, r.Unit, r.Frequency, r.DataSourceCode, r.QualityFlags))
	}
	return out
}
func observationsFromSource(rows []database.ListTimelineObservationsSourceAsOfRow) []Observation {
	out := make([]Observation, 0, len(rows))
	for _, r := range rows {
		out = append(out, observation(r.ID, r.SeriesID, r.ObservationTime, r.SourceKnownAt, r.SystemKnownAt, r.Value, r.ValueText, r.KnowledgeTimeBasis, r.RawObjectID, r.RawObjectSha256, r.Unit, r.Frequency, r.DataSourceCode, r.QualityFlags))
	}
	return out
}
func observationsFromRevisions(rows []database.ListTimelineObservationRevisionsRow) []Observation {
	out := make([]Observation, 0, len(rows))
	for _, r := range rows {
		out = append(out, observation(r.ID, r.SeriesID, r.ObservationTime, r.SourceKnownAt, r.SystemKnownAt, r.Value, r.ValueText, r.KnowledgeTimeBasis, r.RawObjectID, r.RawObjectSha256, r.Unit, r.Frequency, r.DataSourceCode, r.QualityFlags))
	}
	return out
}

func observationsFromCombinedLatest(rows []database.ListTimelineObservationsCombinedLatestRow) []Observation {
	out := make([]Observation, 0, len(rows))
	for _, r := range rows {
		out = append(out, observation(r.ID, r.SeriesID, r.ObservationTime, r.SourceKnownAt, r.SystemKnownAt, r.Value, r.ValueText, r.KnowledgeTimeBasis, r.RawObjectID, r.RawObjectSha256, r.Unit, r.Frequency, r.DataSourceCode, r.QualityFlags))
	}
	return out
}

func observationsFromCombinedSource(rows []database.ListTimelineObservationsCombinedSourceAsOfRow) []Observation {
	out := make([]Observation, 0, len(rows))
	for _, r := range rows {
		out = append(out, observation(r.ID, r.SeriesID, r.ObservationTime, r.SourceKnownAt, r.SystemKnownAt, r.Value, r.ValueText, r.KnowledgeTimeBasis, r.RawObjectID, r.RawObjectSha256, r.Unit, r.Frequency, r.DataSourceCode, r.QualityFlags))
	}
	return out
}

func observationsFromCombinedSystem(rows []database.ListTimelineObservationsCombinedSystemAsOfRow) []Observation {
	out := make([]Observation, 0, len(rows))
	for _, r := range rows {
		out = append(out, observation(r.ID, r.SeriesID, r.ObservationTime, r.SourceKnownAt, r.SystemKnownAt, r.Value, r.ValueText, r.KnowledgeTimeBasis, r.RawObjectID, r.RawObjectSha256, r.Unit, r.Frequency, r.DataSourceCode, r.QualityFlags))
	}
	return out
}
