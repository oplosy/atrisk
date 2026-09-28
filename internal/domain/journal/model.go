package journal

import "time"

const (
	StatusDraft     = "draft"
	StatusFinalized = "finalized"
)

// Decision is the user's recorded investment intent. It is never an order or
// an instruction to an external system.
type Decision struct {
	ID                     string         `json:"id"`
	AccountID              string         `json:"account_id"`
	Thesis                 string         `json:"thesis"`
	Alternatives           []string       `json:"alternatives"`
	EvidenceReferences     []EvidenceRef  `json:"evidence_references"`
	InvalidationConditions []Invalidation `json:"invalidation_conditions"`
	Horizon                Horizon        `json:"horizon"`
	RiskBudget             RiskBudget     `json:"risk_budget"`
	IntendedAction         string         `json:"intended_action"`
	Tags                   []string       `json:"tags"`
	Status                 string         `json:"status"`
	Author                 string         `json:"author"`
	SourceMetadata         map[string]any `json:"source_metadata"`
	CreatedAt              time.Time      `json:"created_at"`
	FinalizedAt            *time.Time     `json:"finalized_at,omitempty"`
}

type EvidenceRef struct {
	Kind        string `json:"kind"`
	Reference   string `json:"reference"`
	Description string `json:"description,omitempty"`
}

type Invalidation struct {
	Condition string `json:"condition"`
	Metric    string `json:"metric,omitempty"`
	Threshold string `json:"threshold,omitempty"`
}

type Horizon struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type RiskBudget struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Measure  string `json:"measure"`
	Horizon  string `json:"horizon"`
}

type Review struct {
	ID             string         `json:"id"`
	DecisionID     string         `json:"decision_id"`
	Review         string         `json:"review"`
	Outcome        string         `json:"outcome"`
	Author         string         `json:"author"`
	SourceMetadata map[string]any `json:"source_metadata"`
	CreatedAt      time.Time      `json:"created_at"`
}

type Amendment struct {
	ID             string         `json:"id"`
	DecisionID     string         `json:"decision_id"`
	Summary        string         `json:"summary"`
	Changes        map[string]any `json:"changes"`
	Author         string         `json:"author"`
	SourceMetadata map[string]any `json:"source_metadata"`
	CreatedAt      time.Time      `json:"created_at"`
}

type TimelineEvent struct {
	ID             string         `json:"id"`
	DecisionID     string         `json:"decision_id"`
	Kind           string         `json:"kind"`
	Payload        any            `json:"payload"`
	Author         string         `json:"author"`
	SourceMetadata map[string]any `json:"source_metadata"`
	CreatedAt      time.Time      `json:"created_at"`
}

type Timeline struct {
	Decision Decision        `json:"decision"`
	Events   []TimelineEvent `json:"events"`
}

type CreateRequest struct {
	AccountID              string         `json:"account_id"`
	Thesis                 string         `json:"thesis"`
	Alternatives           []string       `json:"alternatives"`
	EvidenceReferences     []EvidenceRef  `json:"evidence_references"`
	InvalidationConditions []Invalidation `json:"invalidation_conditions"`
	Horizon                Horizon        `json:"horizon"`
	RiskBudget             RiskBudget     `json:"risk_budget"`
	IntendedAction         string         `json:"intended_action"`
	Tags                   []string       `json:"tags"`
	Author                 string         `json:"author"`
	SourceMetadata         map[string]any `json:"source_metadata"`
}

type ReviewRequest struct {
	Review         string         `json:"review"`
	Outcome        string         `json:"outcome"`
	Author         string         `json:"author"`
	SourceMetadata map[string]any `json:"source_metadata"`
}

type AmendmentRequest struct {
	Summary        string         `json:"summary"`
	Changes        map[string]any `json:"changes"`
	Author         string         `json:"author"`
	SourceMetadata map[string]any `json:"source_metadata"`
}
