package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	apireconciliation "github.com/oplosy/atrisk/apps/api/handlers/reconciliation"
	applicationportfolio "github.com/oplosy/atrisk/internal/application/portfolio"
	applicationreconciliation "github.com/oplosy/atrisk/internal/application/reconciliation"
	domainportfolio "github.com/oplosy/atrisk/internal/domain/portfolio"
	domainreconciliation "github.com/oplosy/atrisk/internal/domain/reconciliation"
	"github.com/oplosy/atrisk/internal/platform/database"
)

func TestReconciliationAPI(t *testing.T) {
	migrateTestDatabase(t)
	_, pool := testDatabase(t)
	defer pool.Close()
	ctx := context.Background()
	portfolioService := applicationportfolio.Service{Queries: database.New(pool), Beginner: pool}
	firstInstrument, err := portfolioService.CreateInstrument(ctx, applicationportfolio.CreateInstrumentRequest{CanonicalSymbol: "recon-cash-a", InstrumentType: domainportfolio.InstrumentCash, NativeUnit: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	secondInstrument, err := portfolioService.CreateInstrument(ctx, applicationportfolio.CreateInstrumentRequest{CanonicalSymbol: "recon-cash-b", InstrumentType: domainportfolio.InstrumentCash, NativeUnit: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	thirdInstrument, err := portfolioService.CreateInstrument(ctx, applicationportfolio.CreateInstrumentRequest{CanonicalSymbol: "recon-cash-c", InstrumentType: domainportfolio.InstrumentCash, NativeUnit: "USD"})
	if err != nil {
		t.Fatal(err)
	}
	portfolio, err := portfolioService.CreatePortfolio(ctx, applicationportfolio.CreatePortfolioRequest{Name: "reconciliation-fixture", ReportingCurrency: "TRY"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := portfolioService.CreateAccount(ctx, portfolio.ID, applicationportfolio.CreateAccountRequest{Name: "reconciliation-account"})
	if err != nil {
		t.Fatal(err)
	}
	otherAccount, err := portfolioService.CreateAccount(ctx, portfolio.ID, applicationportfolio.CreateAccountRequest{Name: "reconciliation-other-account"})
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	snapshot, err := portfolioService.CreateSnapshot(ctx, portfolio.ID, applicationportfolio.CreateSnapshotRequest{CapturedAt: cutoff, Lines: []applicationportfolio.SnapshotLineInput{
		{AccountID: account.ID, InstrumentID: firstInstrument.ID, Quantity: "1"},
		{AccountID: account.ID, InstrumentID: secondInstrument.ID, Quantity: "1"},
		{AccountID: otherAccount.ID, InstrumentID: thirdInstrument.ID, Quantity: "1"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	lineIDs := make(map[string]string)
	for _, line := range snapshot.Lines {
		lineIDs[line.InstrumentID] = line.ID
	}
	var valuationID string
	if err := pool.QueryRow(ctx, `INSERT INTO valuation_runs (snapshot_id,cutoff,knowledge_mode,known_at,price_max_age_seconds,fx_max_age_seconds,request,state,result_hash) VALUES ($1::uuid,$2,'system_as_of',$2,0,0,'{}','valid',repeat('0',64)) RETURNING id::text`, mustUUID(t, snapshot.ID), cutoff).Scan(&valuationID); err != nil {
		t.Fatal(err)
	}
	for _, line := range []struct {
		instrumentID, tryAmount, usdAmount string
	}{{firstInstrument.ID, "80", "2"}, {secondInstrument.ID, "20", "0.5"}, {thirdInstrument.ID, "10", "0.25"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO valuation_lines (run_id,snapshot_line_id,native_currency,native_amount,try_amount,usd_amount,state,reason_codes,price_method) VALUES ($1::uuid,$2::uuid,'USD','1',$3,$4,'valid','[]','identity')`, valuationID, mustUUID(t, lineIDs[line.instrumentID]), line.tryAmount, line.usdAmount); err != nil {
			t.Fatal(err)
		}
	}
	h := apireconciliation.New(applicationreconciliation.Service{Pool: pool})
	postJSON := func(method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		payload, marshalErr := json.Marshal(body)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	var versions sync.WaitGroup
	versionErrors := make(chan error, 2)
	for _, amount := range []string{"0.01", "0.02"} {
		amount := amount
		versions.Add(1)
		go func() {
			defer versions.Done()
			rec := postJSON(http.MethodPost, "/api/v1/accounts/"+account.ID+"/reconciliation-tolerances", domainreconciliation.ToleranceRequest{ToleranceAmount: amount})
			if rec.Code != http.StatusCreated {
				versionErrors <- fmt.Errorf("tolerance status=%d body=%s", rec.Code, rec.Body.String())
			}
		}()
	}
	versions.Wait()
	close(versionErrors)
	for err := range versionErrors {
		t.Fatal(err)
	}
	var toleranceVersions []int
	rows, err := pool.Query(ctx, `SELECT version FROM reconciliation_tolerance_versions WHERE account_id=$1::uuid ORDER BY version`, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			t.Fatal(err)
		}
		toleranceVersions = append(toleranceVersions, version)
	}
	rows.Close()
	if !sort.IntsAreSorted(toleranceVersions) || len(toleranceVersions) != 2 || toleranceVersions[0] != 1 || toleranceVersions[1] != 2 {
		t.Fatalf("tolerance versions=%v", toleranceVersions)
	}
	baseRequest := domainreconciliation.Request{AccountID: account.ID, SourceLabel: "broker-nav", Currency: "TRY", Cutoff: cutoff, ExternalNAV: "100"}
	partial := postJSON(http.MethodPost, "/api/v1/valuations/"+valuationID+"/reconciliations", baseRequest)
	if partial.Code != http.StatusCreated {
		t.Fatalf("none status=%d body=%s", partial.Code, partial.Body.String())
	}
	var none domainreconciliation.Checkpoint
	if err := json.NewDecoder(partial.Body).Decode(&none); err != nil || none.LineCheckState != domainreconciliation.LineCheckNone || none.ValuationNAV != "100.000000000000000000" || none.State != domainreconciliation.StateReconciled {
		t.Fatalf("none checkpoint=%+v err=%v", none, err)
	}
	baseRequest.LineChecks = []domainreconciliation.LineCheckInput{{SnapshotLineID: lineIDs[firstInstrument.ID], ExternalAmount: "80"}}
	partial = postJSON(http.MethodPost, "/api/v1/valuations/"+valuationID+"/reconciliations", baseRequest)
	if partial.Code != http.StatusCreated {
		t.Fatalf("partial status=%d body=%s", partial.Code, partial.Body.String())
	}
	var partialCheckpoint domainreconciliation.Checkpoint
	if err := json.NewDecoder(partial.Body).Decode(&partialCheckpoint); err != nil || partialCheckpoint.LineCheckState != domainreconciliation.LineCheckPartial || len(partialCheckpoint.LineChecks) != 1 {
		t.Fatalf("partial checkpoint=%+v err=%v", partialCheckpoint, err)
	}
	baseRequest.LineChecksComplete = true
	incomplete := postJSON(http.MethodPost, "/api/v1/valuations/"+valuationID+"/reconciliations", baseRequest)
	if incomplete.Code != http.StatusConflict || !bytes.Contains(incomplete.Body.Bytes(), []byte(`"code":"LINE_CHECK_INCOMPLETE"`)) {
		t.Fatalf("incomplete status=%d body=%s", incomplete.Code, incomplete.Body.String())
	}
	baseRequest.LineChecksComplete = false
	baseRequest.LineChecks = []domainreconciliation.LineCheckInput{{SnapshotLineID: lineIDs[thirdInstrument.ID], ExternalAmount: "10"}}
	crossAccount := postJSON(http.MethodPost, "/api/v1/valuations/"+valuationID+"/reconciliations", baseRequest)
	if crossAccount.Code != http.StatusConflict || !bytes.Contains(crossAccount.Body.Bytes(), []byte(`"code":"LINE_CHECK_CROSS_ACCOUNT"`)) {
		t.Fatalf("cross-account status=%d body=%s", crossAccount.Code, crossAccount.Body.String())
	}
	baseRequest.LineChecks = []domainreconciliation.LineCheckInput{{SnapshotLineID: lineIDs[firstInstrument.ID], ExternalAmount: "60"}, {SnapshotLineID: lineIDs[secondInstrument.ID], ExternalAmount: "40"}}
	baseRequest.LineChecksComplete = true
	complete := postJSON(http.MethodPost, "/api/v1/valuations/"+valuationID+"/reconciliations", baseRequest)
	if complete.Code != http.StatusCreated {
		t.Fatalf("complete status=%d body=%s", complete.Code, complete.Body.String())
	}
	var completeCheckpoint domainreconciliation.Checkpoint
	if err := json.NewDecoder(complete.Body).Decode(&completeCheckpoint); err != nil || completeCheckpoint.LineCheckState != domainreconciliation.LineCheckComplete || len(completeCheckpoint.LineChecks) != 2 {
		t.Fatalf("complete checkpoint=%+v err=%v", completeCheckpoint, err)
	}
	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/reconciliations/"+completeCheckpoint.ID, nil)
	getRec := httptest.NewRecorder()
	h.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK || !bytes.Contains(getRec.Body.Bytes(), []byte(`"tolerance_version":2`)) {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}
	conflictRequest := baseRequest
	conflictRequest.LineChecks = nil
	conflictRequest.LineChecksComplete = false
	conflictRequest.Currency = "USD"
	conflict := postJSON(http.MethodPost, "/api/v1/valuations/"+valuationID+"/reconciliations", conflictRequest)
	if conflict.Code != http.StatusConflict || !bytes.Contains(conflict.Body.Bytes(), []byte(`"code":"CURRENCY_MISMATCH"`)) {
		t.Fatalf("currency conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	if _, err := pool.Exec(ctx, `UPDATE reconciliation_checkpoints SET source_label='changed' WHERE id=$1::uuid`, mustUUID(t, completeCheckpoint.ID)); err == nil {
		t.Fatal("checkpoint was mutable")
	}
	if _, err := pool.Exec(ctx, `UPDATE reconciliation_tolerance_versions SET tolerance_amount=0 WHERE account_id=$1::uuid AND version=1`, mustUUID(t, account.ID)); err == nil {
		t.Fatal("tolerance version was mutable")
	}
	var persistedLines int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM reconciliation_line_checks WHERE reconciliation_id=$1::uuid`, mustUUID(t, completeCheckpoint.ID)).Scan(&persistedLines); err != nil || persistedLines != 2 {
		t.Fatalf("persisted line checks=%d err=%v", persistedLines, err)
	}
}
