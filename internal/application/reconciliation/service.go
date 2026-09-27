package reconciliation

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	domain "github.com/oplosy/atrisk/internal/domain/reconciliation"
)

var (
	ErrInvalidRequest = errors.New("invalid reconciliation request")
	ErrNotFound       = errors.New("reconciliation resource not found")
	ErrConflict       = errors.New("reconciliation conflict")
	ErrDatabase       = errors.New("reconciliation database unavailable")
)

type ConflictError struct {
	Code    string
	Message string
}

func (e *ConflictError) Error() string { return e.Code + ": " + e.Message }
func (e *ConflictError) Unwrap() error { return ErrConflict }

type Service struct{ Pool *pgxpool.Pool }

var decimalPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)

type valuationInfo struct {
	id, snapshotID, portfolioID, state, reportingCurrency string
	cutoff                                                time.Time
}

type valueLine struct {
	id, accountID string
	amount        *big.Rat
}

func (s Service) CreateTolerance(ctx context.Context, accountID string, request domain.ToleranceRequest) (domain.ToleranceVersion, error) {
	if s.Pool == nil || !validUUID(accountID) || !validDecimal(request.ToleranceAmount) {
		return domain.ToleranceVersion{}, ErrInvalidRequest
	}
	tolerance, err := parseDecimal(request.ToleranceAmount)
	if err != nil || tolerance.Sign() < 0 {
		return domain.ToleranceVersion{}, ErrInvalidRequest
	}
	tolerance = scaleDecimal(tolerance)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.ToleranceVersion{}, fmt.Errorf("begin tolerance transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var lockedID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1::uuid FOR UPDATE`, accountID).Scan(&lockedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ToleranceVersion{}, ErrNotFound
		}
		return domain.ToleranceVersion{}, fmt.Errorf("lock tolerance account: %w", err)
	}
	var version int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM reconciliation_tolerance_versions WHERE account_id=$1::uuid`, accountID).Scan(&version); err != nil {
		return domain.ToleranceVersion{}, fmt.Errorf("read tolerance version: %w", err)
	}
	var result domain.ToleranceVersion
	if err := tx.QueryRow(ctx, `INSERT INTO reconciliation_tolerance_versions (account_id,version,tolerance_amount) VALUES ($1::uuid,$2,$3) RETURNING id::text,created_at`, accountID, version, formatDecimal(tolerance)).Scan(&result.ID, &result.CreatedAt); err != nil {
		return domain.ToleranceVersion{}, fmt.Errorf("insert tolerance version: %w", err)
	}
	result.AccountID = accountID
	result.Version = version
	result.ToleranceAmount = formatDecimal(tolerance)
	if err := tx.Commit(ctx); err != nil {
		return domain.ToleranceVersion{}, fmt.Errorf("commit tolerance version: %w", err)
	}
	return result, nil
}

func (s Service) CreateToleranceVersion(ctx context.Context, accountID string, request domain.ToleranceRequest) (domain.ToleranceVersion, error) {
	return s.CreateTolerance(ctx, accountID, request)
}

func (s Service) Create(ctx context.Context, valuationID string, request domain.Request) (domain.Checkpoint, error) {
	if s.Pool == nil || !validUUID(valuationID) || !validRequest(request) {
		return domain.Checkpoint{}, ErrInvalidRequest
	}
	request.AccountID = strings.TrimSpace(request.AccountID)
	request.Currency = strings.ToUpper(strings.TrimSpace(request.Currency))
	request.SourceLabel = strings.TrimSpace(request.SourceLabel)
	request.Cutoff = request.Cutoff.UTC()
	request.ExternalNAV = strings.TrimSpace(request.ExternalNAV)
	externalNAV, err := parseDecimal(request.ExternalNAV)
	if err != nil {
		return domain.Checkpoint{}, ErrInvalidRequest
	}
	externalNAV = scaleDecimal(externalNAV)
	seen := make(map[string]struct{}, len(request.LineChecks))
	for _, check := range request.LineChecks {
		if !validUUID(check.SnapshotLineID) || !validDecimal(check.ExternalAmount) {
			return domain.Checkpoint{}, ErrInvalidRequest
		}
		if _, exists := seen[check.SnapshotLineID]; exists {
			return domain.Checkpoint{}, conflict("LINE_CHECK_DUPLICATE", "the same snapshot line was supplied more than once")
		}
		seen[check.SnapshotLineID] = struct{}{}
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.Checkpoint{}, fmt.Errorf("begin reconciliation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var accountPortfolioID string
	if err := tx.QueryRow(ctx, `SELECT portfolio_id::text FROM accounts WHERE id=$1::uuid FOR UPDATE`, request.AccountID).Scan(&accountPortfolioID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Checkpoint{}, ErrNotFound
		}
		return domain.Checkpoint{}, fmt.Errorf("lock reconciliation account: %w", err)
	}
	var valuation valuationInfo
	if err := tx.QueryRow(ctx, `SELECT vr.id::text,vr.snapshot_id::text,ps.portfolio_id::text,vr.state,vr.cutoff,p.reporting_currency FROM valuation_runs vr JOIN portfolio_snapshots ps ON ps.id=vr.snapshot_id JOIN portfolios p ON p.id=ps.portfolio_id WHERE vr.id=$1::uuid`, valuationID).Scan(&valuation.id, &valuation.snapshotID, &valuation.portfolioID, &valuation.state, &valuation.cutoff, &valuation.reportingCurrency); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Checkpoint{}, ErrNotFound
		}
		return domain.Checkpoint{}, fmt.Errorf("load valuation: %w", err)
	}
	if valuation.state != "valid" {
		return domain.Checkpoint{}, conflict("VALUATION_NOT_VALID", "only valid valuations can be reconciled")
	}
	if valuation.reportingCurrency != request.Currency {
		return domain.Checkpoint{}, conflict("CURRENCY_MISMATCH", "reconciliation currency must equal the portfolio reporting currency")
	}
	if !valuation.cutoff.UTC().Equal(request.Cutoff) {
		return domain.Checkpoint{}, conflict("CUTOFF_MISMATCH", "reconciliation cutoff must equal the valuation cutoff")
	}

	rows, err := tx.Query(ctx, `
		SELECT sl.id::text, sl.account_id::text, vl.id::text,
		       CASE WHEN $3='TRY' THEN vl.try_amount::text ELSE vl.usd_amount::text END,
		       vl.state
		FROM portfolio_snapshot_lines sl
		LEFT JOIN valuation_lines vl ON vl.snapshot_line_id=sl.id AND vl.run_id=$1::uuid
		WHERE sl.snapshot_id=$2::uuid
		ORDER BY sl.id`, valuationID, valuation.snapshotID, request.Currency)
	if err != nil {
		return domain.Checkpoint{}, fmt.Errorf("load valuation lines: %w", err)
	}
	defer rows.Close()
	allLines := make(map[string]valueLine)
	accountLines := make(map[string]valueLine)
	expectedAccountLines := make(map[string]struct{})
	invalidAccountLines := make(map[string]bool)
	valuationLineCounts := make(map[string]int)
	for rows.Next() {
		var lineID, accountID string
		var valuationLineID, amountText, lineStateText pgtype.Text
		if err := rows.Scan(&lineID, &accountID, &valuationLineID, &amountText, &lineStateText); err != nil {
			return domain.Checkpoint{}, fmt.Errorf("scan valuation line: %w", err)
		}
		if accountID == request.AccountID {
			expectedAccountLines[lineID] = struct{}{}
		}
		if valuationLineID.Valid {
			valuationLineCounts[lineID]++
		}
		if !valuationLineID.Valid || !amountText.Valid || !lineStateText.Valid || lineStateText.String != "valid" {
			continue
		}
		rat, parseErr := parseDecimal(amountText.String)
		if parseErr != nil {
			if accountID == request.AccountID {
				invalidAccountLines[lineID] = true
			}
			continue
		}
		line := valueLine{id: lineID, accountID: accountID, amount: rat}
		allLines[lineID] = line
		if accountID == request.AccountID {
			accountLines[lineID] = line
		}
	}
	if err := rows.Err(); err != nil {
		return domain.Checkpoint{}, fmt.Errorf("read valuation lines: %w", err)
	}
	if len(expectedAccountLines) == 0 {
		if accountPortfolioID == valuation.portfolioID {
			return domain.Checkpoint{}, conflict("ACCOUNT_HAS_NO_LINES", "account has no lines in the valuation snapshot")
		}
		return domain.Checkpoint{}, conflict("ACCOUNT_NOT_IN_SNAPSHOT", "account is not part of the valuation snapshot")
	}
	for lineID := range expectedAccountLines {
		switch {
		case valuationLineCounts[lineID] > 1:
			return domain.Checkpoint{}, conflict("VALUATION_LINE_DUPLICATE", "valuation has more than one line for a snapshot line")
		case valuationLineCounts[lineID] == 0:
			return domain.Checkpoint{}, conflict("VALUATION_ACCOUNT_LINES_INCOMPLETE", "valuation is missing an account snapshot line")
		case invalidAccountLines[lineID]:
			return domain.Checkpoint{}, conflict("VALUATION_ACCOUNT_LINES_INCOMPLETE", "valuation account line has an invalid amount")
		case accountLines[lineID].amount == nil:
			return domain.Checkpoint{}, conflict("VALUATION_ACCOUNT_LINES_INCOMPLETE", "valuation account line is not valid with a non-null amount")
		}
	}
	var valuationNAV = new(big.Rat)
	for _, line := range accountLines {
		valuationNAV.Add(valuationNAV, line.amount)
	}

	var toleranceVersion int
	var toleranceText string
	if err := tx.QueryRow(ctx, `SELECT version,tolerance_amount::text FROM reconciliation_tolerance_versions WHERE account_id=$1::uuid ORDER BY version DESC LIMIT 1`, request.AccountID).Scan(&toleranceVersion, &toleranceText); errors.Is(err, pgx.ErrNoRows) {
		toleranceVersion = 0
		toleranceText = formatDecimal(defaultTolerance(externalNAV))
	} else if err != nil {
		return domain.Checkpoint{}, fmt.Errorf("load tolerance version: %w", err)
	}
	tolerance, err := parseDecimal(toleranceText)
	if err != nil {
		return domain.Checkpoint{}, fmt.Errorf("parse tolerance: %w", err)
	}
	difference := new(big.Rat).Sub(externalNAV, valuationNAV)
	absoluteDifference := new(big.Rat).Abs(difference)
	var relativeDifference *string
	if externalNAV.Sign() != 0 {
		ratio := new(big.Rat).Quo(absoluteDifference, new(big.Rat).Abs(externalNAV))
		value := formatDecimal(ratio)
		relativeDifference = &value
	}
	state := domain.StateUnreconciled
	if absoluteDifference.Cmp(tolerance) <= 0 {
		state = domain.StateReconciled
	}

	lineState := domain.LineCheckNone
	lineResults := make([]domain.LineCheck, 0, len(request.LineChecks))
	if len(request.LineChecks) > 0 {
		lineState = domain.LineCheckPartial
		lineTotal := new(big.Rat)
		for _, input := range request.LineChecks {
			line, exists := allLines[input.SnapshotLineID]
			if !exists {
				var lineAccount string
				if err := tx.QueryRow(ctx, `SELECT account_id::text FROM portfolio_snapshot_lines WHERE id=$1::uuid AND snapshot_id=$2::uuid`, input.SnapshotLineID, valuation.snapshotID).Scan(&lineAccount); errors.Is(err, pgx.ErrNoRows) {
					return domain.Checkpoint{}, conflict("LINE_CHECK_UNKNOWN", "snapshot line is not part of the valuation snapshot")
				} else if err != nil {
					return domain.Checkpoint{}, fmt.Errorf("check line membership: %w", err)
				} else {
					if lineAccount == request.AccountID {
						return domain.Checkpoint{}, conflict("LINE_CHECK_UNKNOWN", "snapshot line has no valid persisted valuation amount")
					}
					return domain.Checkpoint{}, conflict("LINE_CHECK_CROSS_ACCOUNT", "snapshot line belongs to another account")
				}
			}
			if line.accountID != request.AccountID {
				return domain.Checkpoint{}, conflict("LINE_CHECK_CROSS_ACCOUNT", "snapshot line belongs to another account")
			}
			externalAmount, parseErr := parseDecimal(input.ExternalAmount)
			if parseErr != nil {
				return domain.Checkpoint{}, ErrInvalidRequest
			}
			externalAmount = scaleDecimal(externalAmount)
			lineDifference := new(big.Rat).Sub(externalAmount, line.amount)
			lineAbsolute := new(big.Rat).Abs(lineDifference)
			lineTotal.Add(lineTotal, externalAmount)
			lineResults = append(lineResults, domain.LineCheck{SnapshotLineID: line.id, ExternalAmount: formatDecimal(externalAmount), ValuationAmount: formatDecimal(line.amount), Difference: formatDecimal(lineDifference), AbsoluteDifference: formatDecimal(lineAbsolute)})
		}
		if len(request.LineChecks) == len(accountLines) {
			if lineTotal.Cmp(externalNAV) != 0 {
				return domain.Checkpoint{}, conflict("LINE_CHECK_TOTAL_MISMATCH", "complete line checks do not sum to external NAV")
			}
			lineState = domain.LineCheckComplete
		} else if request.LineChecksComplete {
			return domain.Checkpoint{}, conflict("LINE_CHECK_INCOMPLETE", "complete line checks must include every account line")
		}
	} else if request.LineChecksComplete {
		return domain.Checkpoint{}, conflict("LINE_CHECK_INCOMPLETE", "complete line checks must include every account line")
	}

	var result domain.Checkpoint
	var reason *string
	if err := tx.QueryRow(ctx, `INSERT INTO reconciliation_checkpoints (valuation_id,account_id,source_label,currency,cutoff,external_nav,valuation_nav,absolute_difference,relative_difference,effective_tolerance,tolerance_version,state,reason_code,line_check_state) VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) RETURNING id::text,created_at`, valuationID, request.AccountID, request.SourceLabel, request.Currency, request.Cutoff, formatDecimal(externalNAV), formatDecimal(valuationNAV), formatDecimal(absoluteDifference), nullableString(relativeDifference), formatDecimal(tolerance), toleranceVersion, state, reason, lineState).Scan(&result.ID, &result.CreatedAt); err != nil {
		return domain.Checkpoint{}, fmt.Errorf("insert reconciliation checkpoint: %w", err)
	}
	for _, line := range lineResults {
		if _, err := tx.Exec(ctx, `INSERT INTO reconciliation_line_checks (reconciliation_id,snapshot_line_id,external_amount,valuation_amount,difference,absolute_difference) VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6)`, result.ID, line.SnapshotLineID, line.ExternalAmount, line.ValuationAmount, line.Difference, line.AbsoluteDifference); err != nil {
			return domain.Checkpoint{}, fmt.Errorf("insert line check: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Checkpoint{}, fmt.Errorf("commit reconciliation checkpoint: %w", err)
	}
	result.ValuationID = valuationID
	result.AccountID = request.AccountID
	result.SourceLabel = request.SourceLabel
	result.Currency = request.Currency
	result.Cutoff = request.Cutoff
	result.ExternalNAV = formatDecimal(externalNAV)
	result.ValuationNAV = formatDecimal(valuationNAV)
	result.AbsoluteDifference = formatDecimal(absoluteDifference)
	result.RelativeDifference = relativeDifference
	result.EffectiveTolerance = formatDecimal(tolerance)
	result.ToleranceVersion = toleranceVersion
	result.State = state
	result.LineCheckState = lineState
	result.LineChecks = lineResults
	return result, nil
}

func (s Service) Get(ctx context.Context, id string) (domain.Checkpoint, error) {
	if s.Pool == nil || !validUUID(id) {
		return domain.Checkpoint{}, ErrInvalidRequest
	}
	var result domain.Checkpoint
	var relative, reason *string
	if err := s.Pool.QueryRow(ctx, `SELECT id::text,valuation_id::text,account_id::text,source_label,currency,cutoff,external_nav::text,valuation_nav::text,absolute_difference::text,relative_difference::text,effective_tolerance::text,tolerance_version,state,reason_code,line_check_state,created_at FROM reconciliation_checkpoints WHERE id=$1::uuid`, id).Scan(&result.ID, &result.ValuationID, &result.AccountID, &result.SourceLabel, &result.Currency, &result.Cutoff, &result.ExternalNAV, &result.ValuationNAV, &result.AbsoluteDifference, &relative, &result.EffectiveTolerance, &result.ToleranceVersion, &result.State, &reason, &result.LineCheckState, &result.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Checkpoint{}, ErrNotFound
		}
		return domain.Checkpoint{}, fmt.Errorf("get reconciliation checkpoint: %w", err)
	}
	result.RelativeDifference = relative
	result.ReasonCode = reason
	rows, err := s.Pool.Query(ctx, `SELECT snapshot_line_id::text,external_amount::text,valuation_amount::text,difference::text,absolute_difference::text FROM reconciliation_line_checks WHERE reconciliation_id=$1::uuid ORDER BY snapshot_line_id`, id)
	if err != nil {
		return domain.Checkpoint{}, fmt.Errorf("get reconciliation line checks: %w", err)
	}
	defer rows.Close()
	result.LineChecks = []domain.LineCheck{}
	for rows.Next() {
		var line domain.LineCheck
		if err := rows.Scan(&line.SnapshotLineID, &line.ExternalAmount, &line.ValuationAmount, &line.Difference, &line.AbsoluteDifference); err != nil {
			return domain.Checkpoint{}, fmt.Errorf("scan reconciliation line check: %w", err)
		}
		result.LineChecks = append(result.LineChecks, line)
	}
	if err := rows.Err(); err != nil {
		return domain.Checkpoint{}, fmt.Errorf("read reconciliation line checks: %w", err)
	}
	return result, nil
}

func validRequest(request domain.Request) bool {
	return validUUID(request.AccountID) && strings.TrimSpace(request.SourceLabel) != "" && (strings.EqualFold(request.Currency, domain.CurrencyTRY) || strings.EqualFold(request.Currency, domain.CurrencyUSD)) && !request.Cutoff.IsZero() && validDecimal(request.ExternalNAV)
}

func validUUID(value string) bool {
	var id pgtype.UUID
	return id.Scan(strings.TrimSpace(value)) == nil && id.Valid
}

func validDecimal(value string) bool {
	return decimalPattern.MatchString(strings.TrimSpace(value))
}

func parseDecimal(value string) (*big.Rat, error) {
	result := new(big.Rat)
	if !validDecimal(value) {
		return nil, ErrInvalidRequest
	}
	if _, ok := result.SetString(strings.TrimSpace(value)); !ok {
		return nil, ErrInvalidRequest
	}
	return result, nil
}

func formatDecimal(value *big.Rat) string {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	numerator := new(big.Int).Mul(value.Num(), scale)
	quotient, remainder := new(big.Int), new(big.Int)
	quotient.QuoRem(numerator, value.Denom(), remainder)
	if remainder.Sign() != 0 {
		double := new(big.Int).Abs(remainder)
		double.Lsh(double, 1)
		if double.Cmp(value.Denom()) >= 0 {
			if numerator.Sign() < 0 {
				quotient.Sub(quotient, big.NewInt(1))
			} else {
				quotient.Add(quotient, big.NewInt(1))
			}
		}
	}
	digits := quotient.String()
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	if len(digits) <= 18 {
		digits = strings.Repeat("0", 19-len(digits)) + digits
	}
	point := len(digits) - 18
	return sign + digits[:point] + "." + digits[point:]
}

func scaleDecimal(value *big.Rat) *big.Rat {
	result, err := parseDecimal(formatDecimal(value))
	if err != nil {
		return new(big.Rat)
	}
	return result
}

func defaultTolerance(externalNAV *big.Rat) *big.Rat {
	minimum := new(big.Rat).SetFrac(big.NewInt(1), big.NewInt(100))
	basisPoint := new(big.Rat).Quo(new(big.Rat).Abs(externalNAV), big.NewRat(10000, 1))
	if basisPoint.Cmp(minimum) > 0 {
		return basisPoint
	}
	return minimum
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func conflict(code, message string) error { return &ConflictError{Code: code, Message: message} }
