// Package scenarios versions account-scoped stress scenarios and queues revaluations.
package scenarios

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidRequest = errors.New("invalid scenario request")

type Service struct{ Pool *pgxpool.Pool }

type VersionInput struct {
	AccountID      string           `json:"account_id"`
	SnapshotID     string           `json:"snapshot_id"`
	ScenarioID     string           `json:"scenario_id,omitempty"`
	Name           string           `json:"name"`
	TemplateKey    string           `json:"template_key"`
	IdempotencyKey string           `json:"idempotency_key"`
	Units          map[string]any   `json:"units"`
	Shocks         map[string]any   `json:"shocks"`
	Mappings       map[string]any   `json:"mappings"`
	Assumptions    map[string]any   `json:"assumptions"`
	Positions      []map[string]any `json:"positions"`
	PreMetrics     map[string]any   `json:"pre_metrics"`
}

type Run struct {
	ID            string `json:"id"`
	ScenarioID    string `json:"scenario_id"`
	ScenarioVer   int    `json:"scenario_version"`
	JobID         string `json:"job_id"`
	ContentSHA256 string `json:"content_sha256"`
}

func valid(input VersionInput) bool {
	if input.AccountID == "" || input.SnapshotID == "" || strings.TrimSpace(input.Name) == "" || input.IdempotencyKey == "" {
		return false
	}
	if input.TemplateKey != "try_depreciation" && input.TemplateKey != "rates_up" && input.TemplateKey != "risk_off" {
		return false
	}
	return input.Units != nil && input.Shocks != nil && input.Mappings != nil && input.Assumptions != nil && input.Positions != nil
}

// CreateVersionAndRun stores a new immutable version and enqueues its run atomically.
func (s Service) CreateVersionAndRun(ctx context.Context, input VersionInput) (Run, error) {
	if s.Pool == nil || !valid(input) {
		return Run{}, ErrInvalidRequest
	}
	preMetrics := input.PreMetrics
	if preMetrics == nil {
		preMetrics = map[string]any{}
	}
	canonical, err := json.Marshal(map[string]any{
		"template_key": input.TemplateKey, "units": input.Units, "shocks": input.Shocks,
		"mappings": input.Mappings, "assumptions": input.Assumptions,
	})
	if err != nil {
		return Run{}, fmt.Errorf("encode scenario version: %w", err)
	}
	digest := sha256.Sum256(canonical)
	hash := hex.EncodeToString(digest[:])
	requestBytes, err := json.Marshal(map[string]any{
		"account_id": input.AccountID, "snapshot_id": input.SnapshotID, "scenario_id": input.ScenarioID,
		"template_key": input.TemplateKey, "units": input.Units, "shocks": input.Shocks,
		"mappings": input.Mappings, "assumptions": input.Assumptions,
		"positions": input.Positions, "pre_metrics": preMetrics,
	})
	if err != nil {
		return Run{}, fmt.Errorf("encode scenario request fingerprint: %w", err)
	}
	requestDigest := sha256.Sum256(requestBytes)
	requestHash := hex.EncodeToString(requestDigest[:])
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Run{}, fmt.Errorf("begin scenario transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('scenario.revalue:' || $1, 0))`, input.IdempotencyKey); err != nil {
		return Run{}, fmt.Errorf("lock scenario idempotency key: %w", err)
	}
	var existing Run
	var existingAccountID, existingSnapshotID, existingHash, existingRequestHash string
	err = tx.QueryRow(ctx, `
		SELECT sr.id::text,sr.scenario_id::text,sr.scenario_version,sr.job_id::text,
		       sr.account_id::text,sr.snapshot_id::text,btrim(sv.content_hash),sr.request_hash
		FROM scenario_runs sr
		JOIN risk_jobs j ON j.id=sr.job_id
		JOIN scenario_versions sv ON sv.scenario_id=sr.scenario_id AND sv.version=sr.scenario_version
		WHERE j.kind='scenario.revalue' AND j.idempotency_key=$1`, input.IdempotencyKey).Scan(
		&existing.ID, &existing.ScenarioID, &existing.ScenarioVer, &existing.JobID,
		&existingAccountID, &existingSnapshotID, &existingHash, &existingRequestHash,
	)
	if err == nil {
		if existingAccountID != input.AccountID || existingSnapshotID != input.SnapshotID || existingHash != hash || existingRequestHash != requestHash || (input.ScenarioID != "" && existing.ScenarioID != input.ScenarioID) {
			return Run{}, ErrInvalidRequest
		}
		existing.ContentSHA256 = existingHash
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Run{}, fmt.Errorf("check scenario idempotency key: %w", err)
	}
	if input.ScenarioID == "" {
		err = tx.QueryRow(ctx, `INSERT INTO scenarios (account_id,name,template_key) VALUES ($1::uuid,$2,$3) RETURNING id::text`, input.AccountID, input.Name, input.TemplateKey).Scan(&input.ScenarioID)
		if err != nil {
			return Run{}, fmt.Errorf("create scenario: %w", err)
		}
	} else {
		var accountID string
		err = tx.QueryRow(ctx, `SELECT account_id::text FROM scenarios WHERE id=$1::uuid FOR UPDATE`, input.ScenarioID).Scan(&accountID)
		if errors.Is(err, pgx.ErrNoRows) || accountID != input.AccountID {
			return Run{}, ErrInvalidRequest
		}
		if err != nil {
			return Run{}, fmt.Errorf("load scenario: %w", err)
		}
		if _, err = tx.Exec(ctx, `UPDATE scenarios SET template_key=$2 WHERE id=$1::uuid`, input.ScenarioID, input.TemplateKey); err != nil {
			return Run{}, fmt.Errorf("update scenario template: %w", err)
		}
	}
	var snapshotAllowed bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM accounts a JOIN portfolio_snapshots ps ON ps.portfolio_id=a.portfolio_id
		WHERE a.id=$1::uuid AND ps.id=$2::uuid)`, input.AccountID, input.SnapshotID).Scan(&snapshotAllowed); err != nil {
		return Run{}, fmt.Errorf("validate account snapshot ownership: %w", err)
	}
	if !snapshotAllowed {
		return Run{}, ErrInvalidRequest
	}
	var snapshotPositionCount int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM portfolio_snapshot_lines WHERE snapshot_id=$1::uuid AND account_id=$2::uuid`, input.SnapshotID, input.AccountID).Scan(&snapshotPositionCount); err != nil {
		return Run{}, fmt.Errorf("count scenario snapshot positions: %w", err)
	}
	if snapshotPositionCount != len(input.Positions) {
		return Run{}, ErrInvalidRequest
	}
	seenLines := make(map[string]struct{}, len(input.Positions))
	for _, position := range input.Positions {
		lineID, lineOK := position["snapshot_line_id"].(string)
		instrumentID, instrumentOK := position["instrument_id"].(string)
		if !lineOK || !instrumentOK || lineID == "" || instrumentID == "" {
			return Run{}, ErrInvalidRequest
		}
		if _, duplicate := seenLines[lineID]; duplicate {
			return Run{}, ErrInvalidRequest
		}
		seenLines[lineID] = struct{}{}
		var lineAllowed bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM portfolio_snapshot_lines
			WHERE id=$1::uuid AND snapshot_id=$2::uuid AND account_id=$3::uuid AND instrument_id=$4::uuid
		)`, lineID, input.SnapshotID, input.AccountID, instrumentID).Scan(&lineAllowed); err != nil {
			return Run{}, fmt.Errorf("validate scenario position snapshot membership: %w", err)
		}
		if !lineAllowed {
			return Run{}, ErrInvalidRequest
		}
	}
	var version int
	if err = tx.QueryRow(ctx, `UPDATE scenarios SET current_version=current_version+1 WHERE id=$1::uuid RETURNING current_version`, input.ScenarioID).Scan(&version); err != nil {
		return Run{}, fmt.Errorf("advance scenario version: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO scenario_versions (scenario_id,version,template_key,units,shocks,mappings,assumptions,content_hash) VALUES ($1::uuid,$2,$3,$4::jsonb,$5::jsonb,$6::jsonb,$7::jsonb,$8)`, input.ScenarioID, version, input.TemplateKey, mustJSON(input.Units), mustJSON(input.Shocks), mustJSON(input.Mappings), mustJSON(input.Assumptions), hash); err != nil {
		return Run{}, fmt.Errorf("persist immutable scenario version: %w", err)
	}
	var jobID, runID string
	payload, err := json.Marshal(map[string]any{
		"scenario_id": input.ScenarioID,
		"scenario_version": map[string]any{
			"scenario_id": input.ScenarioID, "version": version, "template_key": input.TemplateKey,
			"units": input.Units, "shocks": input.Shocks, "mappings": input.Mappings,
			"assumptions": input.Assumptions,
		},
		"snapshot_id": input.SnapshotID, "positions": input.Positions, "pre_metrics": preMetrics,
	})
	if err != nil {
		return Run{}, fmt.Errorf("encode scenario job payload: %w", err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO risk_jobs (kind,schema_version,idempotency_key,input_snapshot_ids,payload) VALUES ('scenario.revalue','1.0',$1,ARRAY[$2]::text[],$3::jsonb) RETURNING id::text`, input.IdempotencyKey, input.SnapshotID, payload).Scan(&jobID)
	if err != nil {
		return Run{}, fmt.Errorf("enqueue scenario revaluation: %w", err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO scenario_runs (scenario_id,scenario_version,account_id,snapshot_id,job_id,request_hash) VALUES ($1::uuid,$2,$3::uuid,$4::uuid,$5::uuid,$6) RETURNING id::text`, input.ScenarioID, version, input.AccountID, input.SnapshotID, jobID, requestHash).Scan(&runID)
	if err != nil {
		return Run{}, fmt.Errorf("create scenario run: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Run{}, fmt.Errorf("commit scenario version and run: %w", err)
	}
	return Run{ID: runID, ScenarioID: input.ScenarioID, ScenarioVer: version, JobID: jobID, ContentSHA256: hash}, nil
}

func mustJSON(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}
