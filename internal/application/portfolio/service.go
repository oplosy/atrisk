package portfolio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	domain "github.com/oplosy/atrisk/internal/domain/portfolio"
	database "github.com/oplosy/atrisk/internal/platform/database"
)

var (
	ErrInvalidRequest = errors.New("invalid portfolio request")
	ErrNotFound       = errors.New("portfolio resource not found")
	ErrConflict       = errors.New("portfolio resource conflict")
	ErrDatabase       = errors.New("portfolio database is required")
)

var unitCodePattern = regexp.MustCompile(`^[A-Z0-9]+$`)
var decimalPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)

type Beginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type Service struct {
	Queries  *database.Queries
	Beginner Beginner
}

type CreateInstrumentRequest struct {
	CanonicalSymbol string                      `json:"canonical_symbol"`
	InstrumentType  string                      `json:"instrument_type"`
	NativeUnit      string                      `json:"native_unit"`
	ExternalIDs     []domain.ExternalIdentifier `json:"external_ids"`
	Status          string                      `json:"status"`
}

type CreatePortfolioRequest struct {
	Name              string         `json:"name"`
	ReportingCurrency string         `json:"reporting_currency"`
	Metadata          map[string]any `json:"metadata"`
}

type UpdatePortfolioRequest struct {
	Name              string         `json:"name"`
	ReportingCurrency string         `json:"reporting_currency"`
	Metadata          map[string]any `json:"metadata"`
}

type CreateAccountRequest struct {
	Name     string         `json:"name"`
	Metadata map[string]any `json:"metadata"`
}

type UpdateAccountRequest struct {
	Name     string         `json:"name"`
	Metadata map[string]any `json:"metadata"`
}

type SnapshotLineInput struct {
	AccountID             string  `json:"account_id"`
	InstrumentID          string  `json:"instrument_id"`
	Quantity              string  `json:"quantity"`
	TotalCostBasis        *string `json:"total_cost_basis"`
	ModifiedDurationYears string  `json:"modified_duration_years"`
	ConvexityYearsSquared string  `json:"convexity_years_squared"`
}

type CreateSnapshotRequest struct {
	CapturedAt           time.Time           `json:"captured_at"`
	SupersedesSnapshotID string              `json:"supersedes_snapshot_id"`
	Lines                []SnapshotLineInput `json:"lines"`
}

func (s Service) CreateInstrument(ctx context.Context, request CreateInstrumentRequest) (domain.Instrument, error) {
	if s.Queries == nil || s.Beginner == nil {
		return domain.Instrument{}, ErrDatabase
	}
	request.CanonicalSymbol = strings.TrimSpace(request.CanonicalSymbol)
	request.InstrumentType = strings.TrimSpace(request.InstrumentType)
	request.NativeUnit = strings.ToUpper(strings.TrimSpace(request.NativeUnit))
	if request.CanonicalSymbol == "" || !isSupportedType(request.InstrumentType) || !unitCodePattern.MatchString(request.NativeUnit) {
		return domain.Instrument{}, ErrInvalidRequest
	}
	if request.Status == "" {
		request.Status = domain.StatusActive
	}
	if !isSupportedStatus(request.Status) || !validExternalIDs(request.ExternalIDs) {
		return domain.Instrument{}, ErrInvalidRequest
	}
	metadata, err := json.Marshal(externalIDsMap(request.ExternalIDs))
	if err != nil {
		return domain.Instrument{}, ErrInvalidRequest
	}
	tx, err := s.Beginner.Begin(ctx)
	if err != nil {
		return domain.Instrument{}, fmt.Errorf("begin instrument transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := database.New(tx)
	row, err := queries.CreatePortfolioInstrument(ctx, database.CreatePortfolioInstrumentParams{
		CanonicalSymbol: request.CanonicalSymbol,
		InstrumentType:  request.InstrumentType,
		NativeCurrency:  request.NativeUnit,
		ExternalIds:     metadata,
		Status:          request.Status,
	})
	if err != nil {
		return domain.Instrument{}, mapDatabaseError(err)
	}
	for _, identifier := range request.ExternalIDs {
		if _, err := queries.InsertInstrumentExternalIdentifier(ctx, database.InsertInstrumentExternalIdentifierParams{InstrumentID: row.ID, Namespace: identifier.Namespace, ExternalID: identifier.ExternalID}); err != nil {
			return domain.Instrument{}, mapDatabaseError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Instrument{}, fmt.Errorf("commit instrument transaction: %w", err)
	}
	return s.instrument(ctx, row)
}

func (s Service) ListInstruments(ctx context.Context) ([]domain.Instrument, error) {
	if s.Queries == nil {
		return nil, ErrDatabase
	}
	rows, err := s.Queries.ListPortfolioInstruments(ctx)
	if err != nil {
		return nil, fmt.Errorf("list instruments: %w", err)
	}
	items := make([]domain.Instrument, 0, len(rows))
	for _, row := range rows {
		item, err := s.instrument(ctx, row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s Service) GetInstrument(ctx context.Context, id string) (domain.Instrument, error) {
	if s.Queries == nil {
		return domain.Instrument{}, ErrDatabase
	}
	parsed, err := parseUUID(id)
	if err != nil {
		return domain.Instrument{}, ErrInvalidRequest
	}
	row, err := s.Queries.GetPortfolioInstrument(ctx, parsed)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Instrument{}, ErrNotFound
	}
	if err != nil {
		return domain.Instrument{}, fmt.Errorf("get instrument: %w", err)
	}
	return s.instrument(ctx, row)
}

func (s Service) UpdateInstrumentStatus(ctx context.Context, id, status string) (domain.Instrument, error) {
	if s.Queries == nil {
		return domain.Instrument{}, ErrDatabase
	}
	parsed, err := parseUUID(id)
	if err != nil || !isSupportedStatus(status) {
		return domain.Instrument{}, ErrInvalidRequest
	}
	row, err := s.Queries.UpdatePortfolioInstrumentStatus(ctx, database.UpdatePortfolioInstrumentStatusParams{ID: parsed, Status: status})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Instrument{}, ErrNotFound
	}
	if err != nil {
		return domain.Instrument{}, mapDatabaseError(err)
	}
	return s.instrument(ctx, row)
}

func (s Service) CreatePortfolio(ctx context.Context, request CreatePortfolioRequest) (domain.Portfolio, error) {
	if s.Queries == nil {
		return domain.Portfolio{}, ErrDatabase
	}
	if err := validatePortfolio(request.Name, request.ReportingCurrency); err != nil {
		return domain.Portfolio{}, err
	}
	metadata, err := encodeMetadata(request.Metadata)
	if err != nil {
		return domain.Portfolio{}, err
	}
	row, err := s.Queries.CreatePortfolio(ctx, database.CreatePortfolioParams{Name: strings.TrimSpace(request.Name), ReportingCurrency: request.ReportingCurrency, Metadata: metadata})
	if err != nil {
		return domain.Portfolio{}, mapDatabaseError(err)
	}
	return portfolioFromRow(row), nil
}

func (s Service) ListPortfolios(ctx context.Context) ([]domain.Portfolio, error) {
	if s.Queries == nil {
		return nil, ErrDatabase
	}
	rows, err := s.Queries.ListPortfolios(ctx)
	if err != nil {
		return nil, fmt.Errorf("list portfolios: %w", err)
	}
	items := make([]domain.Portfolio, 0, len(rows))
	for _, row := range rows {
		items = append(items, portfolioFromRow(row))
	}
	return items, nil
}

func (s Service) GetPortfolio(ctx context.Context, id string) (domain.Portfolio, error) {
	if s.Queries == nil {
		return domain.Portfolio{}, ErrDatabase
	}
	parsed, err := parseUUID(id)
	if err != nil {
		return domain.Portfolio{}, ErrInvalidRequest
	}
	row, err := s.Queries.GetPortfolio(ctx, parsed)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Portfolio{}, ErrNotFound
	}
	if err != nil {
		return domain.Portfolio{}, fmt.Errorf("get portfolio: %w", err)
	}
	return portfolioFromRow(row), nil
}

func (s Service) UpdatePortfolio(ctx context.Context, id string, request UpdatePortfolioRequest) (domain.Portfolio, error) {
	if s.Queries == nil {
		return domain.Portfolio{}, ErrDatabase
	}
	parsed, err := parseUUID(id)
	if err != nil {
		return domain.Portfolio{}, ErrInvalidRequest
	}
	if err := validatePortfolio(request.Name, request.ReportingCurrency); err != nil {
		return domain.Portfolio{}, err
	}
	metadata, err := encodeMetadata(request.Metadata)
	if err != nil {
		return domain.Portfolio{}, err
	}
	row, err := s.Queries.UpdatePortfolio(ctx, database.UpdatePortfolioParams{ID: parsed, Name: strings.TrimSpace(request.Name), ReportingCurrency: request.ReportingCurrency, Metadata: metadata})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Portfolio{}, ErrNotFound
	}
	if err != nil {
		return domain.Portfolio{}, mapDatabaseError(err)
	}
	return portfolioFromRow(row), nil
}

func (s Service) DeletePortfolio(ctx context.Context, id string) error {
	if s.Queries == nil {
		return ErrDatabase
	}
	parsed, err := parseUUID(id)
	if err != nil {
		return ErrInvalidRequest
	}
	rows, err := s.Queries.DeletePortfolio(ctx, parsed)
	if err != nil {
		return mapDatabaseError(err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s Service) CreateAccount(ctx context.Context, portfolioID string, request CreateAccountRequest) (domain.Account, error) {
	if s.Queries == nil {
		return domain.Account{}, ErrDatabase
	}
	pid, err := parseUUID(portfolioID)
	if err != nil || strings.TrimSpace(request.Name) == "" {
		return domain.Account{}, ErrInvalidRequest
	}
	if _, err := s.Queries.GetPortfolio(ctx, pid); errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, ErrNotFound
	} else if err != nil {
		return domain.Account{}, fmt.Errorf("get account portfolio: %w", err)
	}
	metadata, err := encodeMetadata(request.Metadata)
	if err != nil {
		return domain.Account{}, err
	}
	row, err := s.Queries.CreateAccount(ctx, database.CreateAccountParams{PortfolioID: pid, Name: strings.TrimSpace(request.Name), Metadata: metadata})
	if err != nil {
		return domain.Account{}, mapDatabaseError(err)
	}
	return accountFromRow(row), nil
}

func (s Service) ListAccounts(ctx context.Context, portfolioID string) ([]domain.Account, error) {
	if s.Queries == nil {
		return nil, ErrDatabase
	}
	pid, err := parseUUID(portfolioID)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	if _, err := s.Queries.GetPortfolio(ctx, pid); errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("get accounts portfolio: %w", err)
	}
	rows, err := s.Queries.ListAccounts(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	items := make([]domain.Account, 0, len(rows))
	for _, row := range rows {
		items = append(items, accountFromRow(row))
	}
	return items, nil
}

func (s Service) GetAccount(ctx context.Context, id string) (domain.Account, error) {
	if s.Queries == nil {
		return domain.Account{}, ErrDatabase
	}
	parsed, err := parseUUID(id)
	if err != nil {
		return domain.Account{}, ErrInvalidRequest
	}
	row, err := s.Queries.GetAccount(ctx, parsed)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, ErrNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("get account: %w", err)
	}
	return accountFromRow(row), nil
}

func (s Service) UpdateAccount(ctx context.Context, id string, request UpdateAccountRequest) (domain.Account, error) {
	if s.Queries == nil {
		return domain.Account{}, ErrDatabase
	}
	parsed, err := parseUUID(id)
	if err != nil || strings.TrimSpace(request.Name) == "" {
		return domain.Account{}, ErrInvalidRequest
	}
	metadata, err := encodeMetadata(request.Metadata)
	if err != nil {
		return domain.Account{}, err
	}
	row, err := s.Queries.UpdateAccount(ctx, database.UpdateAccountParams{ID: parsed, Name: strings.TrimSpace(request.Name), Metadata: metadata})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, ErrNotFound
	}
	if err != nil {
		return domain.Account{}, mapDatabaseError(err)
	}
	return accountFromRow(row), nil
}

func (s Service) DeleteAccount(ctx context.Context, id string) error {
	if s.Queries == nil {
		return ErrDatabase
	}
	parsed, err := parseUUID(id)
	if err != nil {
		return ErrInvalidRequest
	}
	rows, err := s.Queries.DeleteAccount(ctx, parsed)
	if err != nil {
		return mapDatabaseError(err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s Service) CreateSnapshot(ctx context.Context, portfolioID string, request CreateSnapshotRequest) (domain.Snapshot, error) {
	if s.Queries == nil || s.Beginner == nil {
		return domain.Snapshot{}, ErrDatabase
	}
	pid, err := parseUUID(portfolioID)
	if err != nil || request.CapturedAt.IsZero() || len(request.Lines) == 0 {
		return domain.Snapshot{}, ErrInvalidRequest
	}
	if _, err := s.Queries.GetPortfolio(ctx, pid); errors.Is(err, pgx.ErrNoRows) {
		return domain.Snapshot{}, ErrNotFound
	} else if err != nil {
		return domain.Snapshot{}, fmt.Errorf("get snapshot portfolio: %w", err)
	}
	supersedes := pgtype.UUID{}
	if request.SupersedesSnapshotID != "" {
		supersedes, err = parseUUID(request.SupersedesSnapshotID)
		if err != nil {
			return domain.Snapshot{}, ErrInvalidRequest
		}
		row, getErr := s.Queries.GetPortfolioSnapshot(ctx, supersedes)
		if errors.Is(getErr, pgx.ErrNoRows) {
			return domain.Snapshot{}, ErrNotFound
		}
		if getErr != nil || row.PortfolioID != pid {
			return domain.Snapshot{}, ErrInvalidRequest
		}
	}
	inputs, err := s.prepareLines(ctx, pid, request.Lines)
	if err != nil {
		return domain.Snapshot{}, err
	}
	tx, err := s.Beginner.Begin(ctx)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("begin snapshot transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := database.New(tx)
	snapshot, err := queries.CreatePortfolioSnapshot(ctx, database.CreatePortfolioSnapshotParams{PortfolioID: pid, CapturedAt: pgtype.Timestamptz{Time: request.CapturedAt.UTC(), Valid: true}, SupersedesSnapshotID: supersedes})
	if err != nil {
		return domain.Snapshot{}, mapDatabaseError(err)
	}
	for _, line := range inputs {
		if _, err := queries.InsertPortfolioSnapshotLine(ctx, database.InsertPortfolioSnapshotLineParams{
			PortfolioID: pid, SnapshotID: snapshot.ID, AccountID: line.accountID, InstrumentID: line.instrumentID,
			Quantity: line.quantity, TotalCostBasis: line.costBasis, ModifiedDurationYears: line.duration, ConvexityYearsSquared: line.convexity,
		}); err != nil {
			return domain.Snapshot{}, mapDatabaseError(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Snapshot{}, fmt.Errorf("commit snapshot transaction: %w", err)
	}
	return s.snapshot(ctx, snapshot)
}

func (s Service) ListSnapshots(ctx context.Context, portfolioID string) ([]domain.Snapshot, error) {
	if s.Queries == nil {
		return nil, ErrDatabase
	}
	pid, err := parseUUID(portfolioID)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	rows, err := s.Queries.ListPortfolioSnapshots(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("list snapshots: %w", err)
	}
	items := make([]domain.Snapshot, 0, len(rows))
	for _, row := range rows {
		item, err := s.snapshot(ctx, row)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s Service) GetSnapshot(ctx context.Context, id string) (domain.Snapshot, error) {
	if s.Queries == nil {
		return domain.Snapshot{}, ErrDatabase
	}
	parsed, err := parseUUID(id)
	if err != nil {
		return domain.Snapshot{}, ErrInvalidRequest
	}
	row, err := s.Queries.GetPortfolioSnapshot(ctx, parsed)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Snapshot{}, ErrNotFound
	}
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("get snapshot: %w", err)
	}
	return s.snapshot(ctx, row)
}

type preparedLine struct {
	accountID, instrumentID pgtype.UUID
	quantity, costBasis     pgtype.Numeric
	duration, convexity     pgtype.Numeric
}

func (s Service) prepareLines(ctx context.Context, portfolioID pgtype.UUID, lines []SnapshotLineInput) ([]preparedLine, error) {
	seen := make(map[string]struct{}, len(lines))
	prepared := make([]preparedLine, 0, len(lines))
	for _, input := range lines {
		accountID, err := parseUUID(input.AccountID)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		instrumentID, err := parseUUID(input.InstrumentID)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		key := accountID.String() + ":" + instrumentID.String()
		if _, exists := seen[key]; exists {
			return nil, ErrInvalidRequest
		}
		seen[key] = struct{}{}
		account, err := s.Queries.GetAccount(ctx, accountID)
		if errors.Is(err, pgx.ErrNoRows) || account.PortfolioID != portfolioID {
			return nil, ErrInvalidRequest
		}
		if err != nil {
			return nil, fmt.Errorf("get snapshot account: %w", err)
		}
		instrument, err := s.Queries.GetPortfolioInstrument(ctx, instrumentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidRequest
		}
		if err != nil {
			return nil, fmt.Errorf("get snapshot instrument: %w", err)
		}
		quantity, err := parseDecimal(input.Quantity, false)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		costBasis := pgtype.Numeric{}
		if input.TotalCostBasis != nil {
			costBasis, err = parseDecimal(*input.TotalCostBasis, false)
		}
		if err != nil {
			return nil, ErrInvalidRequest
		}
		duration, err := parseDecimal(input.ModifiedDurationYears, true)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		convexity, err := parseDecimal(input.ConvexityYearsSquared, true)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		if instrument.InstrumentType == domain.InstrumentFixedBond {
			if !duration.Valid || duration.Int.Sign() == 0 || (duration.Int.Sign() < 0) || (convexity.Valid && convexity.Int.Sign() < 0) {
				return nil, ErrInvalidRequest
			}
		} else if duration.Valid || convexity.Valid {
			return nil, ErrInvalidRequest
		}
		prepared = append(prepared, preparedLine{accountID: accountID, instrumentID: instrumentID, quantity: quantity, costBasis: costBasis, duration: duration, convexity: convexity})
	}
	return prepared, nil
}

func (s Service) snapshot(ctx context.Context, row database.PortfolioSnapshot) (domain.Snapshot, error) {
	lines, err := s.Queries.ListPortfolioSnapshotLines(ctx, row.ID)
	if err != nil {
		return domain.Snapshot{}, fmt.Errorf("list snapshot lines: %w", err)
	}
	item := domain.Snapshot{ID: row.ID.String(), PortfolioID: row.PortfolioID.String(), CapturedAt: row.CapturedAt.Time.UTC(), CreatedAt: row.CreatedAt.Time.UTC(), Lines: make([]domain.SnapshotLine, 0, len(lines))}
	if row.SupersedesSnapshotID.Valid {
		item.SupersedesSnapshotID = row.SupersedesSnapshotID.String()
	}
	for _, line := range lines {
		item.Lines = append(item.Lines, domain.SnapshotLine{ID: line.ID.String(), AccountID: line.AccountID.String(), InstrumentID: line.InstrumentID.String(), Quantity: numericString(line.Quantity), TotalCostBasis: numericPointer(line.TotalCostBasis), ModifiedDurationYears: numericOptionalString(line.ModifiedDurationYears), ConvexityYearsSquared: numericOptionalString(line.ConvexityYearsSquared), AccountName: line.AccountName, CanonicalSymbol: line.CanonicalSymbol, InstrumentType: line.InstrumentType, NativeUnit: line.NativeCurrency, Status: line.Status})
	}
	return item, nil
}

func (s Service) instrument(ctx context.Context, row database.Instrument) (domain.Instrument, error) {
	ids, err := s.Queries.ListInstrumentExternalIdentifiers(ctx, row.ID)
	if err != nil {
		return domain.Instrument{}, fmt.Errorf("list instrument identifiers: %w", err)
	}
	result := domain.Instrument{ID: row.ID.String(), CanonicalSymbol: row.CanonicalSymbol, InstrumentType: row.InstrumentType, NativeUnit: row.NativeCurrency, Status: row.Status, CreatedAt: row.CreatedAt.Time.UTC(), ExternalIDs: make([]domain.ExternalIdentifier, 0, len(ids))}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		result.ExternalIDs = append(result.ExternalIDs, domain.ExternalIdentifier{Namespace: id.Namespace, ExternalID: id.ExternalID})
		seen[id.Namespace] = struct{}{}
	}
	// AR-101/105 rows may predate the normalized identifier table. The
	// migration backfills them, but the fallback keeps reads correct during a
	// rolling upgrade and makes the two representations converge on response.
	var legacy map[string]string
	if json.Unmarshal(row.ExternalIds, &legacy) == nil {
		for namespace, externalID := range legacy {
			if _, exists := seen[namespace]; exists || strings.TrimSpace(namespace) == "" || strings.TrimSpace(externalID) == "" {
				continue
			}
			result.ExternalIDs = append(result.ExternalIDs, domain.ExternalIdentifier{Namespace: namespace, ExternalID: externalID})
		}
	}
	return result, nil
}

func validatePortfolio(name, currency string) error {
	if strings.TrimSpace(name) == "" || (currency != domain.ReportingCurrencyTRY && currency != domain.ReportingCurrencyUSD) {
		return ErrInvalidRequest
	}
	return nil
}

func isSupportedType(value string) bool   { _, ok := domain.SupportedInstrumentTypes[value]; return ok }
func isSupportedStatus(value string) bool { _, ok := domain.SupportedStatuses[value]; return ok }

func validExternalIDs(ids []domain.ExternalIdentifier) bool {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id.Namespace != strings.TrimSpace(id.Namespace) || id.ExternalID != strings.TrimSpace(id.ExternalID) || id.Namespace == "" || id.ExternalID == "" {
			return false
		}
		key := strings.TrimSpace(id.Namespace)
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func externalIDsMap(ids []domain.ExternalIdentifier) map[string]string {
	result := make(map[string]string, len(ids))
	for _, id := range ids {
		result[id.Namespace] = id.ExternalID
	}
	return result
}

func encodeMetadata(value map[string]any) ([]byte, error) {
	if value == nil {
		return []byte(`{}`), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil || !json.Valid(encoded) {
		return nil, ErrInvalidRequest
	}
	return encoded, nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(strings.TrimSpace(value)); err != nil || !id.Valid {
		return id, ErrInvalidRequest
	}
	return id, nil
}

func parseDecimal(value string, optional bool) (pgtype.Numeric, error) {
	if strings.TrimSpace(value) == "" {
		if optional {
			return pgtype.Numeric{}, nil
		}
		return pgtype.Numeric{}, ErrInvalidRequest
	}
	value = strings.TrimSpace(value)
	if !decimalPattern.MatchString(value) {
		return pgtype.Numeric{}, ErrInvalidRequest
	}
	digits := strings.TrimPrefix(value, "-")
	parts := strings.SplitN(digits, ".", 2)
	fractionalDigits := ""
	if len(parts) == 2 {
		fractionalDigits = parts[1]
	}
	if len(fractionalDigits) > 18 {
		return pgtype.Numeric{}, ErrInvalidRequest
	}
	integerDigits := strings.TrimLeft(parts[0], "0")
	if len(integerDigits) > 20 || len(integerDigits)+len(fractionalDigits) > 38 {
		return pgtype.Numeric{}, ErrInvalidRequest
	}
	var result pgtype.Numeric
	if err := result.Scan(value); err != nil || !result.Valid {
		return pgtype.Numeric{}, ErrInvalidRequest
	}
	return result, nil
}

func numericString(value pgtype.Numeric) string {
	if !value.Valid || value.Int == nil {
		return ""
	}
	digits := value.Int.String()
	sign := ""
	if strings.HasPrefix(digits, "-") || strings.HasPrefix(digits, "+") {
		sign, digits = digits[:1], digits[1:]
	}
	if value.Exp >= 0 {
		return sign + digits + strings.Repeat("0", int(value.Exp))
	}
	scale := int(-value.Exp)
	if len(digits) <= scale {
		digits = strings.Repeat("0", scale-len(digits)+1) + digits
	}
	point := len(digits) - scale
	return sign + digits[:point] + "." + digits[point:]
}

func numericOptionalString(value pgtype.Numeric) string {
	if !value.Valid {
		return ""
	}
	return numericString(value)
}

func numericPointer(value pgtype.Numeric) *string {
	if !value.Valid {
		return nil
	}
	result := numericString(value)
	return &result
}

func portfolioFromRow(row database.Portfolio) domain.Portfolio {
	return domain.Portfolio{ID: row.ID.String(), Name: row.Name, ReportingCurrency: row.ReportingCurrency, Metadata: decodeMetadata(row.Metadata), CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC()}
}

func accountFromRow(row database.Account) domain.Account {
	return domain.Account{ID: row.ID.String(), PortfolioID: row.PortfolioID.String(), Name: row.Name, Metadata: decodeMetadata(row.Metadata), CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC()}
}

func decodeMetadata(value []byte) map[string]any {
	result := map[string]any{}
	if json.Unmarshal(value, &result) != nil || result == nil {
		return map[string]any{}
	}
	return result
}

func mapDatabaseError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505", "23503", "23514", "55000":
			return ErrConflict
		}
	}
	return err
}
