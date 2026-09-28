// Package risk exposes immutable risk and stress-run evidence without exposing
// the persistence tables as an HTTP contract.
package risk

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
	"github.com/jackc/pgx/v5/pgxpool"
	applicationscenarios "github.com/oplosy/atrisk/internal/application/scenarios"
)

var (
	ErrInvalidRequest = errors.New("invalid risk request")
	ErrNotFound       = errors.New("risk run not found")
)

type SubmitRequest struct {
	AccountID   string `json:"account_id"`
	SnapshotID  string `json:"snapshot_id"`
	ScenarioID  string `json:"scenario_id,omitempty"`
	Name        string `json:"name"`
	TemplateKey string `json:"template_key"`
	// IdempotencyKey is supplied by the Idempotency-Key header at the HTTP
	// boundary. json:"-" prevents a body value from becoming a second source.
	IdempotencyKey string           `json:"-"`
	Units          map[string]any   `json:"units"`
	Shocks         map[string]any   `json:"shocks"`
	Mappings       map[string]any   `json:"mappings"`
	Assumptions    map[string]any   `json:"assumptions"`
	Positions      []map[string]any `json:"positions"`
	PreMetrics     map[string]any   `json:"pre_metrics,omitempty"`
}

type Run struct {
	ID                 string          `json:"id"`
	ScenarioID         string          `json:"scenario_id"`
	ScenarioVersion    int             `json:"scenario_version"`
	AccountID          string          `json:"account_id"`
	SnapshotID         string          `json:"snapshot_id"`
	JobID              string          `json:"job_id"`
	Status             string          `json:"status"`
	DataQuality        string          `json:"data_quality"`
	SchemaVersion      string          `json:"schema_version"`
	EngineVersion      string          `json:"engine_version"`
	ScenarioTemplate   string          `json:"scenario_template"`
	ScenarioContentSHA string          `json:"scenario_content_sha256"`
	RequestHash        string          `json:"request_hash"`
	ResultHash         string          `json:"result_hash,omitempty"`
	InputSnapshotIDs   []string        `json:"input_snapshot_ids"`
	ReasonCodes        []string        `json:"reason_codes"`
	Result             json.RawMessage `json:"result,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	CompletedAt        *time.Time      `json:"completed_at,omitempty"`
	ErrorCode          string          `json:"error_code,omitempty"`
	ErrorMessage       string          `json:"error_message,omitempty"`
}

type Page struct {
	Items      []map[string]any `json:"items"`
	Limit      int              `json:"limit"`
	NextCursor string           `json:"next_cursor,omitempty"`
	HasMore    bool             `json:"has_more"`
}

type Service struct{ Pool *pgxpool.Pool }

func (s Service) Submit(ctx context.Context, request SubmitRequest) (Run, error) {
	if s.Pool == nil || strings.TrimSpace(request.IdempotencyKey) == "" || !validUUID(request.AccountID) || !validUUID(request.SnapshotID) || (request.ScenarioID != "" && !validUUID(request.ScenarioID)) {
		return Run{}, ErrInvalidRequest
	}
	created, err := (applicationscenarios.Service{Pool: s.Pool}).CreateVersionAndRun(ctx, applicationscenarios.VersionInput{
		AccountID: request.AccountID, SnapshotID: request.SnapshotID, ScenarioID: request.ScenarioID,
		Name: request.Name, TemplateKey: request.TemplateKey, IdempotencyKey: request.IdempotencyKey,
		Units: request.Units, Shocks: request.Shocks, Mappings: request.Mappings, Assumptions: request.Assumptions,
		Positions: request.Positions, PreMetrics: request.PreMetrics,
	})
	if err != nil {
		if errors.Is(err, applicationscenarios.ErrInvalidRequest) {
			return Run{}, ErrInvalidRequest
		}
		return Run{}, fmt.Errorf("submit risk run: %w", err)
	}
	return s.Get(ctx, created.ID)
}

func validUUID(value string) bool {
	var id pgtype.UUID
	return id.Scan(strings.TrimSpace(value)) == nil && id.Valid
}

func (s Service) Get(ctx context.Context, id string) (Run, error) {
	if s.Pool == nil || strings.TrimSpace(id) == "" {
		return Run{}, ErrInvalidRequest
	}
	const query = `
SELECT sr.id::text, sr.scenario_id::text, sr.scenario_version, sr.account_id::text,
       sr.snapshot_id::text, sr.job_id::text, sr.state, COALESCE(sr.result,'null'::jsonb),
       COALESCE(btrim(sr.result_hash),''), btrim(sr.request_hash), sr.created_at, sr.completed_at,
       sv.template_key, btrim(sv.content_hash), j.state, j.schema_version,
       j.input_snapshot_ids, COALESCE(j.result,'null'::jsonb), COALESCE(btrim(j.result_hash),''),
       COALESCE(j.error_code,''), COALESCE(j.error_message,'')
FROM scenario_runs sr
JOIN scenario_versions sv ON sv.scenario_id=sr.scenario_id AND sv.version=sr.scenario_version
JOIN risk_jobs j ON j.id=sr.job_id
WHERE sr.id=$1::uuid`
	var run Run
	var runState, jobState, template, resultHash, requestHash, schemaVersion, jobHash, errorCode, errorMessage string
	var scenarioResult, jobResult []byte
	var inputSnapshotIDs []string
	if err := s.Pool.QueryRow(ctx, query, id).Scan(&run.ID, &run.ScenarioID, &run.ScenarioVersion, &run.AccountID, &run.SnapshotID, &run.JobID, &runState, &scenarioResult, &resultHash, &requestHash, &run.CreatedAt, &run.CompletedAt, &template, &run.ScenarioContentSHA, &jobState, &schemaVersion, &inputSnapshotIDs, &jobResult, &jobHash, &errorCode, &errorMessage); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, fmt.Errorf("get risk run: %w", err)
	}
	run.ScenarioTemplate, run.RequestHash, run.SchemaVersion = template, requestHash, schemaVersion
	run.InputSnapshotIDs = append([]string(nil), inputSnapshotIDs...)
	run.Status = mapStatus(jobState, runState)
	run.DataQuality = mapQuality(runState, scenarioResult, jobResult)
	run.EngineVersion = resultString(scenarioResult, "engine_version")
	if run.EngineVersion == "" {
		run.EngineVersion = resultString(jobResult, "engine_version")
	}
	if run.EngineVersion == "" {
		run.EngineVersion = "unknown"
	}
	run.ResultHash = resultHash
	if run.ResultHash == "" {
		run.ResultHash = jobHash
	}
	if len(scenarioResult) > 0 && string(scenarioResult) != "null" {
		run.Result = append(json.RawMessage(nil), scenarioResult...)
	} else if len(jobResult) > 0 && string(jobResult) != "null" {
		run.Result = append(json.RawMessage(nil), jobResult...)
	}
	run.ReasonCodes = reasonCodes(run.Result, errorCode)
	run.ErrorCode, run.ErrorMessage = errorCode, errorMessage
	return run, nil
}

func (s Service) Positions(ctx context.Context, id, cursor string, limit int) (Page, error) {
	if s.Pool == nil || strings.TrimSpace(id) == "" || limit < 0 || limit > 200 {
		return Page{}, ErrInvalidRequest
	}
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM scenario_runs WHERE id=$1::uuid)`, id).Scan(&exists); err != nil {
		return Page{}, fmt.Errorf("check risk run: %w", err)
	}
	if !exists {
		return Page{}, ErrNotFound
	}
	if limit == 0 {
		limit = 50
	}
	last, err := decodeCursor(cursor)
	if err != nil {
		return Page{}, ErrInvalidRequest
	}
	const query = `
SELECT p.snapshot_line_id::text, p.instrument_id::text, p.state, p.reason_codes,
       p.pre_value_try::text, p.post_value_try::text, p.pnl_try::text,
       p.pre_value_usd::text, p.post_value_usd::text, p.pnl_usd::text,
       p.price_return::text, p.yield_return::text, p.fx_multiplier_try::text, p.fx_multiplier_usd::text
FROM scenario_run_positions p
WHERE p.run_id=$1::uuid AND ($2='' OR p.snapshot_line_id::text > $2)
ORDER BY p.snapshot_line_id LIMIT $3`
	rows, err := s.Pool.Query(ctx, query, id, last, limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("list risk positions: %w", err)
	}
	defer rows.Close()
	items := make([]map[string]any, 0, limit)
	for rows.Next() {
		var lineID, instrumentID, state string
		var reasons []string
		var preTRY, postTRY, pnlTRY, preUSD, postUSD, pnlUSD, priceReturn, yieldReturn, fxTRY, fxUSD *string
		if err := rows.Scan(&lineID, &instrumentID, &state, &reasons, &preTRY, &postTRY, &pnlTRY, &preUSD, &postUSD, &pnlUSD, &priceReturn, &yieldReturn, &fxTRY, &fxUSD); err != nil {
			return Page{}, fmt.Errorf("scan risk position: %w", err)
		}
		items = append(items, map[string]any{"snapshot_line_id": lineID, "instrument_id": instrumentID, "state": state, "reason_codes": reasons, "pre_value_try": preTRY, "post_value_try": postTRY, "pnl_try": pnlTRY, "pre_value_usd": preUSD, "post_value_usd": postUSD, "pnl_usd": pnlUSD, "price_return": priceReturn, "yield_return": yieldReturn, "fx_multiplier_try": fxTRY, "fx_multiplier_usd": fxUSD})
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("list risk positions: %w", err)
	}
	page := Page{Items: items, Limit: limit, HasMore: len(items) > limit}
	if page.HasMore {
		page.Items = page.Items[:limit]
		page.NextCursor = encodeCursor(page.Items[len(page.Items)-1]["snapshot_line_id"].(string))
	}
	return page, nil
}

func (s Service) Cancel(ctx context.Context, id, reason string) (Run, error) {
	if s.Pool == nil || strings.TrimSpace(id) == "" {
		return Run{}, ErrInvalidRequest
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Run{}, fmt.Errorf("begin cancel risk run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var jobID string
	if err := tx.QueryRow(ctx, `SELECT job_id::text FROM scenario_runs WHERE id=$1::uuid`, id).Scan(&jobID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Run{}, ErrNotFound
		}
		return Run{}, fmt.Errorf("find risk run job: %w", err)
	}
	if strings.TrimSpace(reason) == "" {
		reason = "cancelled by client"
	}
	var currentJobState string
	if err := tx.QueryRow(ctx, `SELECT state FROM risk_jobs WHERE id=$1::uuid FOR UPDATE`, jobID).Scan(&currentJobState); err != nil {
		return Run{}, fmt.Errorf("lock risk run job: %w", err)
	}
	result, err := tx.Exec(ctx, `UPDATE risk_jobs SET state='cancelled', error_code='JOB_CANCELLED', error_message=$1, lease_owner=NULL, lease_expires_at=NULL, completed_at=clock_timestamp() WHERE id=$2::uuid AND state IN ('queued','retryable_failed','running')`, reason, jobID)
	if err != nil {
		return Run{}, fmt.Errorf("cancel risk run: %w", err)
	}
	if result.RowsAffected() == 1 || currentJobState == "cancelled" {
		// scenario_runs has no cancelled state in the AR-303 schema. Mark the
		// unfinished evidence terminal atomically; the API status remains
		// cancelled from risk_jobs, while completed evidence is never touched.
		if _, err := tx.Exec(ctx, `UPDATE scenario_runs SET state='failed', completed_at=clock_timestamp() WHERE id=$1::uuid AND state IN ('queued','running')`, id); err != nil {
			return Run{}, fmt.Errorf("close cancelled scenario run: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Run{}, fmt.Errorf("commit cancel risk run: %w", err)
	}
	return s.Get(ctx, id)
}

func mapStatus(job, run string) string {
	switch job {
	case "queued":
		return "queued"
	case "running":
		return "running"
	case "retryable_failed":
		return "retryable"
	case "failed":
		return "permanent"
	case "cancelled":
		return "cancelled"
	case "succeeded":
		return "completed"
	}
	if run == "failed" {
		return "permanent"
	}
	return "queued"
}

func mapQuality(run string, scenario, job []byte) string {
	if run == "degraded" {
		return "degraded"
	}
	if run == "blocked" {
		return "blocked"
	}
	for _, raw := range [][]byte{scenario, job} {
		var envelope struct {
			DataQuality string `json:"data_quality"`
		}
		if json.Unmarshal(raw, &envelope) == nil && envelope.DataQuality != "" {
			return envelope.DataQuality
		}
	}
	if run == "valid" {
		return "healthy"
	}
	return "degraded"
}

func resultString(raw []byte, key string) string {
	var value map[string]any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	result, _ := value[key].(string)
	return result
}

func reasonCodes(raw json.RawMessage, errorCode string) []string {
	seen := map[string]struct{}{}
	add := func(value string) {
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	add(errorCode)
	var envelope struct {
		Output struct {
			Positions []struct {
				ReasonCodes []string `json:"reason_codes"`
			} `json:"positions"`
		} `json:"output"`
	}
	if json.Unmarshal(raw, &envelope) == nil {
		for _, position := range envelope.Output.Positions {
			for _, code := range position.ReasonCodes {
				add(code)
			}
		}
	}
	result := make([]string, 0, len(seen))
	for code := range seen {
		result = append(result, code)
	}
	return result
}

func encodeCursor(value string) string { return base64.RawURLEncoding.EncodeToString([]byte(value)) }
func decodeCursor(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) == 0 {
		return "", errors.New("invalid cursor")
	}
	return string(decoded), nil
}
