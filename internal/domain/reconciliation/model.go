package reconciliation

import "time"

const (
	CurrencyTRY = "TRY"
	CurrencyUSD = "USD"

	StateReconciled   = "reconciled"
	StateUnreconciled = "unreconciled"

	LineCheckNone     = "none"
	LineCheckPartial  = "partial"
	LineCheckComplete = "complete"
)

type ToleranceRequest struct {
	ToleranceAmount string `json:"tolerance_amount"`
}

type ToleranceVersion struct {
	ID              string    `json:"id"`
	AccountID       string    `json:"account_id"`
	Version         int       `json:"version"`
	ToleranceAmount string    `json:"tolerance_amount"`
	CreatedAt       time.Time `json:"created_at"`
}

type LineCheckInput struct {
	SnapshotLineID string `json:"snapshot_line_id"`
	ExternalAmount string `json:"external_amount"`
}

type Request struct {
	AccountID          string           `json:"account_id"`
	SourceLabel        string           `json:"source_label"`
	Currency           string           `json:"currency"`
	Cutoff             time.Time        `json:"cutoff"`
	ExternalNAV        string           `json:"external_nav"`
	LineChecks         []LineCheckInput `json:"line_checks,omitempty"`
	LineChecksComplete bool             `json:"line_checks_complete"`
}

type LineCheck struct {
	SnapshotLineID     string `json:"snapshot_line_id"`
	ExternalAmount     string `json:"external_amount"`
	ValuationAmount    string `json:"valuation_amount"`
	Difference         string `json:"difference"`
	AbsoluteDifference string `json:"absolute_difference"`
}

type Checkpoint struct {
	ID                 string      `json:"id"`
	ValuationID        string      `json:"valuation_id"`
	AccountID          string      `json:"account_id"`
	SourceLabel        string      `json:"source_label"`
	Currency           string      `json:"currency"`
	Cutoff             time.Time   `json:"cutoff"`
	ExternalNAV        string      `json:"external_nav"`
	ValuationNAV       string      `json:"valuation_nav"`
	AbsoluteDifference string      `json:"absolute_difference"`
	RelativeDifference *string     `json:"relative_difference"`
	EffectiveTolerance string      `json:"effective_tolerance"`
	ToleranceVersion   int         `json:"tolerance_version"`
	State              string      `json:"state"`
	ReasonCode         *string     `json:"reason_code,omitempty"`
	LineCheckState     string      `json:"line_check_state"`
	LineChecks         []LineCheck `json:"line_checks"`
	CreatedAt          time.Time   `json:"created_at"`
}
