package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apijournal "github.com/oplosy/atrisk/apps/api/handlers/journal"
	applicationjournal "github.com/oplosy/atrisk/internal/application/journal"
	domain "github.com/oplosy/atrisk/internal/domain/journal"
)

func TestDecisionJournalAPI(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	var portfolioID, accountID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO portfolios (name, reporting_currency)
		VALUES ('journal-' || gen_random_uuid()::text, 'TRY')
		RETURNING id::text`).Scan(&portfolioID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (portfolio_id, name)
		VALUES ($1::uuid, 'journal-account-' || gen_random_uuid()::text)
		RETURNING id::text`, portfolioID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	_, sealedRefs := decisionEvidenceFixture(t, pool, portfolioID, accountID)
	h := apijournal.New(applicationjournal.Service{Pool: pool})
	post := func(path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	invalid := post("/api/v1/decisions", map[string]any{"account_id": accountID, "thesis": "incomplete"})
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("missing invalidation conditions status=%d body=%s", invalid.Code, invalid.Body.String())
	}
	create := domain.CreateRequest{
		AccountID: accountID,
		Thesis:    "TRY rates normalize over the next year",
		Alternatives: []string{
			"Reduce exposure",
		},
		EvidenceReferences:     []domain.EvidenceRef{{Kind: "valuation", Reference: "valuation-fixture", Description: "stored valuation"}},
		InvalidationConditions: []domain.Invalidation{{Condition: "Policy rate remains above 15%", Metric: "policy_rate", Threshold: "15%"}},
		Horizon:                domain.Horizon{Start: mustTime("2026-01-01T00:00:00Z"), End: mustTime("2027-01-01T00:00:00Z")},
		RiskBudget:             domain.RiskBudget{Amount: "1000", Currency: "TRY", Measure: "absolute_loss", Horizon: "12_months"},
		IntendedAction:         "Hold and review monthly",
		Tags:                   []string{"rates", "macro"},
		Author:                 "integration-test",
		SourceMetadata:         map[string]any{"source": "fixture"},
	}
	create.EvidenceReferences = []domain.EvidenceRef{{Kind: " ", Reference: "fixture"}}
	invalidEvidence := post("/api/v1/decisions", create)
	if invalidEvidence.Code != http.StatusBadRequest {
		t.Fatalf("blank evidence reference status=%d body=%s", invalidEvidence.Code, invalidEvidence.Body.String())
	}
	create.EvidenceReferences = sealedRefs
	created := post("/api/v1/decisions", create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var decision domain.Decision
	if err := json.NewDecoder(created.Body).Decode(&decision); err != nil {
		t.Fatal(err)
	}
	if decision.Status != domain.StatusDraft || decision.RiskBudget.Currency != "TRY" {
		t.Fatalf("created decision=%+v", decision)
	}
	beforeRequest := httptest.NewRequest(http.MethodGet, "/api/v1/decisions/"+decision.ID+"/timeline", nil)
	beforeRecorder := httptest.NewRecorder()
	h.ServeHTTP(beforeRecorder, beforeRequest)
	if beforeRecorder.Code != http.StatusOK {
		t.Fatalf("draft timeline status=%d body=%s", beforeRecorder.Code, beforeRecorder.Body.String())
	}
	var beforeTimeline domain.Timeline
	if err := json.NewDecoder(beforeRecorder.Body).Decode(&beforeTimeline); err != nil {
		t.Fatal(err)
	}
	if len(beforeTimeline.Events) != 1 || beforeTimeline.Events[0].Kind != "decision" {
		t.Fatalf("draft timeline events=%+v", beforeTimeline.Events)
	}
	beforePayload, err := json.Marshal(beforeTimeline.Events[0].Payload)
	if err != nil {
		t.Fatal(err)
	}

	finalized := post("/api/v1/decisions/"+decision.ID+"/finalize", map[string]any{})
	if finalized.Code != http.StatusOK {
		t.Fatalf("finalize status=%d body=%s", finalized.Code, finalized.Body.String())
	}
	var sealed domain.Decision
	if err := json.NewDecoder(finalized.Body).Decode(&sealed); err != nil {
		t.Fatal(err)
	}
	if sealed.Status != domain.StatusFinalized || sealed.FinalizedAt == nil {
		t.Fatalf("finalized decision=%+v", sealed)
	}
	if _, err := pool.Exec(ctx, `UPDATE decisions SET thesis='rewritten' WHERE id=$1::uuid`, decision.ID); err == nil {
		t.Fatal("finalized decision content was mutable")
	}
	secondFinalize := post("/api/v1/decisions/"+decision.ID+"/finalize", map[string]any{})
	if secondFinalize.Code != http.StatusConflict {
		t.Fatalf("second finalize status=%d body=%s", secondFinalize.Code, secondFinalize.Body.String())
	}

	review := post("/api/v1/decisions/"+decision.ID+"/reviews", domain.ReviewRequest{Review: "Rates remain elevated", Outcome: "Keep the thesis", Author: "reviewer", SourceMetadata: map[string]any{"channel": "manual"}})
	if review.Code != http.StatusCreated {
		t.Fatalf("review status=%d body=%s", review.Code, review.Body.String())
	}
	amendment := post("/api/v1/decisions/"+decision.ID+"/amendments", domain.AmendmentRequest{Summary: "Extend review cadence", Changes: map[string]any{"review_cadence": "monthly"}, Author: "reviewer", SourceMetadata: map[string]any{"channel": "manual"}})
	if amendment.Code != http.StatusCreated {
		t.Fatalf("amendment status=%d body=%s", amendment.Code, amendment.Body.String())
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/decisions/"+decision.ID+"/timeline", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("timeline status=%d body=%s", rec.Code, rec.Body.String())
	}
	var timeline domain.Timeline
	if err := json.NewDecoder(rec.Body).Decode(&timeline); err != nil {
		t.Fatal(err)
	}
	if len(timeline.Events) != 3 || timeline.Events[0].Kind != "decision" || timeline.Events[1].Kind != "review" || timeline.Events[2].Kind != "amendment" {
		t.Fatalf("timeline events=%+v", timeline.Events)
	}
	afterPayload, err := json.Marshal(timeline.Events[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if string(beforePayload) != string(afterPayload) {
		t.Fatalf("creation timeline payload changed across finalization: before=%s after=%s", beforePayload, afterPayload)
	}
}

func mustTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
