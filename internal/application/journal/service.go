package journal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	domain "github.com/oplosy/atrisk/internal/domain/journal"
)

var (
	ErrInvalidRequest = errors.New("invalid decision journal request")
	ErrNotFound       = errors.New("decision journal resource not found")
	ErrConflict       = errors.New("decision journal conflict")
)

type Service struct{ Pool *pgxpool.Pool }

type ConflictError struct {
	Code    string
	Message string
}

func (e *ConflictError) Error() string { return e.Code + ": " + e.Message }
func (e *ConflictError) Unwrap() error { return ErrConflict }

var decimalPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)

func (s Service) Create(ctx context.Context, request domain.CreateRequest) (domain.Decision, error) {
	request = normalizeCreate(request)
	if s.Pool == nil || !validCreate(request) {
		return domain.Decision{}, ErrInvalidRequest
	}
	amount, err := parseDecimal(request.RiskBudget.Amount)
	if err != nil || amount.Sign() < 0 {
		return domain.Decision{}, ErrInvalidRequest
	}
	amount = scaleDecimal(amount)
	if _, err := parseUUID(request.AccountID); err != nil {
		return domain.Decision{}, ErrInvalidRequest
	}
	if !request.Horizon.Start.Before(request.Horizon.End) {
		return domain.Decision{}, ErrInvalidRequest
	}
	if request.SourceMetadata == nil {
		request.SourceMetadata = map[string]any{}
	}
	alternatives, err := json.Marshal(request.Alternatives)
	if err != nil {
		return domain.Decision{}, ErrInvalidRequest
	}
	evidence, err := json.Marshal(request.EvidenceReferences)
	if err != nil {
		return domain.Decision{}, ErrInvalidRequest
	}
	invalidation, err := json.Marshal(request.InvalidationConditions)
	if err != nil {
		return domain.Decision{}, ErrInvalidRequest
	}
	sourceMetadata, err := json.Marshal(request.SourceMetadata)
	if err != nil {
		return domain.Decision{}, ErrInvalidRequest
	}
	creationSnapshot, err := json.Marshal(map[string]any{
		"account_id": request.AccountID, "thesis": request.Thesis,
		"alternatives": request.Alternatives, "evidence_references": request.EvidenceReferences,
		"invalidation_conditions": request.InvalidationConditions, "horizon": request.Horizon,
		"risk_budget": request.RiskBudget, "intended_action": request.IntendedAction,
		"tags": request.Tags, "status": domain.StatusDraft, "author": request.Author,
		"source_metadata": request.SourceMetadata,
	})
	if err != nil {
		return domain.Decision{}, ErrInvalidRequest
	}
	var result domain.Decision
	var alternativesJSON, evidenceJSON, invalidationJSON, sourceJSON []byte
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO decisions (
			account_id, thesis, alternatives, evidence_references,
			invalidation_conditions, horizon_start, horizon_end, creation_snapshot,
			risk_budget_amount, risk_budget_currency, risk_budget_measure,
			risk_budget_horizon, intended_action, tags, author, source_metadata
		) VALUES ($1::uuid,$2,$3::jsonb,$4::jsonb,$5::jsonb,$6,$7,$8::jsonb,$9,$10,$11,$12,$13,$14,$15,$16::jsonb)
		RETURNING id::text, account_id::text, thesis, alternatives, evidence_references,
		          invalidation_conditions, horizon_start, horizon_end,
		          risk_budget_amount::text, risk_budget_currency, risk_budget_measure,
		          risk_budget_horizon, intended_action, tags, status, author,
		          source_metadata, created_at, finalized_at`,
		request.AccountID, request.Thesis, alternatives, evidence, invalidation,
		request.Horizon.Start.UTC(), request.Horizon.End.UTC(), creationSnapshot, formatDecimal(amount),
		request.RiskBudget.Currency, request.RiskBudget.Measure, request.RiskBudget.Horizon,
		request.IntendedAction, request.Tags, request.Author, sourceMetadata,
	).Scan(
		&result.ID, &result.AccountID, &result.Thesis, &alternativesJSON, &evidenceJSON,
		&invalidationJSON, &result.Horizon.Start, &result.Horizon.End,
		&result.RiskBudget.Amount, &result.RiskBudget.Currency, &result.RiskBudget.Measure,
		&result.RiskBudget.Horizon, &result.IntendedAction, &result.Tags, &result.Status,
		&result.Author, &sourceJSON, &result.CreatedAt, &result.FinalizedAt,
	)
	if err != nil {
		if isForeignKeyViolation(err) {
			return domain.Decision{}, ErrNotFound
		}
		return domain.Decision{}, fmt.Errorf("create decision: %w", err)
	}
	if err := hydrateDecision(&result, alternativesJSON, evidenceJSON, invalidationJSON, sourceJSON); err != nil {
		return domain.Decision{}, fmt.Errorf("decode decision: %w", err)
	}
	return result, nil
}

func (s Service) Get(ctx context.Context, rawID string) (domain.Decision, error) {
	id, err := parseUUID(rawID)
	if err != nil || s.Pool == nil {
		return domain.Decision{}, ErrInvalidRequest
	}
	return s.get(ctx, id)
}

func (s Service) Finalize(ctx context.Context, rawID string) (domain.Decision, error) {
	id, err := parseUUID(rawID)
	if err != nil || s.Pool == nil {
		return domain.Decision{}, ErrInvalidRequest
	}
	var result domain.Decision
	var alternativesJSON, evidenceJSON, invalidationJSON, sourceJSON []byte
	err = s.Pool.QueryRow(ctx, `
		UPDATE decisions
		SET status='finalized', finalized_at=clock_timestamp()
		WHERE id=$1::uuid AND status='draft'
		RETURNING id::text, account_id::text, thesis, alternatives, evidence_references,
		          invalidation_conditions, horizon_start, horizon_end,
		          risk_budget_amount::text, risk_budget_currency, risk_budget_measure,
		          risk_budget_horizon, intended_action, tags, status, author,
		          source_metadata, created_at, finalized_at`, id,
	).Scan(
		&result.ID, &result.AccountID, &result.Thesis, &alternativesJSON, &evidenceJSON,
		&invalidationJSON, &result.Horizon.Start, &result.Horizon.End,
		&result.RiskBudget.Amount, &result.RiskBudget.Currency, &result.RiskBudget.Measure,
		&result.RiskBudget.Horizon, &result.IntendedAction, &result.Tags, &result.Status,
		&result.Author, &sourceJSON, &result.CreatedAt, &result.FinalizedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		var status string
		lookupErr := s.Pool.QueryRow(ctx, `SELECT status FROM decisions WHERE id=$1::uuid`, id).Scan(&status)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return domain.Decision{}, ErrNotFound
		}
		if lookupErr != nil {
			return domain.Decision{}, fmt.Errorf("check decision finalization: %w", lookupErr)
		}
		return domain.Decision{}, &ConflictError{Code: "DECISION_IMMUTABLE", Message: "the decision is already finalized"}
	}
	if err != nil {
		return domain.Decision{}, fmt.Errorf("finalize decision: %w", err)
	}
	if err := hydrateDecision(&result, alternativesJSON, evidenceJSON, invalidationJSON, sourceJSON); err != nil {
		return domain.Decision{}, fmt.Errorf("decode finalized decision: %w", err)
	}
	return result, nil
}

func (s Service) AddReview(ctx context.Context, rawID string, request domain.ReviewRequest) (domain.Review, error) {
	request = normalizeReview(request)
	if s.Pool == nil || !validUUID(rawID) || strings.TrimSpace(request.Review) == "" || strings.TrimSpace(request.Outcome) == "" || strings.TrimSpace(request.Author) == "" {
		return domain.Review{}, ErrInvalidRequest
	}
	if request.SourceMetadata == nil {
		request.SourceMetadata = map[string]any{}
	}
	metadata, err := json.Marshal(request.SourceMetadata)
	if err != nil {
		return domain.Review{}, ErrInvalidRequest
	}
	var result domain.Review
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO decision_reviews (decision_id,review,outcome,author,source_metadata)
		SELECT $1::uuid,$2,$3,$4,$5::jsonb
		WHERE EXISTS (SELECT 1 FROM decisions WHERE id=$1::uuid)
		RETURNING id::text, decision_id::text, review, outcome, author, source_metadata, created_at`,
		rawID, request.Review, request.Outcome, request.Author, metadata,
	).Scan(&result.ID, &result.DecisionID, &result.Review, &result.Outcome, &result.Author, &metadata, &result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Review{}, ErrNotFound
	}
	if err != nil {
		return domain.Review{}, fmt.Errorf("create decision review: %w", err)
	}
	result.SourceMetadata = map[string]any{}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &result.SourceMetadata)
	}
	return result, nil
}

func (s Service) AddAmendment(ctx context.Context, rawID string, request domain.AmendmentRequest) (domain.Amendment, error) {
	request = normalizeAmendment(request)
	if s.Pool == nil || !validUUID(rawID) || strings.TrimSpace(request.Summary) == "" || request.Changes == nil || strings.TrimSpace(request.Author) == "" {
		return domain.Amendment{}, ErrInvalidRequest
	}
	if request.SourceMetadata == nil {
		request.SourceMetadata = map[string]any{}
	}
	changes, err := json.Marshal(request.Changes)
	if err != nil {
		return domain.Amendment{}, ErrInvalidRequest
	}
	metadata, err := json.Marshal(request.SourceMetadata)
	if err != nil {
		return domain.Amendment{}, ErrInvalidRequest
	}
	var result domain.Amendment
	err = s.Pool.QueryRow(ctx, `
		INSERT INTO decision_amendments (decision_id,summary,changes,author,source_metadata)
		SELECT $1::uuid,$2,$3::jsonb,$4,$5::jsonb
		WHERE EXISTS (SELECT 1 FROM decisions WHERE id=$1::uuid)
		RETURNING id::text, decision_id::text, summary, changes, author, source_metadata, created_at`,
		rawID, request.Summary, changes, request.Author, metadata,
	).Scan(&result.ID, &result.DecisionID, &result.Summary, &changes, &result.Author, &metadata, &result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Amendment{}, ErrNotFound
	}
	if err != nil {
		return domain.Amendment{}, fmt.Errorf("create decision amendment: %w", err)
	}
	result.Changes = map[string]any{}
	result.SourceMetadata = map[string]any{}
	_ = json.Unmarshal(changes, &result.Changes)
	_ = json.Unmarshal(metadata, &result.SourceMetadata)
	return result, nil
}

func (s Service) Timeline(ctx context.Context, rawID string) (domain.Timeline, error) {
	id, err := parseUUID(rawID)
	if err != nil || s.Pool == nil {
		return domain.Timeline{}, ErrInvalidRequest
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return domain.Timeline{}, fmt.Errorf("begin decision timeline snapshot: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	decision, err := getDecision(ctx, tx, id)
	if err != nil {
		return domain.Timeline{}, err
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, decision_id::text, kind, payload, author, source_metadata, created_at
		FROM (
			SELECT id, id AS decision_id, 'decision' AS kind, creation_snapshot AS payload,
			       author, source_metadata, created_at, timeline_sequence
			FROM decisions WHERE id=$1::uuid
			UNION ALL
			SELECT id, decision_id, 'review',
			       jsonb_build_object('review', review, 'outcome', outcome),
			       author, source_metadata, created_at, timeline_sequence
			FROM decision_reviews WHERE decision_id=$1::uuid
			UNION ALL
			SELECT id, decision_id, 'amendment',
			       jsonb_build_object('summary', summary, 'changes', changes),
			       author, source_metadata, created_at, timeline_sequence
			FROM decision_amendments WHERE decision_id=$1::uuid
		) events
		ORDER BY timeline_sequence ASC`, id)
	if err != nil {
		return domain.Timeline{}, fmt.Errorf("list decision timeline: %w", err)
	}
	defer rows.Close()
	timeline := domain.Timeline{Decision: decision, Events: []domain.TimelineEvent{}}
	for rows.Next() {
		var event domain.TimelineEvent
		var payload, metadata []byte
		if err := rows.Scan(&event.ID, &event.DecisionID, &event.Kind, &payload, &event.Author, &metadata, &event.CreatedAt); err != nil {
			return domain.Timeline{}, fmt.Errorf("scan decision timeline: %w", err)
		}
		event.Payload = json.RawMessage(payload)
		event.SourceMetadata = map[string]any{}
		_ = json.Unmarshal(metadata, &event.SourceMetadata)
		timeline.Events = append(timeline.Events, event)
	}
	if err := rows.Err(); err != nil {
		return domain.Timeline{}, fmt.Errorf("read decision timeline: %w", err)
	}
	rows.Close()
	if err := tx.Commit(ctx); err != nil {
		return domain.Timeline{}, fmt.Errorf("commit decision timeline snapshot: %w", err)
	}
	return timeline, nil
}

func (s Service) get(ctx context.Context, id string) (domain.Decision, error) {
	return getDecision(ctx, s.Pool, id)
}

type rowQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func getDecision(ctx context.Context, query rowQuerier, id string) (domain.Decision, error) {
	var result domain.Decision
	var alternativesJSON, evidenceJSON, invalidationJSON, sourceJSON []byte
	err := query.QueryRow(ctx, `
		SELECT id::text, account_id::text, thesis, alternatives, evidence_references,
		       invalidation_conditions, horizon_start, horizon_end,
		       risk_budget_amount::text, risk_budget_currency, risk_budget_measure,
		       risk_budget_horizon, intended_action, tags, status, author,
		       source_metadata, created_at, finalized_at
		FROM decisions WHERE id=$1::uuid`, id).Scan(
		&result.ID, &result.AccountID, &result.Thesis, &alternativesJSON, &evidenceJSON,
		&invalidationJSON, &result.Horizon.Start, &result.Horizon.End,
		&result.RiskBudget.Amount, &result.RiskBudget.Currency, &result.RiskBudget.Measure,
		&result.RiskBudget.Horizon, &result.IntendedAction, &result.Tags, &result.Status,
		&result.Author, &sourceJSON, &result.CreatedAt, &result.FinalizedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Decision{}, ErrNotFound
	}
	if err != nil {
		return domain.Decision{}, fmt.Errorf("get decision: %w", err)
	}
	if err := hydrateDecision(&result, alternativesJSON, evidenceJSON, invalidationJSON, sourceJSON); err != nil {
		return domain.Decision{}, fmt.Errorf("decode decision: %w", err)
	}
	return result, nil
}

func hydrateDecision(result *domain.Decision, alternatives, evidence, invalidation, metadata []byte) error {
	result.Alternatives = []string{}
	result.EvidenceReferences = []domain.EvidenceRef{}
	result.InvalidationConditions = []domain.Invalidation{}
	result.SourceMetadata = map[string]any{}
	if err := json.Unmarshal(alternatives, &result.Alternatives); err != nil {
		return err
	}
	if err := json.Unmarshal(evidence, &result.EvidenceReferences); err != nil {
		return err
	}
	if err := json.Unmarshal(invalidation, &result.InvalidationConditions); err != nil {
		return err
	}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &result.SourceMetadata); err != nil {
			return err
		}
	}
	return nil
}

func normalizeCreate(request domain.CreateRequest) domain.CreateRequest {
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.Thesis = strings.TrimSpace(request.Thesis)
	request.IntendedAction = strings.TrimSpace(request.IntendedAction)
	request.Author = strings.TrimSpace(request.Author)
	request.RiskBudget.Amount = strings.TrimSpace(request.RiskBudget.Amount)
	request.RiskBudget.Currency = strings.ToUpper(strings.TrimSpace(request.RiskBudget.Currency))
	request.RiskBudget.Measure = strings.TrimSpace(request.RiskBudget.Measure)
	request.RiskBudget.Horizon = strings.TrimSpace(request.RiskBudget.Horizon)
	if request.Alternatives == nil {
		request.Alternatives = []string{}
	}
	if request.EvidenceReferences == nil {
		request.EvidenceReferences = []domain.EvidenceRef{}
	}
	if request.Tags == nil {
		request.Tags = []string{}
	}
	for i := range request.Alternatives {
		request.Alternatives[i] = strings.TrimSpace(request.Alternatives[i])
	}
	for i := range request.Tags {
		request.Tags[i] = strings.TrimSpace(request.Tags[i])
	}
	for i := range request.EvidenceReferences {
		request.EvidenceReferences[i].Kind = strings.TrimSpace(request.EvidenceReferences[i].Kind)
		request.EvidenceReferences[i].Reference = strings.TrimSpace(request.EvidenceReferences[i].Reference)
		request.EvidenceReferences[i].Description = strings.TrimSpace(request.EvidenceReferences[i].Description)
	}
	return request
}

func normalizeReview(request domain.ReviewRequest) domain.ReviewRequest {
	request.Review, request.Outcome, request.Author = strings.TrimSpace(request.Review), strings.TrimSpace(request.Outcome), strings.TrimSpace(request.Author)
	return request
}

func normalizeAmendment(request domain.AmendmentRequest) domain.AmendmentRequest {
	request.Summary, request.Author = strings.TrimSpace(request.Summary), strings.TrimSpace(request.Author)
	return request
}

func validCreate(request domain.CreateRequest) bool {
	if request.AccountID == "" || request.Thesis == "" || request.IntendedAction == "" || request.Author == "" || request.RiskBudget.Currency == "" || request.RiskBudget.Measure == "" || request.RiskBudget.Horizon == "" || !validPersistedDecimal(request.RiskBudget.Amount) || len(request.InvalidationConditions) == 0 {
		return false
	}
	for _, evidence := range request.EvidenceReferences {
		if strings.TrimSpace(evidence.Kind) == "" || strings.TrimSpace(evidence.Reference) == "" {
			return false
		}
	}
	for _, condition := range request.InvalidationConditions {
		if strings.TrimSpace(condition.Condition) == "" {
			return false
		}
	}
	return true
}

func validUUID(value string) bool {
	var id pgtype.UUID
	return id.Scan(strings.TrimSpace(value)) == nil && id.Valid
}

func parseUUID(value string) (string, error) {
	value = strings.TrimSpace(value)
	var id pgtype.UUID
	if err := id.Scan(value); err != nil || !id.Valid {
		return "", ErrInvalidRequest
	}
	return id.String(), nil
}

func parseDecimal(value string) (*big.Rat, error) {
	result := new(big.Rat)
	if !decimalPattern.MatchString(value) {
		return nil, ErrInvalidRequest
	}
	if _, ok := result.SetString(value); !ok {
		return nil, ErrInvalidRequest
	}
	return result, nil
}

// NUMERIC(38,18) permits at most 20 integer digits and 18 fractional digits.
// Rejecting out-of-range input before formatting prevents silent rounding or
// a database-dependent overflow error at the persistence boundary.
func validPersistedDecimal(value string) bool {
	if !decimalPattern.MatchString(value) {
		return false
	}
	value = strings.TrimPrefix(value, "-")
	parts := strings.SplitN(value, ".", 2)
	if len(parts[0]) > 20 {
		return false
	}
	return len(parts) == 1 || len(parts[1]) <= 18
}

func scaleDecimal(value *big.Rat) *big.Rat {
	result, err := parseDecimal(formatDecimal(value))
	if err != nil {
		return new(big.Rat)
	}
	return result
}

func formatDecimal(value *big.Rat) string {
	if value == nil {
		return "0"
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	numerator := new(big.Int).Mul(value.Num(), scale)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, value.Denom(), remainder)
	if remainder.Sign() != 0 {
		double := new(big.Int).Abs(remainder)
		double.Lsh(double, 1)
		if double.Cmp(value.Denom()) >= 0 {
			if numerator.Sign() < 0 {
				quotient.Sub(quotient, big.NewInt(1))
			} else {
				quotient.Add(quotient, big.NewInt(1))
			}
		}
	}
	digits := quotient.String()
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	if len(digits) <= 18 {
		digits = strings.Repeat("0", 19-len(digits)) + digits
	}
	point := len(digits) - 18
	return sign + digits[:point] + "." + digits[point:]
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
