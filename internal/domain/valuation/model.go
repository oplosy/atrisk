package valuation

import "time"

const (
	KnowledgeSystem = "system_as_of"
	KnowledgeSource = "source_as_of"
	StateValid      = "valid"
	StateDegraded   = "degraded"
	StateBlocked    = "blocked"
)

type Request struct {
	SnapshotID         string    `json:"snapshot_id"`
	Cutoff             time.Time `json:"cutoff"`
	KnowledgeMode      string    `json:"knowledge_mode"`
	KnownAt            time.Time `json:"known_at"`
	PriceMaxAgeSeconds int64     `json:"price_max_age_seconds"`
	FXMaxAgeSeconds    int64     `json:"fx_max_age_seconds"`
}

type FXPathEntry struct {
	QuoteRevisionID string `json:"quote_revision_id"`
	Direction       string `json:"direction"`
}

type Reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Line struct {
	ID              string        `json:"id"`
	SnapshotLineID  string        `json:"snapshot_line_id"`
	InstrumentID    string        `json:"instrument_id"`
	NativeCurrency  string        `json:"native_currency"`
	NativeAmount    *string       `json:"native_amount"`
	TryAmount       *string       `json:"try_amount"`
	USDAmount       *string       `json:"usd_amount"`
	State           string        `json:"state"`
	ReasonCodes     []Reason      `json:"reason_codes"`
	PriceMethod     string        `json:"price_method"`
	PriceRevisionID *string       `json:"price_revision_id"`
	PriceQuoteUnit  *string       `json:"price_quote_unit"`
	TryFXPath       []FXPathEntry `json:"try_fx_path"`
	USDFXPath       []FXPathEntry `json:"usd_fx_path"`
}

type Totals struct {
	TRY *string `json:"try"`
	USD *string `json:"usd"`
}

type Run struct {
	ID                 string    `json:"id"`
	SnapshotID         string    `json:"snapshot_id"`
	Cutoff             time.Time `json:"cutoff"`
	KnowledgeMode      string    `json:"knowledge_mode"`
	KnownAt            time.Time `json:"known_at"`
	PriceMaxAgeSeconds int64     `json:"price_max_age_seconds"`
	FXMaxAgeSeconds    int64     `json:"fx_max_age_seconds"`
	State              string    `json:"state"`
	ResultHash         string    `json:"result_hash"`
	Lines              []Line    `json:"lines"`
	Totals             Totals    `json:"totals"`
	CreatedAt          time.Time `json:"created_at"`
}
