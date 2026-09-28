// Package scenarios versions account-scoped stress scenarios and queues revaluations.
package scenarios

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalidRequest = errors.New("invalid scenario request")

type Service struct{ Pool *pgxpool.Pool }

type VersionInput struct {
	AccountID      string         `json:"account_id"`
	SnapshotID     string         `json:"snapshot_id"`
	ValuationID    string         `json:"valuation_id"`
	ScenarioID     string         `json:"scenario_id,omitempty"`
	Name           string         `json:"name"`
	TemplateKey    string         `json:"template_key"`
	IdempotencyKey string         `json:"idempotency_key"`
	Units          map[string]any `json:"units"`
	Shocks         map[string]any `json:"shocks"`
	Mappings       map[string]any `json:"mappings"`
	Assumptions    map[string]any `json:"assumptions"`
}

type Run struct {
	ID            string `json:"id"`
	ScenarioID    string `json:"scenario_id"`
	ScenarioVer   int    `json:"scenario_version"`
	JobID         string `json:"job_id"`
	ContentSHA256 string `json:"content_sha256"`
}

func valid(input VersionInput) bool {
	if input.AccountID == "" || input.SnapshotID == "" || input.ValuationID == "" || strings.TrimSpace(input.Name) == "" || input.IdempotencyKey == "" {
		return false
	}
	if input.TemplateKey != "try_depreciation" && input.TemplateKey != "rates_up" && input.TemplateKey != "risk_off" {
		return false
	}
	return input.Units != nil && input.Shocks != nil && input.Mappings != nil && input.Assumptions != nil
}

type sealedValuation struct {
	Positions  []map[string]any
	Provenance map[string]any
	MetricInputs map[string]any
}

// preShockMetricsUnavailable is the fail-closed marker used when the sealed
// valuation cannot produce the AR-302 history bundle. The worker replaces it
// only when metric_inputs contains complete point-in-time histories.
func preShockMetricsUnavailable() map[string]any {
	return map[string]any{
		"data_quality": "blocked",
		"reason":       "PRE_SHOCK_METRICS_INPUT_HISTORY_UNAVAILABLE",
		"metric_engine": "AR-302",
		"missing_inputs": []string{
			"point_in_time_price_history",
			"point_in_time_nav_history",
			"signed_exposures",
		},
	}
}

func loadSealedValuation(ctx context.Context, tx pgx.Tx, accountID, snapshotID, valuationID string) (sealedValuation, error) {
	var (
		valuationSnapshot, valuationState, knowledgeMode, cutoff, knownAt, resultHash string
		priceMaxAge, fxMaxAge                                                         int64
	)
	err := tx.QueryRow(ctx, `
		SELECT vr.snapshot_id::text, vr.state, vr.cutoff::text, vr.knowledge_mode,
		       vr.known_at::text, vr.price_max_age_seconds, vr.fx_max_age_seconds,
		       btrim(vr.result_hash)
		FROM valuation_runs vr
		JOIN portfolio_snapshots ps ON ps.id=vr.snapshot_id
		JOIN accounts a ON a.portfolio_id=ps.portfolio_id AND a.id=$1::uuid
		WHERE vr.id=$2::uuid
		FOR SHARE`, accountID, valuationID).Scan(
		&valuationSnapshot, &valuationState, &cutoff, &knowledgeMode, &knownAt,
		&priceMaxAge, &fxMaxAge, &resultHash,
	)
	if errors.Is(err, pgx.ErrNoRows) || valuationSnapshot != snapshotID || valuationState != "valid" || resultHash == "" {
		return sealedValuation{}, ErrInvalidRequest
	}
	if err != nil {
		return sealedValuation{}, fmt.Errorf("load sealed valuation: %w", err)
	}
	var expectedLines int
	if err := tx.QueryRow(ctx, `
		SELECT count(*)
		FROM portfolio_snapshot_lines
		WHERE snapshot_id=$1::uuid AND account_id=$2::uuid`, snapshotID, accountID).Scan(&expectedLines); err != nil {
		return sealedValuation{}, fmt.Errorf("count sealed snapshot lines: %w", err)
	}
	if expectedLines == 0 {
		return sealedValuation{}, ErrInvalidRequest
	}

	rows, err := tx.Query(ctx, `
		SELECT vl.snapshot_line_id::text, sl.instrument_id::text, i.instrument_type,
		       i.native_currency, sl.quantity::text, sl.modified_duration_years::text,
		       sl.convexity_years_squared::text, vl.try_amount::text, vl.usd_amount::text,
		       vl.state, vl.reason_codes, vl.price_method, vl.price_revision_id::text,
		       vl.price_quote_unit, vl.try_fx_quote_revision_ids, vl.try_fx_directions,
		       vl.usd_fx_quote_revision_ids, vl.usd_fx_directions
		FROM valuation_lines vl
		JOIN portfolio_snapshot_lines sl ON sl.id=vl.snapshot_line_id
		JOIN instruments i ON i.id=sl.instrument_id
		WHERE vl.run_id=$1::uuid AND sl.snapshot_id=$2::uuid AND sl.account_id=$3::uuid
		ORDER BY vl.snapshot_line_id`, valuationID, snapshotID, accountID)
	if err != nil {
		return sealedValuation{}, fmt.Errorf("load sealed valuation lines: %w", err)
	}
	defer rows.Close()
	positions := make([]map[string]any, 0)
	provenanceLines := make([]map[string]any, 0)
	metricPrices := make(map[string]map[string]string)
	metricQuantities := make(map[string]string)
	metricExposures := make(map[string]string)
	metricCalendar := "crypto_daily"
	metricInputsAvailable := true
	for rows.Next() {
		var lineID, instrumentID, instrumentType, nativeCurrency, state, priceMethod, quantity string
		var duration, convexity, tryAmount, usdAmount, priceID, quoteUnit *string
		var reasonsRaw []byte
		var tryIDs, usdIDs []pgtype.UUID
		var tryDirections, usdDirections []string
		if err := rows.Scan(&lineID, &instrumentID, &instrumentType, &nativeCurrency, &quantity, &duration, &convexity, &tryAmount, &usdAmount, &state, &reasonsRaw, &priceMethod, &priceID, &quoteUnit, &tryIDs, &tryDirections, &usdIDs, &usdDirections); err != nil {
			return sealedValuation{}, fmt.Errorf("scan sealed valuation line: %w", err)
		}
		var reasons []string
		if len(reasonsRaw) > 0 && string(reasonsRaw) != "null" {
			if err := json.Unmarshal(reasonsRaw, &reasons); err != nil {
				return sealedValuation{}, fmt.Errorf("decode sealed valuation reasons: %w", err)
			}
		}
		if state != "valid" || tryAmount == nil || usdAmount == nil {
			return sealedValuation{}, ErrInvalidRequest
		}
		tryPairs, err := loadFXPairs(ctx, tx, tryIDs)
		if err != nil {
			return sealedValuation{}, fmt.Errorf("load TRY FX provenance: %w", err)
		}
		usdPairs, err := loadFXPairs(ctx, tx, usdIDs)
		if err != nil {
			return sealedValuation{}, fmt.Errorf("load USD FX provenance: %w", err)
		}
		tryPath := fxPathMaps(tryIDs, tryDirections, tryPairs)
		usdPath := fxPathMaps(usdIDs, usdDirections, usdPairs)
		position := map[string]any{
			"snapshot_line_id": lineID, "instrument_id": instrumentID,
			"instrument_type": instrumentType, "asset_class": assetClass(instrumentType),
			"native_currency": nativeCurrency, "value_try": *tryAmount, "value_usd": *usdAmount,
			"fx_path_to_try": tryPath, "fx_path_to_usd": usdPath,
		}
		if duration != nil {
			position["modified_duration_years"] = *duration
		}
		if convexity != nil {
			position["convexity_years_squared"] = *convexity
		}
		positions = append(positions, position)
		metricExposures[instrumentID] = *usdAmount
		if instrumentType != "crypto_spot" {
			metricCalendar = "business_daily"
		}
		if priceMethod != "revision" || quoteUnit == nil || strings.TrimSpace(*quoteUnit) != "USD" {
			metricInputsAvailable = false
		} else {
			history, historyErr := loadMetricPriceHistory(ctx, tx, instrumentID, *quoteUnit, cutoff, knowledgeMode, knownAt)
			if historyErr != nil {
				return sealedValuation{}, fmt.Errorf("load pre-shock price history: %w", historyErr)
			}
			if len(history) == 0 {
				metricInputsAvailable = false
			} else {
				metricPrices[instrumentID] = history
				metricQuantities[instrumentID] = quantity
			}
		}
		provenanceLines = append(provenanceLines, map[string]any{
			"snapshot_line_id": lineID, "price_method": priceMethod,
			"price_revision_id": priceID, "price_quote_unit": quoteUnit,
			"try_fx_path": fxEvidence(tryIDs, tryDirections, tryPairs), "usd_fx_path": fxEvidence(usdIDs, usdDirections, usdPairs),
			"reason_codes": reasons,
		})
	}
	if err := rows.Err(); err != nil {
		return sealedValuation{}, fmt.Errorf("read sealed valuation lines: %w", err)
	}
	if len(positions) == 0 || len(positions) != expectedLines {
		return sealedValuation{}, ErrInvalidRequest
	}
	metricInputs := map[string]any{}
	if metricInputsAvailable && len(metricPrices) > 0 {
		navHistory, navErr := buildMetricNAV(metricPrices, metricQuantities)
		if navErr == nil && len(navHistory) > 0 {
			metricInputs = map[string]any{
				"price_history": metricPrices, "nav_history": navHistory,
				"signed_exposures": metricExposures, "calendar": metricCalendar,
			}
		}
	}
	return sealedValuation{Positions: positions, MetricInputs: metricInputs, Provenance: map[string]any{
		"valuation_run_id": valuationID, "snapshot_id": snapshotID, "state": valuationState,
		"cutoff": cutoff, "knowledge_mode": knowledgeMode, "known_at": knownAt,
		"price_max_age_seconds": priceMaxAge, "fx_max_age_seconds": fxMaxAge,
		"valuation_result_hash": resultHash, "lines": provenanceLines,
	}}, nil
}

func loadMetricPriceHistory(ctx context.Context, tx pgx.Tx, instrumentID, quoteUnit, cutoff, knowledgeMode, knownAt string) (map[string]string, error) {
	knownClause := "system_known_at <= $4"
	orderClause := "observation_time, system_known_at DESC, id DESC"
	if knowledgeMode == "source_as_of" {
		knownClause = "source_known_at IS NOT NULL AND source_known_at <= $4"
		orderClause = "observation_time, source_known_at DESC, system_known_at DESC, id DESC"
	}
	query := fmt.Sprintf(`SELECT observation_time::date::text, price::text
		FROM (SELECT DISTINCT ON (observation_time) observation_time, price, source_known_at, system_known_at, id
			FROM price_revisions
			WHERE instrument_id=$1::uuid AND quote_currency=$2
			  AND observation_time <= $3::timestamptz
			  AND observation_time >= $3::timestamptz - interval '730 days'
			  AND %s
			ORDER BY %s) AS revisions
		ORDER BY observation_time`, knownClause, orderClause)
	rows, err := tx.Query(ctx, query, instrumentID, quoteUnit, cutoff, knownAt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	history := make(map[string]string)
	for rows.Next() {
		var day, price string
		if err := rows.Scan(&day, &price); err != nil {
			return nil, err
		}
		history[day] = price
	}
	return history, rows.Err()
}

func buildMetricNAV(priceHistory map[string]map[string]string, quantities map[string]string) (map[string]string, error) {
	commonDays := make(map[string]bool)
	first := true
	for instrument, history := range priceHistory {
		if _, ok := quantities[instrument]; !ok || len(history) == 0 {
			return nil, errors.New("metric quantity or price history is missing")
		}
		if first {
			for day := range history {
				commonDays[day] = true
			}
			first = false
			continue
		}
		for day := range commonDays {
			if _, ok := history[day]; !ok {
				delete(commonDays, day)
			}
		}
	}
	if len(commonDays) == 0 {
		return nil, errors.New("metric price histories have no common observations")
	}
	nav := make(map[string]string, len(commonDays))
	for day := range commonDays {
		total := new(big.Rat)
		for instrument, history := range priceHistory {
			quantity, ok := new(big.Rat).SetString(quantities[instrument])
			if !ok {
				return nil, fmt.Errorf("invalid metric quantity for %s", instrument)
			}
			price, ok := new(big.Rat).SetString(history[day])
			if !ok || price.Sign() <= 0 {
				return nil, fmt.Errorf("invalid metric price for %s on %s", instrument, day)
			}
			total.Add(total, new(big.Rat).Mul(quantity, price))
		}
		nav[day] = total.FloatString(18)
	}
	return nav, nil
}

func assetClass(instrumentType string) string {
	switch instrumentType {
	case "crypto_spot":
		return "crypto"
	case "equity_spot":
		return "equity"
	case "fixed_rate_bond":
		return "fixed_rate_bond"
	case "cash", "currency":
		return "cash"
	default:
		return ""
	}
}

func loadFXPairs(ctx context.Context, tx pgx.Tx, ids []pgtype.UUID) (map[string]string, error) {
	pairs := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return pairs, nil
	}
	rows, err := tx.Query(ctx, `SELECT id::text, base_currency || '/' || quote_currency FROM fx_quote_revisions WHERE id = ANY($1::uuid[])`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, pair string
		if err := rows.Scan(&id, &pair); err != nil {
			return nil, err
		}
		pairs[id] = pair
	}
	return pairs, rows.Err()
}

func fxPathMaps(ids []pgtype.UUID, directions []string, pairs map[string]string) []map[string]string {
	path := make([]map[string]string, 0, len(ids))
	for i, id := range ids {
		if id.Valid && i < len(directions) {
			direction := directions[i]
			if direction == "forward" {
				direction = "direct"
			} else if direction == "reverse" {
				direction = "inverse"
			}
			path = append(path, map[string]string{"quote_revision_id": id.String(), "pair": pairs[id.String()], "direction": direction})
		}
	}
	return path
}

func fxEvidence(ids []pgtype.UUID, directions []string, pairs map[string]string) []map[string]string {
	path := make([]map[string]string, 0, len(ids))
	for i, id := range ids {
		if id.Valid && i < len(directions) {
			path = append(path, map[string]string{"quote_revision_id": id.String(), "pair": pairs[id.String()], "direction": directions[i]})
		}
	}
	return path
}

// CreateVersionAndRun stores a new immutable version and enqueues its run atomically.
func (s Service) CreateVersionAndRun(ctx context.Context, input VersionInput) (Run, error) {
	if s.Pool == nil || !valid(input) {
		return Run{}, ErrInvalidRequest
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
		"valuation_id": input.ValuationID,
		"template_key": input.TemplateKey, "units": input.Units, "shocks": input.Shocks,
		"mappings": input.Mappings, "assumptions": input.Assumptions,
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
	sealed, err := loadSealedValuation(ctx, tx, input.AccountID, input.SnapshotID, input.ValuationID)
	if err != nil {
		return Run{}, err
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
	var version int
	if err = tx.QueryRow(ctx, `UPDATE scenarios SET current_version=current_version+1 WHERE id=$1::uuid RETURNING current_version`, input.ScenarioID).Scan(&version); err != nil {
		return Run{}, fmt.Errorf("advance scenario version: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO scenario_versions (scenario_id,version,template_key,units,shocks,mappings,assumptions,content_hash) VALUES ($1::uuid,$2,$3,$4::jsonb,$5::jsonb,$6::jsonb,$7::jsonb,$8)`, input.ScenarioID, version, input.TemplateKey, mustJSON(input.Units), mustJSON(input.Shocks), mustJSON(input.Mappings), mustJSON(input.Assumptions), hash); err != nil {
		return Run{}, fmt.Errorf("persist immutable scenario version: %w", err)
	}
	sealed.Provenance["account_id"] = input.AccountID
	sealed.Provenance["scenario_id"] = input.ScenarioID
	sealed.Provenance["scenario_version"] = version
	sealed.Provenance["contract_version"] = "1.0"
	sealedHashBytes := mustJSON(map[string]any{
		"account_id": input.AccountID, "snapshot_id": input.SnapshotID, "valuation_id": input.ValuationID,
		"scenario_id": input.ScenarioID, "scenario_version": version, "scenario_content_hash": hash,
		"sealed_input": sealed.Provenance, "positions": sealed.Positions,
	})
	sealedDigest := sha256.Sum256(sealedHashBytes)
	sealedInputHash := hex.EncodeToString(sealedDigest[:])
	sealed.Provenance["input_hash"] = sealedInputHash
	var jobID, runID string
	payload, err := json.Marshal(map[string]any{
		"scenario_id": input.ScenarioID,
		"scenario_version": map[string]any{
			"scenario_id": input.ScenarioID, "version": version, "template_key": input.TemplateKey,
			"units": input.Units, "shocks": input.Shocks, "mappings": input.Mappings,
			"assumptions": input.Assumptions,
		},
		"snapshot_id": input.SnapshotID, "valuation_id": input.ValuationID,
		"sealed_input": sealed.Provenance, "positions": sealed.Positions,
		"metric_inputs": sealed.MetricInputs,
		"pre_metrics": preShockMetricsUnavailable(),
	})
	if err != nil {
		return Run{}, fmt.Errorf("encode scenario job payload: %w", err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO risk_jobs (kind,schema_version,idempotency_key,input_snapshot_ids,input_hash,payload) VALUES ('scenario.revalue','1.0',$1,ARRAY[$2]::text[],$3,$4::jsonb) RETURNING id::text`, input.IdempotencyKey, input.SnapshotID, sealedInputHash, payload).Scan(&jobID)
	if err != nil {
		return Run{}, fmt.Errorf("enqueue scenario revaluation: %w", err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO scenario_runs (scenario_id,scenario_version,account_id,snapshot_id,valuation_id,job_id,input_provenance,request_hash) VALUES ($1::uuid,$2,$3::uuid,$4::uuid,$5::uuid,$6::uuid,$7::jsonb,$8) RETURNING id::text`, input.ScenarioID, version, input.AccountID, input.SnapshotID, input.ValuationID, jobID, mustJSON(sealed.Provenance), requestHash).Scan(&runID)
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
