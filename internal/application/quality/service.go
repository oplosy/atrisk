package quality

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/oplosy/atrisk/internal/platform/database"
	domain "github.com/oplosy/atrisk/internal/quality"
)

var (
	ErrInvalidRequest = errors.New("invalid quality evaluation request")
	ErrNotFound       = errors.New("quality series not found")
)

const (
	maxInputs       = 100
	maxWindow       = 370 * 24 * time.Hour
	maxObservations = 10000
)

type Service struct {
	Queries *database.Queries
}

type Input struct {
	SeriesID string `json:"series_id"`
	Required bool   `json:"required"`
}

type Request struct {
	From   time.Time `json:"from"`
	To     time.Time `json:"to"`
	AsOf   time.Time `json:"as_of"`
	Inputs []Input   `json:"inputs"`
}

type Item struct {
	SeriesID string `json:"series_id"`
	Required bool   `json:"required"`
	domain.Result
}

type Response struct {
	State       domain.State `json:"state"`
	EvaluatedAt time.Time    `json:"evaluated_at"`
	Items       []Item       `json:"items"`
}

func (s Service) Evaluate(ctx context.Context, request Request) (Response, error) {
	if s.Queries == nil {
		return Response{}, errors.New("quality database is required")
	}
	if !request.From.Before(request.To) || request.To.After(request.AsOf) || request.AsOf.IsZero() || request.To.Sub(request.From) > maxWindow {
		return Response{}, ErrInvalidRequest
	}
	if len(request.Inputs) == 0 || len(request.Inputs) > maxInputs {
		return Response{}, ErrInvalidRequest
	}
	response := Response{EvaluatedAt: request.AsOf.UTC(), Items: make([]Item, 0, len(request.Inputs))}
	states := make([]domain.State, 0, len(request.Inputs))
	seen := make(map[string]struct{}, len(request.Inputs))
	totalObservations := 0
	for _, input := range request.Inputs {
		id := pgtype.UUID{}
		if err := id.Scan(input.SeriesID); err != nil || !id.Valid {
			return Response{}, ErrInvalidRequest
		}
		seriesID := id.String()
		if _, exists := seen[seriesID]; exists {
			return Response{}, ErrInvalidRequest
		}
		seen[seriesID] = struct{}{}
		series, err := s.Queries.GetTimelineSeries(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return Response{}, ErrNotFound
		}
		if err != nil {
			return Response{}, fmt.Errorf("get quality series: %w", err)
		}
		if !series.CreatedAt.Valid || series.CreatedAt.Time.After(request.AsOf) {
			return Response{}, ErrNotFound
		}
		rows, err := s.Queries.ListQualityObservations(ctx, database.ListQualityObservationsParams{
			EvaluationCutoff: pgtype.Timestamptz{Time: request.AsOf.UTC(), Valid: true},
			SeriesID:         id,
			FromTime:         pgtype.Timestamptz{Time: request.From.UTC(), Valid: true},
			ToTime:           pgtype.Timestamptz{Time: request.To.UTC(), Valid: true},
		})
		if err != nil {
			return Response{}, fmt.Errorf("list quality observations: %w", err)
		}
		totalObservations += len(rows)
		if totalObservations > maxObservations {
			return Response{}, ErrInvalidRequest
		}
		samples := make([]domain.Sample, 0, len(rows))
		for _, row := range rows {
			samples = append(samples, domain.Sample{
				ObservationTime: row.ObservationTime.Time,
				QualityFlags:    json.RawMessage(row.QualityFlags),
				Revised:         row.Revised,
			})
		}
		sourceTimezone := ""
		if series.SourceTimezone != nil {
			sourceTimezone = *series.SourceTimezone
		}
		result := domain.Evaluate(series.Frequency, sourceTimezone, json.RawMessage(series.FreshnessPolicy), request.From, request.To, request.AsOf, samples, input.Required)
		response.Items = append(response.Items, Item{SeriesID: seriesID, Required: input.Required, Result: result})
		states = append(states, result.State)
	}
	response.State = domain.Aggregate(states)
	return response, nil
}
