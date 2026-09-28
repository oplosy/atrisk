package evidence

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

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
	manifest := Manifest{SchemaVersion: "1", ContractVersion: ContractVersion, DecisionID: decisionID, AccountID: accountID, References: []Entry{}, RawObjects: []RawRef{}}
	var snapshotID, valuationSnapshot, riskSnapshot, valuationID string
	seen := map[string]bool{}
	rawSeen := map[string]bool{}
	for _, ref := range refs {
		key := ref.Kind + ":" + ref.Reference
		if seen[key] || !validUUID(ref.Reference) {
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
				WHERE p.id=$1::uuid AND a.id=$2::uuid`, ref.Reference, accountID).Scan(&data)
			snapshotID = ref.Reference
		case "valuation_run":
			if valuationID != "" {
				return ErrIncomplete
			}
			valuationID = ref.Reference
			err = tx.QueryRow(ctx, `SELECT jsonb_build_object('run',to_jsonb(v),'lines',
				(SELECT COALESCE(jsonb_agg(to_jsonb(l) ORDER BY l.id),'[]'::jsonb) FROM valuation_lines l WHERE l.run_id=v.id))
				FROM valuation_runs v JOIN portfolio_snapshots p ON p.id=v.snapshot_id
				JOIN accounts a ON a.portfolio_id=p.portfolio_id
				WHERE v.id=$1::uuid AND a.id=$2::uuid AND v.state IN ('valid','degraded')`, ref.Reference, accountID).Scan(&data)
			if err == nil {
				err = tx.QueryRow(ctx, `SELECT snapshot_id::text FROM valuation_runs WHERE id=$1::uuid`, ref.Reference).Scan(&valuationSnapshot)
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
				AND NULLIF(j.result->>'engine_version','') IS NOT NULL`, ref.Reference, accountID).Scan(&data)
			if err == nil {
				err = tx.QueryRow(ctx, `SELECT snapshot_id::text FROM scenario_runs WHERE id=$1::uuid`, ref.Reference).Scan(&riskSnapshot)
			}
		case "raw_object":
			var raw RawRef
			err = tx.QueryRow(ctx, `SELECT id::text,object_key,btrim(content_sha256) FROM raw_objects WHERE id=$1::uuid`, ref.Reference).Scan(&raw.ID, &raw.Key, &raw.SHA256)
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
			err = tx.QueryRow(ctx, query, ref.Reference).Scan(&data, &raw.ID, &raw.Key, &raw.SHA256)
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
		manifest.References = append(manifest.References, Entry{Kind: ref.Kind, Reference: ref.Reference, Description: ref.Description, Snapshot: data})
	}
	if snapshotID == "" || valuationSnapshot != snapshotID || riskSnapshot != snapshotID {
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

// Reconstruct returns the original bytes, never a latest-value projection.
func (s Service) Reconstruct(ctx context.Context, decisionID string) (Sealed, error) {
	if s.Pool == nil || !validUUID(decisionID) {
		return Sealed{}, ErrIncomplete
	}
	var data []byte
	var hash string
	err := s.Pool.QueryRow(ctx, `SELECT manifest_bytes,btrim(manifest_sha256) FROM decision_evidence WHERE decision_id=$1::uuid`, decisionID).Scan(&data, &hash)
	if err != nil {
		return Sealed{}, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != hash {
		return Sealed{}, ErrIntegrity
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.DecisionID != decisionID {
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

func validUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}
