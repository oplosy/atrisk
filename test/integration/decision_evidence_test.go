package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oplosy/atrisk/internal/application/evidence"
	applicationjournal "github.com/oplosy/atrisk/internal/application/journal"
	"github.com/oplosy/atrisk/internal/archive"
	domain "github.com/oplosy/atrisk/internal/domain/journal"
)

type evidenceArchive struct{ objects map[string][]byte }

func (a *evidenceArchive) Put(context.Context, archive.Object) error { return nil }
func (a *evidenceArchive) Get(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := a.objects[key]
	if !ok {
		return nil, errors.New("object missing")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func decisionEvidenceFixture(t *testing.T, pool *pgxpool.Pool, portfolioID, accountID string) (string, []domain.EvidenceRef) {
	t.Helper()
	ctx := context.Background()
	var snapshotID, valuationID, scenarioID, jobID, riskID string
	if err := pool.QueryRow(ctx, `INSERT INTO portfolio_snapshots (portfolio_id,captured_at) VALUES ($1::uuid,'2026-01-01T00:00:00Z') RETURNING id::text`, portfolioID).Scan(&snapshotID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO valuation_runs (snapshot_id,cutoff,knowledge_mode,known_at,price_max_age_seconds,fx_max_age_seconds,request,state,result_hash)
		VALUES ($1::uuid,'2026-01-01T00:00:00Z','system_as_of','2026-01-01T00:00:00Z',3600,3600,'{}','valid',$2) RETURNING id::text`, snapshotID, strings.Repeat("a", 64)).Scan(&valuationID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO scenarios (account_id,name,template_key) VALUES ($1::uuid,'evidence-'||gen_random_uuid()::text,'rates_up') RETURNING id::text`, accountID).Scan(&scenarioID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scenario_versions (scenario_id,version,template_key,units,shocks,assumptions,content_hash)
		VALUES ($1::uuid,1,'rates_up','{}','{}','{}',$2)`, scenarioID, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO risk_jobs (kind,schema_version,idempotency_key,input_snapshot_ids,payload,state,result,result_hash,completed_at)
		VALUES ('scenario','1',gen_random_uuid()::text,ARRAY[$1]::text[],'{}','succeeded','{"engine_version":"fixture-1"}',$2,clock_timestamp()) RETURNING id::text`, snapshotID, strings.Repeat("c", 64)).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO scenario_runs (scenario_id,scenario_version,account_id,snapshot_id,job_id,state,result,result_hash,request_hash,completed_at)
		VALUES ($1::uuid,1,$2::uuid,$3::uuid,$4::uuid,'valid','{"engine_version":"fixture-1"}',$5,$6,clock_timestamp()) RETURNING id::text`, scenarioID, accountID, snapshotID, jobID, strings.Repeat("d", 64), strings.Repeat("e", 64)).Scan(&riskID); err != nil {
		t.Fatal(err)
	}
	return scenarioID, []domain.EvidenceRef{{Kind: "portfolio_snapshot", Reference: snapshotID}, {Kind: "valuation_run", Reference: valuationID}, {Kind: "risk_run", Reference: riskID}}
}

func TestHistoricalDecisionReconstruction(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	var portfolioID, accountID string
	if err := pool.QueryRow(ctx, `INSERT INTO portfolios (name,reporting_currency) VALUES ('evidence-'||gen_random_uuid()::text,'TRY') RETURNING id::text`).Scan(&portfolioID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO accounts (portfolio_id,name) VALUES ($1::uuid,'decision-evidence') RETURNING id::text`, portfolioID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	scenarioID, refs := decisionEvidenceFixture(t, pool, portfolioID, accountID)
	key := "fixture-" + accountID
	archiveStore := &evidenceArchive{objects: map[string][]byte{key: []byte("source evidence:" + accountID)}}
	content := archiveStore.objects[key]
	sum := sha256.Sum256(content)
	var rawID string
	if err := pool.QueryRow(ctx, `INSERT INTO raw_objects (content_sha256,object_key,media_type,byte_length,retrieved_at) VALUES ($1,$2,'text/plain',$3,clock_timestamp()) RETURNING id::text`, hex.EncodeToString(sum[:]), key, len(content)).Scan(&rawID); err != nil {
		t.Fatal(err)
	}
	refs = append(refs, domain.EvidenceRef{Kind: "raw_object", Reference: rawID})
	for i := range refs {
		refs[i].Reference = strings.ToUpper(refs[i].Reference)
	}
	svc := applicationjournal.Service{Pool: pool, Archive: archiveStore}
	request := domain.CreateRequest{AccountID: strings.ToUpper(accountID), Thesis: "Historical fixture", EvidenceReferences: refs, InvalidationConditions: []domain.Invalidation{{Condition: "Rates change"}}, Horizon: domain.Horizon{Start: mustTime("2026-01-01T00:00:00Z"), End: mustTime("2027-01-01T00:00:00Z")}, RiskBudget: domain.RiskBudget{Amount: "100", Currency: "TRY", Measure: "loss", Horizon: "one_year"}, IntendedAction: "Hold", Author: "fixture"}
	var queuedJobID, queuedRunID string
	if err := pool.QueryRow(ctx, `INSERT INTO risk_jobs (kind,schema_version,idempotency_key,input_snapshot_ids,payload) VALUES ('scenario','1',gen_random_uuid()::text,ARRAY[$1]::text[],'{}') RETURNING id::text`, refs[0].Reference).Scan(&queuedJobID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO scenario_runs (scenario_id,scenario_version,account_id,snapshot_id,job_id,request_hash) VALUES ($1::uuid,1,$2::uuid,$3::uuid,$4::uuid,$5) RETURNING id::text`, scenarioID, accountID, refs[0].Reference, queuedJobID, strings.Repeat("f", 64)).Scan(&queuedRunID); err != nil {
		t.Fatal(err)
	}
	incomplete := request
	incomplete.EvidenceReferences = append([]domain.EvidenceRef(nil), refs...)
	incomplete.EvidenceReferences[2].Reference = queuedRunID
	incompleteDecision, err := svc.Create(ctx, incomplete)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finalize(ctx, incompleteDecision.ID); !errors.Is(err, applicationjournal.ErrConflict) {
		t.Fatalf("queued result finalization: %v", err)
	}
	if current, err := svc.Get(ctx, incompleteDecision.ID); err != nil || current.Status != domain.StatusDraft {
		t.Fatalf("queued result changed draft: %+v %v", current, err)
	}
	decision, err := svc.Create(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	// Missing archive blocks finalization without altering the draft.
	delete(archiveStore.objects, key)
	if _, err := svc.Finalize(ctx, decision.ID); !errors.Is(err, applicationjournal.ErrConflict) {
		t.Fatalf("missing archive finalization: %v", err)
	}
	if current, err := svc.Get(ctx, decision.ID); err != nil || current.Status != domain.StatusDraft {
		t.Fatalf("non-atomic failure: %+v %v", current, err)
	}
	archiveStore.objects[key] = content
	if _, err := svc.Finalize(ctx, decision.ID); err != nil {
		t.Fatal(err)
	}
	before, err := svc.Evidence(ctx, decision.ID)
	if err != nil {
		t.Fatal(err)
	}
	upper, err := svc.Evidence(ctx, strings.ToUpper(decision.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Manifest, upper.Manifest) || before.SHA256 != upper.SHA256 {
		t.Fatal("uppercase decision id did not reconstruct the same manifest")
	}
	var manifest evidence.Manifest
	if err := json.Unmarshal(before.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.DecisionID != strings.ToLower(decision.ID) || manifest.AccountID != strings.ToLower(accountID) {
		t.Fatalf("manifest identifiers were not canonicalized: %+v", manifest)
	}
	for _, ref := range manifest.References {
		if ref.Reference != strings.ToLower(ref.Reference) {
			t.Fatalf("manifest reference was not canonicalized: %q", ref.Reference)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO portfolio_snapshots (portfolio_id,captured_at) VALUES ($1::uuid,clock_timestamp())`, portfolioID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO scenario_versions (scenario_id,version,template_key,units,shocks,assumptions,content_hash) VALUES ($1::uuid,2,'rates_up','{}','{"rate_bps":200}','{}',$2)`, scenarioID, strings.Repeat("f", 64)); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Evidence(ctx, decision.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before.Manifest, after.Manifest) || before.SHA256 != after.SHA256 {
		t.Fatal("historical manifest drifted")
	}
	delete(archiveStore.objects, key)
	if _, err := svc.Evidence(ctx, decision.ID); !errors.Is(err, evidence.ErrIntegrity) {
		t.Fatalf("missing archived evidence: %v", err)
	}
}
