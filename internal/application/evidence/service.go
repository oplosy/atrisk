package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oplosy/atrisk/internal/archive"
	domain "github.com/oplosy/atrisk/internal/domain/journal"
)

var (
	ErrIncomplete = errors.New("decision evidence incomplete")
	ErrIntegrity  = errors.New("decision evidence integrity failure")
)

// ContractVersion is checked against the OpenAPI contract in contract tests.
const ContractVersion = "1.0.0"

type Manifest struct {
	SchemaVersion   string   `json:"schema_version"`
	ContractVersion string   `json:"contract_version"`
	DecisionID      string   `json:"decision_id"`
	AccountID       string   `json:"account_id"`
	References      []Entry  `json:"references"`
	RawObjects      []RawRef `json:"raw_objects"`
}

type Entry struct {
	Kind        string          `json:"kind"`
	Reference   string          `json:"reference"`
	Description string          `json:"description,omitempty"`
	Snapshot    json.RawMessage `json:"snapshot"`
}

type RawRef struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	SHA256 string `json:"sha256"`
}

type Sealed struct {
	Manifest json.RawMessage `json:"manifest"`
	SHA256   string          `json:"sha256"`
}

type Service struct {
	Pool    *pgxpool.Pool
	Archive archive.Store
}

// Seal must run within the caller's decision-row transaction. Every required
// artifact is checked before inserting the immutable manifest.
func (s Service) Seal(ctx context.Context, tx pgx.Tx, decisionID, accountID string, refs []domain.EvidenceRef) error {
	canonicalDecisionID, ok := canonicalUUID(decisionID)
	if !ok {
		return ErrIncomplete
	}
	canonicalAccountID, ok := canonicalUUID(accountID)
	if !ok {
		return ErrIncomplete
	}
	manifest := Manifest{SchemaVersion: "1", ContractVersion: ContractVersion, DecisionID: canonicalDecisionID, AccountID: canonicalAccountID, References: []Entry{}, RawObjects: []RawRef{}}
	var snapshotID, valuationSnapshot, riskSnapshot, valuationID, riskValuationID string
	seen := map[string]bool{}
	rawSeen := map[string]bool{}
	for _, ref := range refs {
		canonicalReference, ok := canonicalUUID(ref.Reference)
		if !ok {
			return ErrIncomplete
		}
		key := ref.Kind + ":" + canonicalReference
		if seen[key] {
			return ErrIncomplete
		}
		seen[key] = true
		var data []byte
		var err error
		switch ref.Kind {
		case "portfolio_snapshot":
			if snapshotID != "" {
				return ErrIncomplete
			}
			err = tx.QueryRow(ctx, `SELECT jsonb_build_object('snapshot',to_jsonb(p),'lines',
				(SELECT COALESCE(jsonb_agg(to_jsonb(l) ORDER BY l.id),'[]'::jsonb) FROM portfolio_snapshot_lines l WHERE l.snapshot_id=p.id))
				FROM portfolio_snapshots p JOIN accounts a ON a.portfolio_id=p.portfolio_id
				WHERE p.id=$1::uuid AND a.id=$2::uuid`, canonicalReference, canonicalAccountID).Scan(&data)
			snapshotID = canonicalReference
		case "valuation_run":
			if valuationID != "" {
				return ErrIncomplete
			}
			valuationID = canonicalReference
			err = tx.QueryRow(ctx, `SELECT jsonb_build_object('run',to_jsonb(v),'lines',
				(SELECT COALESCE(jsonb_agg(to_jsonb(l) ORDER BY l.id),'[]'::jsonb) FROM valuation_lines l WHERE l.run_id=v.id))
				FROM valuation_runs v JOIN portfolio_snapshots p ON p.id=v.snapshot_id
				JOIN accounts a ON a.portfolio_id=p.portfolio_id
				WHERE v.id=$1::uuid AND a.id=$2::uuid AND v.state IN ('valid','degraded')`, canonicalReference, canonicalAccountID).Scan(&data)
			if err == nil {
				err = tx.QueryRow(ctx, `SELECT snapshot_id::text FROM valuation_runs WHERE id=$1::uuid`, canonicalReference).Scan(&valuationSnapshot)
			}
		case "risk_run":
			if riskSnapshot != "" {
				return ErrIncomplete
			}
			err = tx.QueryRow(ctx, `SELECT jsonb_build_object('run',to_jsonb(r),'scenario_version',to_jsonb(v),'job',to_jsonb(j),
				'positions',(SELECT COALESCE(jsonb_agg(to_jsonb(p) ORDER BY p.snapshot_line_id),'[]'::jsonb) FROM scenario_run_positions p WHERE p.run_id=r.id),
				'metrics',(SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY m.metric_key),'[]'::jsonb) FROM scenario_run_metrics m WHERE m.run_id=r.id))
				FROM scenario_runs r JOIN scenario_versions v ON (v.scenario_id,v.version)=(r.scenario_id,r.scenario_version)
				JOIN risk_jobs j ON j.id=r.job_id
				WHERE r.id=$1::uuid AND r.account_id=$2::uuid AND r.state IN ('valid','degraded')
				AND r.result IS NOT NULL AND r.result_hash IS NOT NULL AND r.completed_at IS NOT NULL
				AND j.state='succeeded' AND j.result IS NOT NULL AND j.result_hash IS NOT NULL
				AND NULLIF(j.result->>'engine_version','') IS NOT NULL`, canonicalReference, canonicalAccountID).Scan(&data)
			if err == nil {
				err = tx.QueryRow(ctx, `SELECT snapshot_id::text,COALESCE(valuation_id::text,'') FROM scenario_runs WHERE id=$1::uuid`, canonicalReference).Scan(&riskSnapshot, &riskValuationID)
			}
		case "raw_object":
			var raw RawRef
			err = tx.QueryRow(ctx, `SELECT id::text,object_key,btrim(content_sha256) FROM raw_objects WHERE id=$1::uuid`, canonicalReference).Scan(&raw.ID, &raw.Key, &raw.SHA256)
			if err == nil {
				err = s.verifyRaw(ctx, raw)
				if err == nil && !rawSeen[raw.ID] {
					manifest.RawObjects = append(manifest.RawObjects, raw)
					rawSeen[raw.ID] = true
				}
				data, _ = json.Marshal(raw)
			}
		case "observation_revision", "price_revision", "fx_quote_revision":
			query := map[string]string{
				"observation_revision": `SELECT to_jsonb(r),ro.id::text,ro.object_key,btrim(ro.content_sha256) FROM observation_revisions r JOIN raw_objects ro ON ro.id=r.raw_object_id WHERE r.id=$1::uuid`,
				"price_revision":       `SELECT to_jsonb(r),ro.id::text,ro.object_key,btrim(ro.content_sha256) FROM price_revisions r JOIN raw_objects ro ON ro.id=r.raw_object_id WHERE r.id=$1::uuid`,
				"fx_quote_revision":    `SELECT to_jsonb(r),ro.id::text,ro.object_key,btrim(ro.content_sha256) FROM fx_quote_revisions r JOIN raw_objects ro ON ro.id=r.raw_object_id WHERE r.id=$1::uuid`,
			}[ref.Kind]
			var raw RawRef
			err = tx.QueryRow(ctx, query, canonicalReference).Scan(&data, &raw.ID, &raw.Key, &raw.SHA256)
			if err == nil && !rawSeen[raw.ID] {
				err = s.verifyRaw(ctx, raw)
				if err == nil {
					manifest.RawObjects = append(manifest.RawObjects, raw)
					rawSeen[raw.ID] = true
				}
			}
		default:
			return ErrIncomplete
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrIncomplete
		}
		if err != nil {
			return fmt.Errorf("resolve %s evidence: %w", ref.Kind, err)
		}
		if ref.Kind == "risk_run" {
			if err := s.addMetricDependencies(ctx, tx, data, &manifest, rawSeen); err != nil {
				return err
			}
		}
		manifest.References = append(manifest.References, Entry{Kind: ref.Kind, Reference: canonicalReference, Description: ref.Description, Snapshot: data})
	}
	if snapshotID == "" || valuationSnapshot != snapshotID || riskSnapshot != snapshotID || riskValuationID == "" || valuationID == "" || riskValuationID != valuationID {
		return ErrIncomplete
	}
	// Valuation quote paths are data-version dependencies, even when the draft
	// did not list their raw source objects explicitly.
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT ro.id::text,ro.object_key,btrim(ro.content_sha256)
		FROM valuation_lines l JOIN price_revisions p ON p.id=l.price_revision_id
		JOIN raw_objects ro ON ro.id=p.raw_object_id WHERE l.run_id=$1::uuid
		UNION
		SELECT DISTINCT ro.id::text,ro.object_key,btrim(ro.content_sha256)
		FROM valuation_lines l JOIN fx_quote_revisions f ON f.id=ANY(l.try_fx_quote_revision_ids || l.usd_fx_quote_revision_ids)
		JOIN raw_objects ro ON ro.id=f.raw_object_id WHERE l.run_id=$1::uuid
		ORDER BY 1`, valuationID)
	if err != nil {
		return fmt.Errorf("list valuation raw evidence: %w", err)
	}
	for rows.Next() {
		var raw RawRef
		if err := rows.Scan(&raw.ID, &raw.Key, &raw.SHA256); err != nil {
			rows.Close()
			return err
		}
		if !rawSeen[raw.ID] {
			if err := s.verifyRaw(ctx, raw); err != nil {
				rows.Close()
				return ErrIntegrity
			}
			manifest.RawObjects = append(manifest.RawObjects, raw)
			rawSeen[raw.ID] = true
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encode decision manifest: %w", err)
	}
	sum := sha256.Sum256(data)
	if _, err := tx.Exec(ctx, `INSERT INTO decision_evidence (decision_id,manifest_bytes,manifest_sha256) VALUES ($1::uuid,$2,$3)`, decisionID, data, hex.EncodeToString(sum[:])); err != nil {
		return fmt.Errorf("seal decision evidence: %w", err)
	}
	return nil
}

// addMetricDependencies closes the evidence graph for the historical metric
// inputs embedded in a sealed risk run. The metric bundle is immutable input
// provenance, so every revision identity must still match its database row and
// its raw source must be present and byte-identical in the archive.
func (s Service) addMetricDependencies(ctx context.Context, tx pgx.Tx, riskSnapshot []byte, manifest *Manifest, rawSeen map[string]bool) error {
	var envelope struct {
		Run struct {
			InputProvenance json.RawMessage `json:"input_provenance"`
		} `json:"run"`
	}
	if err := json.Unmarshal(riskSnapshot, &envelope); err != nil {
		return ErrIncomplete
	}
	if len(envelope.Run.InputProvenance) == 0 || string(envelope.Run.InputProvenance) == "null" {
		return nil
	}
	var provenance map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Run.InputProvenance, &provenance); err != nil {
		return ErrIncomplete
	}
	metricInputsRaw, ok := provenance["metric_inputs"]
	if !ok {
		return nil
	}
	if string(metricInputsRaw) == "null" {
		return ErrIncomplete
	}
	var metricInputs map[string]json.RawMessage
	if err := json.Unmarshal(metricInputsRaw, &metricInputs); err != nil {
		return ErrIncomplete
	}
	priceHistoryRaw, hasPriceHistory := metricInputs["price_history"]
	prices := make(map[string]map[string]any)
	if hasPriceHistory && string(priceHistoryRaw) == "null" {
		return ErrIncomplete
	}
	if hasPriceHistory && !emptyJSONObject(priceHistoryRaw) {
		if err := json.Unmarshal(priceHistoryRaw, &prices); err != nil {
			return ErrIncomplete
		}
	}
	coveredInstruments := make(map[string]bool)
	expectedHistories := make(map[string]map[string]string)
	instrumentKinds := make(map[string]metricInstrument)
	for _, group := range []struct {
		key       string
		cashGroup bool
	}{
		{key: "price_revision_history"},
		{key: "cash_revision_history", cashGroup: true},
	} {
		historyRaw, present := metricInputs[group.key]
		if !present {
			continue
		}
		if string(historyRaw) == "null" {
			return ErrIncomplete
		}
		if emptyJSONObject(historyRaw) {
			continue
		}
		var histories map[string][]map[string]any
		if err := json.Unmarshal(historyRaw, &histories); err != nil {
			return ErrIncomplete
		}
		instrumentIDs := make([]string, 0, len(histories))
		for instrumentID := range histories {
			instrumentIDs = append(instrumentIDs, instrumentID)
		}
		sort.Strings(instrumentIDs)
		for _, instrumentID := range instrumentIDs {
			revisions := histories[instrumentID]
			if _, ok := canonicalUUID(instrumentID); !ok {
				return ErrIncomplete
			}
			if group.cashGroup {
				instrument, err := loadMetricInstrument(ctx, tx, instrumentID, instrumentKinds)
				if err != nil || (instrument.Kind != "cash" && instrument.Kind != "currency") {
					return ErrIncomplete
				}
				if instrument.Native == "" {
					return ErrIncomplete
				}
			}
			if len(revisions) == 0 {
				return ErrIncomplete
			}
			if _, exists := expectedHistories[instrumentID]; exists {
				return ErrIncomplete
			}
			expectedHistory := make(map[string]string, len(revisions))
			coveredInstruments[instrumentID] = true
			for _, revision := range revisions {
				if group.cashGroup && strings.HasPrefix(metricString(revision, "id"), "cash-constant:") {
					instrument := instrumentKinds[instrumentID]
					if instrument.Native != "USD" || !validConstantCashRevision(instrumentID, revision) {
						return ErrIncomplete
					}
					day, ok := metricObservationDay(revision)
					if !ok {
						return ErrIncomplete
					}
					expectedHistory[day] = metricString(revision, "usd_price")
					continue
				}
				var raw RawRef
				var err error
				if group.cashGroup {
					instrument := instrumentKinds[instrumentID]
					if strings.ToUpper(metricString(revision, "price_quote_currency")) != instrument.Native {
						return ErrIncomplete
					}
					raw, err = s.verifyMetricCashPriceRevision(ctx, tx, instrumentID, revision, metricString(revision, "id"))
				} else {
					raw, err = s.verifyMetricPriceRevision(ctx, tx, instrumentID, revision)
				}
				if err != nil {
					dependencyKind := "price"
					if group.cashGroup {
						dependencyKind = "cash"
					}
					return fmt.Errorf("verify metric %s dependency: %w", dependencyKind, err)
				}
				if err := s.addRawDependency(ctx, raw, manifest, rawSeen); err != nil {
					return fmt.Errorf("archive metric price dependency: %w", err)
				}
				fxPath, ok := revision["fx_path"]
				if !ok || fxPath == nil {
					return ErrIncomplete
				}
				fxPathBytes, err := json.Marshal(fxPath)
				if err != nil {
					return ErrIncomplete
				}
				var fxRevisions []map[string]any
				if err := json.Unmarshal(fxPathBytes, &fxRevisions); err != nil {
					return ErrIncomplete
				}
				if err := validateMetricUSDConversion(revision, fxRevisions); err != nil {
					return err
				}
				for _, fxRevision := range fxRevisions {
					fxRaw, err := s.verifyMetricFXRevision(ctx, tx, fxRevision)
					if err != nil {
						return fmt.Errorf("verify metric FX dependency: %w", err)
					}
					if err := s.addRawDependency(ctx, fxRaw, manifest, rawSeen); err != nil {
						return fmt.Errorf("archive metric FX dependency: %w", err)
					}
				}
				day, ok := metricObservationDay(revision)
				if !ok {
					return ErrIncomplete
				}
				expectedHistory[day] = metricString(revision, "usd_price")
			}
			expectedHistories[instrumentID] = expectedHistory
		}
	}
	if len(coveredInstruments) == 0 {
		if len(prices) == 0 {
			return nil
		}
		return ErrIncomplete
	}
	if len(prices) != len(coveredInstruments) {
		return ErrIncomplete
	}
	for instrumentID := range prices {
		if !coveredInstruments[instrumentID] {
			return ErrIncomplete
		}
	}
	for instrumentID, expected := range expectedHistories {
		actual, ok := prices[instrumentID]
		if !ok || len(actual) != len(expected) {
			return ErrIncomplete
		}
		for day, value := range expected {
			actualValue, ok := actual[day].(string)
			if !ok || actualValue != value {
				return ErrIncomplete
			}
		}
	}
	return nil
}

func emptyJSONObject(raw json.RawMessage) bool {
	var value map[string]any
	return json.Unmarshal(raw, &value) == nil && len(value) == 0
}

type metricInstrument struct {
	Kind   string
	Native string
}

func loadMetricInstrument(ctx context.Context, tx pgx.Tx, instrumentID string, cache map[string]metricInstrument) (metricInstrument, error) {
	if instrument, ok := cache[instrumentID]; ok {
		return instrument, nil
	}
	var instrument metricInstrument
	err := tx.QueryRow(ctx, `SELECT instrument_type,btrim(native_currency) FROM instruments WHERE id=$1::uuid`, instrumentID).Scan(&instrument.Kind, &instrument.Native)
	if err != nil {
		return metricInstrument{}, fmt.Errorf("resolve metric instrument: %w", err)
	}
	instrument.Kind = strings.ToLower(strings.TrimSpace(instrument.Kind))
	instrument.Native = strings.ToUpper(strings.TrimSpace(instrument.Native))
	cache[instrumentID] = instrument
	return instrument, nil
}

func metricObservationDay(entry map[string]any) (string, bool) {
	observation, ok := entry["observation_time"].(string)
	if !ok {
		return "", false
	}
	parsed, err := time.Parse(time.RFC3339Nano, observation)
	if err != nil {
		return "", false
	}
	return parsed.UTC().Format("2006-01-02"), true
}

func validateMetricUSDConversion(entry map[string]any, fxRevisions []map[string]any) error {
	price, ok := new(big.Rat).SetString(metricString(entry, "price"))
	if !ok || price.Sign() <= 0 {
		return ErrIncomplete
	}
	usdPrice, ok := new(big.Rat).SetString(metricString(entry, "usd_price"))
	if !ok || usdPrice.Sign() <= 0 {
		return ErrIncomplete
	}
	currency := strings.ToUpper(metricString(entry, "price_quote_currency"))
	if currency == "" {
		return ErrIncomplete
	}
	expected := new(big.Rat).Set(price)
	if currency != "USD" {
		if len(fxRevisions) == 0 {
			return ErrIncomplete
		}
		current := currency
		for _, revision := range fxRevisions {
			from, to, ok := metricFXStep(revision)
			if !ok || from != current {
				return ErrIncomplete
			}
			rate, ok := new(big.Rat).SetString(metricString(revision, "rate"))
			if !ok || rate.Sign() <= 0 {
				return ErrIncomplete
			}
			if metricString(revision, "direction") == "direct" {
				expected.Mul(expected, rate)
			} else {
				expected.Quo(expected, rate)
			}
			current = to
		}
		if current != "USD" {
			return ErrIncomplete
		}
	} else if len(fxRevisions) != 0 {
		return ErrIncomplete
	}
	roundedExpected, ok := new(big.Rat).SetString(expected.FloatString(18))
	if !ok || roundedExpected.Cmp(usdPrice) != 0 {
		return ErrIncomplete
	}
	return nil
}

func metricFXStep(entry map[string]any) (string, string, bool) {
	parts := strings.Split(metricString(entry, "pair"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	base, quote := strings.ToUpper(strings.TrimSpace(parts[0])), strings.ToUpper(strings.TrimSpace(parts[1]))
	switch metricString(entry, "direction") {
	case "direct":
		return base, quote, true
	case "inverse":
		return quote, base, true
	default:
		return "", "", false
	}
}

func validConstantCashRevision(instrumentID string, entry map[string]any) bool {
	instrumentID, ok := canonicalUUID(instrumentID)
	if !ok {
		return false
	}
	constantID := "cash-constant:" + instrumentID + ":"
	if !strings.HasPrefix(metricString(entry, "id"), constantID) {
		return false
	}
	day := strings.TrimPrefix(metricString(entry, "id"), constantID)
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return false
	}
	observation, ok := entry["observation_time"].(string)
	if !ok {
		return false
	}
	parsed, err := time.Parse(time.RFC3339Nano, observation)
	if err != nil || parsed.UTC().Format("2006-01-02") != day {
		return false
	}
	return metricString(entry, "price") == "1" && metricString(entry, "price_quote_currency") == "USD" && metricString(entry, "usd_price") == "1" && metricString(entry, "knowledge_time_basis") == "constant_cash" && metricNullableTimestamp(entry, "source_known_at", nil) && metricTimestamp(entry, "system_known_at", time.Time{}) && lenMetricFXPath(entry) == 0
}

func lenMetricFXPath(entry map[string]any) int {
	path, ok := entry["fx_path"]
	if !ok || path == nil {
		return -1
	}
	data, err := json.Marshal(path)
	if err != nil {
		return -1
	}
	var revisions []map[string]any
	if json.Unmarshal(data, &revisions) != nil {
		return -1
	}
	return len(revisions)
}

func (s Service) addRawDependency(ctx context.Context, raw RawRef, manifest *Manifest, rawSeen map[string]bool) error {
	if rawSeen[raw.ID] {
		return nil
	}
	if err := s.verifyRaw(ctx, raw); err != nil {
		return ErrIntegrity
	}
	manifest.RawObjects = append(manifest.RawObjects, raw)
	rawSeen[raw.ID] = true
	return nil
}

func (s Service) verifyMetricPriceRevision(ctx context.Context, tx pgx.Tx, instrumentID string, entry map[string]any) (RawRef, error) {
	revisionID, ok := metricUUID(entry, "id")
	if !ok {
		return RawRef{}, ErrIncomplete
	}
	var (
		rowID, rowInstrumentID, price, quoteCurrency, knowledgeBasis string
		observationTime, systemKnownAt                               time.Time
		sourceKnownAt                                                *time.Time
		raw                                                          RawRef
	)
	err := tx.QueryRow(ctx, `SELECT p.id::text,p.instrument_id::text,p.observation_time,p.price::text,p.quote_currency,
		p.source_known_at,p.system_known_at,p.knowledge_time_basis,ro.id::text,ro.object_key,btrim(ro.content_sha256)
		FROM price_revisions p JOIN raw_objects ro ON ro.id=p.raw_object_id WHERE p.id=$1::uuid`, revisionID).Scan(
		&rowID, &rowInstrumentID, &observationTime, &price, &quoteCurrency, &sourceKnownAt, &systemKnownAt,
		&knowledgeBasis, &raw.ID, &raw.Key, &raw.SHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RawRef{}, ErrIncomplete
	}
	if err != nil {
		return RawRef{}, fmt.Errorf("resolve metric price revision: %w", err)
	}
	canonicalInstrumentID, ok := canonicalUUID(instrumentID)
	if !ok || rowID != revisionID || rowInstrumentID != canonicalInstrumentID {
		return RawRef{}, fmt.Errorf("metric price ownership mismatch row=%s instrument=%s expected=%s/%s: %w", rowID, rowInstrumentID, revisionID, canonicalInstrumentID, ErrIncomplete)
	}
	if !metricTimestamp(entry, "observation_time", observationTime) || metricString(entry, "price") != price || metricString(entry, "price_quote_currency") != strings.TrimSpace(quoteCurrency) || !metricNullableTimestamp(entry, "source_known_at", sourceKnownAt) || !metricTimestamp(entry, "system_known_at", systemKnownAt) || metricString(entry, "knowledge_time_basis") != knowledgeBasis {
		return RawRef{}, fmt.Errorf("metric price identity mismatch entry=%v db=%s/%s/%s/%s/%s: %w", entry, observationTime, price, quoteCurrency, systemKnownAt, knowledgeBasis, ErrIncomplete)
	}
	return raw, nil
}

func (s Service) verifyMetricCashPriceRevision(ctx context.Context, tx pgx.Tx, instrumentID string, entry map[string]any, revisionID string) (RawRef, error) {
	var (
		rowID, baseCurrency, quoteCurrency, rate, knowledgeBasis string
		observationTime, systemKnownAt                           time.Time
		sourceKnownAt                                            *time.Time
		raw                                                      RawRef
	)
	err := tx.QueryRow(ctx, `SELECT f.id::text,f.base_currency,f.quote_currency,f.observation_time,f.rate::text,
		f.source_known_at,f.system_known_at,f.knowledge_time_basis,ro.id::text,ro.object_key,btrim(ro.content_sha256)
		FROM fx_quote_revisions f JOIN raw_objects ro ON ro.id=f.raw_object_id WHERE f.id=$1::uuid`, revisionID).Scan(
		&rowID, &baseCurrency, &quoteCurrency, &observationTime, &rate, &sourceKnownAt, &systemKnownAt,
		&knowledgeBasis, &raw.ID, &raw.Key, &raw.SHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RawRef{}, ErrIncomplete
	}
	if err != nil {
		return RawRef{}, fmt.Errorf("resolve metric cash FX revision: %w", err)
	}
	baseCurrency, quoteCurrency = strings.TrimSpace(baseCurrency), strings.TrimSpace(quoteCurrency)
	if _, ok := canonicalUUID(instrumentID); !ok || rowID != revisionID || metricString(entry, "price") != "1" {
		return RawRef{}, ErrIncomplete
	}
	nativeCurrency := baseCurrency
	if baseCurrency == "USD" {
		nativeCurrency = quoteCurrency
	} else if quoteCurrency != "USD" {
		return RawRef{}, ErrIncomplete
	}
	if metricString(entry, "price_quote_currency") != nativeCurrency || !metricTimestamp(entry, "observation_time", observationTime) || !metricNullableTimestamp(entry, "source_known_at", sourceKnownAt) || !metricTimestamp(entry, "system_known_at", systemKnownAt) || metricString(entry, "knowledge_time_basis") != knowledgeBasis {
		return RawRef{}, ErrIncomplete
	}
	return raw, nil
}

func (s Service) verifyMetricFXRevision(ctx context.Context, tx pgx.Tx, entry map[string]any) (RawRef, error) {
	revisionID, ok := metricUUID(entry, "id")
	if !ok {
		return RawRef{}, ErrIncomplete
	}
	var (
		rowID, baseCurrency, quoteCurrency, rate, knowledgeBasis string
		observationTime, systemKnownAt                           time.Time
		sourceKnownAt                                            *time.Time
		raw                                                      RawRef
	)
	err := tx.QueryRow(ctx, `SELECT f.id::text,f.base_currency,f.quote_currency,f.observation_time,f.rate::text,
		f.source_known_at,f.system_known_at,f.knowledge_time_basis,ro.id::text,ro.object_key,btrim(ro.content_sha256)
		FROM fx_quote_revisions f JOIN raw_objects ro ON ro.id=f.raw_object_id WHERE f.id=$1::uuid`, revisionID).Scan(
		&rowID, &baseCurrency, &quoteCurrency, &observationTime, &rate, &sourceKnownAt, &systemKnownAt,
		&knowledgeBasis, &raw.ID, &raw.Key, &raw.SHA256,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RawRef{}, ErrIncomplete
	}
	if err != nil {
		return RawRef{}, fmt.Errorf("resolve metric FX revision: %w", err)
	}
	baseCurrency, quoteCurrency = strings.TrimSpace(baseCurrency), strings.TrimSpace(quoteCurrency)
	direction := "direct"
	if quoteCurrency == "USD" && baseCurrency != "USD" {
		direction = "direct"
	} else if baseCurrency == "USD" && quoteCurrency != "USD" {
		direction = "inverse"
	} else {
		return RawRef{}, ErrIncomplete
	}
	if rowID != revisionID || metricString(entry, "pair") != baseCurrency+"/"+quoteCurrency || metricString(entry, "direction") != direction || !metricTimestamp(entry, "observation_time", observationTime) || metricString(entry, "rate") != rate || !metricNullableTimestamp(entry, "source_known_at", sourceKnownAt) || !metricTimestamp(entry, "system_known_at", systemKnownAt) || metricString(entry, "knowledge_time_basis") != knowledgeBasis {
		return RawRef{}, ErrIncomplete
	}
	return raw, nil
}

func metricUUID(entry map[string]any, key string) (string, bool) {
	value, ok := entry[key].(string)
	if !ok {
		return "", false
	}
	return canonicalUUID(value)
}

func metricString(entry map[string]any, key string) string {
	value, _ := entry[key].(string)
	return strings.TrimSpace(value)
}

func metricTimestamp(entry map[string]any, key string, expected time.Time) bool {
	value, ok := entry[key].(string)
	if !ok {
		return false
	}
	actual, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && actual.Equal(expected)
}

func metricNullableTimestamp(entry map[string]any, key string, expected *time.Time) bool {
	value, exists := entry[key]
	if !exists || value == nil {
		return expected == nil
	}
	text, ok := value.(string)
	if !ok || expected == nil {
		return false
	}
	actual, err := time.Parse(time.RFC3339Nano, text)
	return err == nil && actual.Equal(*expected)
}

// Reconstruct returns the original bytes, never a latest-value projection.
func (s Service) Reconstruct(ctx context.Context, decisionID string) (Sealed, error) {
	canonicalDecisionID, ok := canonicalUUID(decisionID)
	if s.Pool == nil || !ok {
		return Sealed{}, ErrIncomplete
	}
	var data []byte
	var hash string
	err := s.Pool.QueryRow(ctx, `SELECT manifest_bytes,btrim(manifest_sha256) FROM decision_evidence WHERE decision_id=$1::uuid`, canonicalDecisionID).Scan(&data, &hash)
	if err != nil {
		return Sealed{}, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != hash {
		return Sealed{}, ErrIntegrity
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.DecisionID != canonicalDecisionID {
		return Sealed{}, ErrIntegrity
	}
	for _, raw := range manifest.RawObjects {
		if err := s.verifyRaw(ctx, raw); err != nil {
			return Sealed{}, fmt.Errorf("archived evidence %s: %w", raw.ID, ErrIntegrity)
		}
	}
	return Sealed{Manifest: json.RawMessage(data), SHA256: hash}, nil
}

func (s Service) verifyRaw(ctx context.Context, raw RawRef) error {
	if s.Archive == nil {
		return ErrIntegrity
	}
	reader, err := s.Archive.Get(ctx, raw.Key)
	if err != nil {
		return ErrIntegrity
	}
	defer reader.Close()
	h := sha256.New()
	if _, err := io.Copy(h, reader); err != nil || hex.EncodeToString(h.Sum(nil)) != raw.SHA256 {
		return ErrIntegrity
	}
	return nil
}

func canonicalUUID(value string) (string, bool) {
	if len(value) != 36 {
		return "", false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return "", false
			}
			continue
		}
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return "", false
		}
	}
	return strings.ToLower(value), true
}

func validUUID(value string) bool {
	_, ok := canonicalUUID(value)
	return ok
}
