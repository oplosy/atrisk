package portfolio

import "time"

const (
	InstrumentCash       = "cash"
	InstrumentCurrency   = "currency"
	InstrumentSpotCrypto = "spot_crypto"
	InstrumentManualSpot = "manual_spot"
	InstrumentFixedBond  = "fixed_rate_bond"
	StatusActive         = "active"
	StatusInactive       = "inactive"
	StatusDelisted       = "delisted"
	ReportingCurrencyTRY = "TRY"
	ReportingCurrencyUSD = "USD"
)

var SupportedInstrumentTypes = map[string]struct{}{
	InstrumentCash: {}, InstrumentCurrency: {}, InstrumentSpotCrypto: {},
	InstrumentManualSpot: {}, InstrumentFixedBond: {},
}

var SupportedStatuses = map[string]struct{}{
	StatusActive: {}, StatusInactive: {}, StatusDelisted: {},
}

type ExternalIdentifier struct {
	Namespace  string `json:"namespace"`
	ExternalID string `json:"external_id"`
}

type Instrument struct {
	ID              string               `json:"id"`
	CanonicalSymbol string               `json:"canonical_symbol"`
	InstrumentType  string               `json:"instrument_type"`
	NativeUnit      string               `json:"native_unit"`
	ExternalIDs     []ExternalIdentifier `json:"external_ids"`
	Status          string               `json:"status"`
	CreatedAt       time.Time            `json:"created_at"`
}

type Portfolio struct {
	ID                string         `json:"id"`
	Name              string         `json:"name"`
	ReportingCurrency string         `json:"reporting_currency"`
	Metadata          map[string]any `json:"metadata"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type Account struct {
	ID          string         `json:"id"`
	PortfolioID string         `json:"portfolio_id"`
	Name        string         `json:"name"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type SnapshotLine struct {
	ID                    string  `json:"id"`
	AccountID             string  `json:"account_id"`
	InstrumentID          string  `json:"instrument_id"`
	Quantity              string  `json:"quantity"`
	TotalCostBasis        *string `json:"total_cost_basis,omitempty"`
	ModifiedDurationYears string  `json:"modified_duration_years,omitempty"`
	ConvexityYearsSquared string  `json:"convexity_years_squared,omitempty"`
	AccountName           string  `json:"account_name,omitempty"`
	CanonicalSymbol       string  `json:"canonical_symbol,omitempty"`
	InstrumentType        string  `json:"instrument_type,omitempty"`
	NativeUnit            string  `json:"native_unit,omitempty"`
	Status                string  `json:"status,omitempty"`
}

type Snapshot struct {
	ID                   string         `json:"id"`
	PortfolioID          string         `json:"portfolio_id"`
	CapturedAt           time.Time      `json:"captured_at"`
	SupersedesSnapshotID string         `json:"supersedes_snapshot_id,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
	Lines                []SnapshotLine `json:"lines"`
}
