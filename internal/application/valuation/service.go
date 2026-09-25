package valuation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	domain "github.com/oplosy/atrisk/internal/domain/valuation"
	"golang.org/x/text/currency"
)

var (
	ErrInvalidRequest = errors.New("invalid valuation request")
	ErrNotFound       = errors.New("valuation resource not found")
	ErrDatabase       = errors.New("valuation database unavailable")
)

const maxAgeSeconds int64 = 9223372036

type Service struct{ Pool *pgxpool.Pool }

type snapshotLine struct {
	id, instrumentID, nativeCurrency, instrumentType string
	quantity                                         *big.Rat
}

type priceRevision struct {
	id, quote, rawID string
	observation      time.Time
	price            *big.Rat
	sourceKnown      *time.Time
	systemKnown      time.Time
}

type fxRevision struct {
	id, base, quote, rawID string
	observation            time.Time
	rate                   *big.Rat
	sourceKnown            *time.Time
	systemKnown            time.Time
}

type selectedPath struct {
	rate  *big.Rat
	edges []domain.FXPathEntry
}

type resultLine struct {
	domain.Line
	tryRat, usdRat *big.Rat
}

func (s Service) Create(ctx context.Context, request domain.Request) (domain.Run, error) {
	if s.Pool == nil || !validRequest(request) {
		return domain.Run{}, ErrInvalidRequest
	}
	request.Cutoff = request.Cutoff.UTC()
	request.KnownAt = request.KnownAt.UTC()
	lines, _, err := s.loadSnapshot(ctx, request.SnapshotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Run{}, ErrNotFound
	}
	if err != nil {
		return domain.Run{}, fmt.Errorf("load snapshot: %w", err)
	}
	prices := make(map[string][]priceRevision)
	fx, err := s.loadFX(ctx, request)
	if err != nil {
		return domain.Run{}, fmt.Errorf("load fx revisions: %w", err)
	}
	results := make([]resultLine, 0, len(lines))
	tryTotal, usdTotal := new(big.Rat), new(big.Rat)
	blocked := false
	for _, line := range lines {
		if _, ok := prices[line.instrumentID]; !ok && line.instrumentType != "cash" && line.instrumentType != "currency" {
			prices[line.instrumentID], err = s.loadPrices(ctx, line.instrumentID, request)
			if err != nil {
				return domain.Run{}, fmt.Errorf("load prices: %w", err)
			}
		}
		result := resultLine{Line: domain.Line{SnapshotLineID: line.id, InstrumentID: line.instrumentID, NativeCurrency: line.nativeCurrency, ReasonCodes: []domain.Reason{}, TryFXPath: []domain.FXPathEntry{}, USDFXPath: []domain.FXPathEntry{}}}
		var amount *big.Rat
		var quote string
		if line.instrumentType == "cash" || line.instrumentType == "currency" {
			result.PriceMethod = "identity"
			amount = new(big.Rat).Set(line.quantity)
			quote = line.nativeCurrency
		} else {
			price := selectPrice(prices[line.instrumentID])
			if price == nil {
				result.State = domain.StateBlocked
				result.PriceMethod = "revision"
				result.ReasonCodes = append(result.ReasonCodes, domain.Reason{Code: "PRICE_MISSING_OR_STALE", Message: "no eligible price revision at the requested cutoff and knowledge clock"})
				blocked = true
				results = append(results, result)
				continue
			}
			result.PriceMethod = "revision"
			result.PriceRevisionID = stringPointer(price.id)
			result.PriceQuoteUnit = stringPointer(price.quote)
			amount = new(big.Rat).Mul(line.quantity, price.price)
			quote = price.quote
		}
		result.NativeAmount = stringPointer(formatDecimal(amount))
		result.State = domain.StateValid
		tryPath, tryErr := pathFor(quote, "TRY", fx)
		usdPath, usdErr := pathFor(quote, "USD", fx)
		if quote == "TRY" {
			tryPath = &selectedPath{rate: new(big.Rat).SetInt64(1)}
		}
		if quote == "USD" {
			usdPath = &selectedPath{rate: new(big.Rat).SetInt64(1)}
		}
		if tryErr != nil || tryPath == nil {
			result.ReasonCodes = append(result.ReasonCodes, fxReason("TRY", tryErr))
		} else {
			result.tryRat = new(big.Rat).Mul(amount, tryPath.rate)
			result.TryAmount = stringPointer(formatDecimal(result.tryRat))
			result.TryFXPath = append(result.TryFXPath, tryPath.edges...)
			tryTotal.Add(tryTotal, roundedDecimal(result.tryRat))
		}
		if usdErr != nil || usdPath == nil {
			result.ReasonCodes = append(result.ReasonCodes, fxReason("USD", usdErr))
		} else {
			result.usdRat = new(big.Rat).Mul(amount, usdPath.rate)
			result.USDAmount = stringPointer(formatDecimal(result.usdRat))
			result.USDFXPath = append(result.USDFXPath, usdPath.edges...)
			usdTotal.Add(usdTotal, roundedDecimal(result.usdRat))
		}
		if len(result.ReasonCodes) > 0 {
			result.State = domain.StateBlocked
			blocked = true
		}
		results = append(results, result)
	}
	state := domain.StateValid
	if blocked {
		state = domain.StateBlocked
	}
	linesOut := make([]domain.Line, 0, len(results))
	for _, result := range results {
		linesOut = append(linesOut, result.Line)
	}
	run := domain.Run{SnapshotID: request.SnapshotID, Cutoff: request.Cutoff.UTC(), KnowledgeMode: request.KnowledgeMode, KnownAt: request.KnownAt.UTC(), PriceMaxAgeSeconds: request.PriceMaxAgeSeconds, FXMaxAgeSeconds: request.FXMaxAgeSeconds, State: state, Lines: linesOut, Totals: domain.Totals{TRY: stringPointer(formatDecimal(tryTotal)), USD: stringPointer(formatDecimal(usdTotal))}}
	run.ResultHash = resultHash(request, results, run.Totals)
	run.ID, run.CreatedAt, err = s.persist(ctx, request, run, results)
	if err != nil {
		return domain.Run{}, err
	}
	return s.get(ctx, run.ID)
}

func (s Service) Get(ctx context.Context, id string) (domain.Run, error) {
	if s.Pool == nil || !validUUID(id) {
		return domain.Run{}, ErrInvalidRequest
	}
	return s.get(ctx, id)
}

func validRequest(r domain.Request) bool {
	return validUUID(r.SnapshotID) && !r.Cutoff.IsZero() && !r.KnownAt.IsZero() && (r.KnowledgeMode == domain.KnowledgeSystem || r.KnowledgeMode == domain.KnowledgeSource) && r.PriceMaxAgeSeconds >= 0 && r.PriceMaxAgeSeconds <= maxAgeSeconds && r.FXMaxAgeSeconds >= 0 && r.FXMaxAgeSeconds <= maxAgeSeconds
}

func validUUID(value string) bool {
	var id pgtype.UUID
	return id.Scan(strings.TrimSpace(value)) == nil && id.Valid
}

func (s Service) loadSnapshot(ctx context.Context, id string) ([]snapshotLine, string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT l.id::text, l.instrument_id::text, l.quantity::text, i.native_currency, i.instrument_type, p.reporting_currency FROM portfolio_snapshots AS ps JOIN portfolio_snapshot_lines AS l ON l.snapshot_id=ps.id JOIN instruments AS i ON i.id=l.instrument_id JOIN portfolios AS p ON p.id=ps.portfolio_id WHERE ps.id=$1::uuid ORDER BY l.id`, id)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var result []snapshotLine
	reporting := ""
	for rows.Next() {
		var line snapshotLine
		var quantity string
		if err := rows.Scan(&line.id, &line.instrumentID, &quantity, &line.nativeCurrency, &line.instrumentType, &reporting); err != nil {
			return nil, "", err
		}
		line.quantity, err = parseDecimal(quantity)
		if err != nil {
			return nil, "", err
		}
		result = append(result, line)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if reporting == "" {
		return nil, "", pgx.ErrNoRows
	}
	return result, reporting, nil
}

func (s Service) loadPrices(ctx context.Context, instrumentID string, request domain.Request) ([]priceRevision, error) {
	knownClause := "system_known_at <= $4"
	orderClause := "observation_time DESC, system_known_at DESC, id ASC"
	if request.KnowledgeMode == domain.KnowledgeSource {
		knownClause = "source_known_at IS NOT NULL AND source_known_at <= $4"
		orderClause = "observation_time DESC, source_known_at DESC, system_known_at DESC, id ASC"
	}
	query := fmt.Sprintf(`SELECT id::text, quote_currency, observation_time, price::text, source_known_at, system_known_at, raw_object_id::text FROM price_revisions WHERE instrument_id=$1::uuid AND observation_time <= $2 AND observation_time >= $2 - ($3 * interval '1 second') AND %s ORDER BY %s`, knownClause, orderClause)
	rows, err := s.Pool.Query(ctx, query, instrumentID, request.Cutoff.UTC(), request.PriceMaxAgeSeconds, request.KnownAt.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []priceRevision
	seen := map[string]bool{}
	for rows.Next() {
		var item priceRevision
		var price string
		var source *time.Time
		if err := rows.Scan(&item.id, &item.quote, &item.observation, &price, &source, &item.systemKnown, &item.rawID); err != nil {
			return nil, err
		}
		item.sourceKnown = source
		if !eligibleRevision(item.observation, item.sourceKnown, item.systemKnown, request.KnowledgeMode, request.Cutoff, request.KnownAt, request.PriceMaxAgeSeconds) {
			continue
		}
		if seen[item.quote] {
			continue
		}
		item.sourceKnown = source
		item.price, err = parseDecimal(price)
		if err != nil {
			return nil, err
		}
		seen[item.quote] = true
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s Service) loadFX(ctx context.Context, request domain.Request) ([]fxRevision, error) {
	knownClause := "system_known_at <= $2"
	orderClause := "base_currency, quote_currency, observation_time DESC, system_known_at DESC, id ASC"
	if request.KnowledgeMode == domain.KnowledgeSource {
		knownClause = "source_known_at IS NOT NULL AND source_known_at <= $2"
		orderClause = "base_currency, quote_currency, observation_time DESC, source_known_at DESC, system_known_at DESC, id ASC"
	}
	query := fmt.Sprintf(`SELECT id::text, base_currency, quote_currency, observation_time, rate::text, source_known_at, system_known_at, raw_object_id::text FROM fx_quote_revisions WHERE observation_time <= $1 AND observation_time >= $1 - ($3 * interval '1 second') AND %s ORDER BY %s`, knownClause, orderClause)
	rows, err := s.Pool.Query(ctx, query, request.Cutoff.UTC(), request.KnownAt.UTC(), request.FXMaxAgeSeconds)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []fxRevision
	seen := map[string]bool{}
	for rows.Next() {
		var item fxRevision
		var rate string
		var source *time.Time
		if err := rows.Scan(&item.id, &item.base, &item.quote, &item.observation, &rate, &source, &item.systemKnown, &item.rawID); err != nil {
			return nil, err
		}
		if !eligibleRevision(item.observation, source, item.systemKnown, request.KnowledgeMode, request.Cutoff, request.KnownAt, request.FXMaxAgeSeconds) {
			continue
		}
		key := item.base + ":" + item.quote
		if seen[key] {
			continue
		}
		item.sourceKnown = source
		item.rate, err = parseDecimal(rate)
		if err != nil {
			return nil, err
		}
		seen[key] = true
		result = append(result, item)
	}
	return result, rows.Err()
}

func eligibleRevision(observation time.Time, sourceKnown *time.Time, systemKnown time.Time, knowledgeMode string, cutoff time.Time, knownAt time.Time, maxAgeSeconds int64) bool {
	if observation.After(cutoff) || cutoff.Sub(observation) > time.Duration(maxAgeSeconds)*time.Second {
		return false
	}
	if knowledgeMode == domain.KnowledgeSource {
		return sourceKnown != nil && !sourceKnown.After(knownAt)
	}
	return !systemKnown.After(knownAt)
}

func selectPrice(prices []priceRevision) *priceRevision {
	if len(prices) == 0 {
		return nil
	}
	return &prices[0]
}

func pathFor(source, target string, quotes []fxRevision) (*selectedPath, error) {
	if !isFiatCode(source) || !isFiatCode(target) {
		return nil, errors.New("non-fiat asset code cannot be used as an FX currency")
	}
	if source == target {
		return &selectedPath{rate: new(big.Rat).SetInt64(1)}, nil
	}
	var direct []fxRevision
	for _, quote := range quotes {
		if quote.base == source && quote.quote == target {
			direct = append(direct, quote)
		} else if quote.base == target && quote.quote == source {
			copy := quote
			if copy.rate != nil && copy.rate.Sign() > 0 {
				copy.rate = new(big.Rat).Inv(copy.rate)
			}
			direct = append(direct, copy)
		}
	}
	if len(direct) > 0 {
		sort.SliceStable(direct, func(i, j int) bool {
			if !direct[i].observation.Equal(direct[j].observation) {
				return direct[i].observation.After(direct[j].observation)
			}
			if direct[i].sourceKnown != nil && direct[j].sourceKnown != nil && !direct[i].sourceKnown.Equal(*direct[j].sourceKnown) {
				return direct[i].sourceKnown.After(*direct[j].sourceKnown)
			}
			if !direct[i].systemKnown.Equal(direct[j].systemKnown) {
				return direct[i].systemKnown.After(direct[j].systemKnown)
			}
			return direct[i].id < direct[j].id
		})
		q := direct[0]
		if q.rate == nil || q.rate.Sign() <= 0 {
			return nil, errors.New("FX rate must be positive")
		}
		direction := "forward"
		if q.base == target {
			direction = "reverse"
		}
		return &selectedPath{rate: q.rate, edges: []domain.FXPathEntry{{QuoteRevisionID: q.id, Direction: direction}}}, nil
	}
	if source == "USD" || target == "USD" {
		return nil, errors.New("no direct USD path")
	}
	first, firstDir := bridgeQuote(source, "USD", quotes)
	second, secondDir := bridgeQuote("USD", target, quotes)
	if first == nil || second == nil {
		return nil, errors.New("no USD bridge")
	}
	return &selectedPath{rate: new(big.Rat).Mul(first.rate, second.rate), edges: []domain.FXPathEntry{{QuoteRevisionID: first.id, Direction: firstDir}, {QuoteRevisionID: second.id, Direction: secondDir}}}, nil
}

func bridgeQuote(source, target string, quotes []fxRevision) (*fxRevision, string) {
	for i := range quotes {
		q := quotes[i]
		if q.base == source && q.quote == target {
			if q.rate == nil || q.rate.Sign() <= 0 {
				return nil, ""
			}
			return &q, "forward"
		}
		if q.base == target && q.quote == source {
			if q.rate == nil || q.rate.Sign() <= 0 {
				return nil, ""
			}
			q.rate = new(big.Rat).Inv(q.rate)
			return &q, "reverse"
		}
	}
	return nil, ""
}

func isFiatCode(code string) bool {
	code = strings.ToUpper(strings.TrimSpace(code))
	if _, err := currency.ParseISO(code); err != nil {
		return false
	}
	switch code {
	case "XXX", "XAU", "XAG", "XDR", "XBA", "XBB", "XBC", "XBD", "XTS":
		return false
	default:
		return true
	}
}

func fxReason(target string, err error) domain.Reason {
	if err != nil && strings.Contains(err.Error(), "positive") {
		return domain.Reason{Code: "FX_INVALID_RATE", Message: "eligible FX quote has a non-positive rate for " + target}
	}
	return domain.Reason{Code: "FX_" + target + "_MISSING_OR_STALE", Message: "no explicit eligible conversion path to " + target}
}

func (s Service) persist(ctx context.Context, request domain.Request, run domain.Run, lines []resultLine) (string, time.Time, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("begin valuation transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	requestJSON, _ := json.Marshal(request)
	var runID string
	var created time.Time
	if err := tx.QueryRow(ctx, `INSERT INTO valuation_runs (snapshot_id,cutoff,knowledge_mode,known_at,price_max_age_seconds,fx_max_age_seconds,request,state,result_hash) VALUES ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id::text,created_at`, request.SnapshotID, request.Cutoff.UTC(), request.KnowledgeMode, request.KnownAt.UTC(), request.PriceMaxAgeSeconds, request.FXMaxAgeSeconds, requestJSON, run.State, run.ResultHash).Scan(&runID, &created); err != nil {
		return "", time.Time{}, fmt.Errorf("insert valuation run: %w", err)
	}
	for _, line := range lines {
		reasons, _ := json.Marshal(line.ReasonCodes)
		var priceID any
		if line.PriceRevisionID != nil {
			priceID = *line.PriceRevisionID
		}
		tryIDs, tryDirections, err := pathArrays(line.TryFXPath)
		if err != nil {
			return "", time.Time{}, err
		}
		usdIDs, usdDirections, err := pathArrays(line.USDFXPath)
		if err != nil {
			return "", time.Time{}, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO valuation_lines (run_id,snapshot_line_id,native_currency,native_amount,try_amount,usd_amount,state,reason_codes,price_method,price_revision_id,price_quote_unit,try_fx_quote_revision_ids,try_fx_directions,usd_fx_quote_revision_ids,usd_fx_directions) VALUES ($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9,$10::uuid,$11,$12,$13,$14,$15)`, runID, line.SnapshotLineID, line.NativeCurrency, nullableString(line.NativeAmount), nullableString(line.TryAmount), nullableString(line.USDAmount), line.State, reasons, line.PriceMethod, priceID, nullableString(line.PriceQuoteUnit), tryIDs, tryDirections, usdIDs, usdDirections); err != nil {
			return "", time.Time{}, fmt.Errorf("insert valuation line: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return "", time.Time{}, fmt.Errorf("commit valuation: %w", err)
	}
	return runID, created, nil
}

func pathArrays(path []domain.FXPathEntry) ([]pgtype.UUID, []string, error) {
	ids := make([]pgtype.UUID, 0, len(path))
	directions := make([]string, 0, len(path))
	for _, edge := range path {
		var id pgtype.UUID
		if err := id.Scan(edge.QuoteRevisionID); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		directions = append(directions, edge.Direction)
	}
	return ids, directions, nil
}

func fxPathFromArrays(ids []pgtype.UUID, directions []string) []domain.FXPathEntry {
	path := make([]domain.FXPathEntry, 0, len(ids))
	for i, id := range ids {
		if id.Valid && i < len(directions) {
			path = append(path, domain.FXPathEntry{QuoteRevisionID: id.String(), Direction: directions[i]})
		}
	}
	return path
}

func (s Service) get(ctx context.Context, id string) (domain.Run, error) {
	var run domain.Run
	var requestJSON []byte
	if err := s.Pool.QueryRow(ctx, `SELECT id::text,snapshot_id::text,cutoff,knowledge_mode,known_at,price_max_age_seconds,fx_max_age_seconds,state,result_hash,created_at,request FROM valuation_runs WHERE id=$1::uuid`, id).Scan(&run.ID, &run.SnapshotID, &run.Cutoff, &run.KnowledgeMode, &run.KnownAt, &run.PriceMaxAgeSeconds, &run.FXMaxAgeSeconds, &run.State, &run.ResultHash, &run.CreatedAt, &requestJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Run{}, ErrNotFound
		}
		return domain.Run{}, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT vl.id::text,vl.snapshot_line_id::text,sl.instrument_id::text,vl.native_currency,vl.native_amount::text,vl.try_amount::text,vl.usd_amount::text,vl.state,vl.reason_codes,vl.price_method,vl.price_revision_id::text,vl.price_quote_unit,vl.try_fx_quote_revision_ids,vl.try_fx_directions,vl.usd_fx_quote_revision_ids,vl.usd_fx_directions FROM valuation_lines AS vl JOIN portfolio_snapshot_lines AS sl ON sl.id=vl.snapshot_line_id WHERE vl.run_id=$1::uuid ORDER BY vl.id`, id)
	if err != nil {
		return domain.Run{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var line domain.Line
		var native, tryAmount, usdAmount, priceID, priceQuoteUnit *string
		var reasons []byte
		var tryIDs, usdIDs []pgtype.UUID
		var tryDirections, usdDirections []string
		if err := rows.Scan(&line.ID, &line.SnapshotLineID, &line.InstrumentID, &line.NativeCurrency, &native, &tryAmount, &usdAmount, &line.State, &reasons, &line.PriceMethod, &priceID, &priceQuoteUnit, &tryIDs, &tryDirections, &usdIDs, &usdDirections); err != nil {
			return domain.Run{}, err
		}
		line.NativeAmount, line.TryAmount, line.USDAmount, line.PriceRevisionID, line.PriceQuoteUnit = native, tryAmount, usdAmount, priceID, priceQuoteUnit
		_ = json.Unmarshal(reasons, &line.ReasonCodes)
		if line.ReasonCodes == nil {
			line.ReasonCodes = []domain.Reason{}
		}
		line.TryFXPath = fxPathFromArrays(tryIDs, tryDirections)
		line.USDFXPath = fxPathFromArrays(usdIDs, usdDirections)
		run.Lines = append(run.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return domain.Run{}, err
	}
	tryTotal, usdTotal := new(big.Rat), new(big.Rat)
	for _, line := range run.Lines {
		if line.TryAmount != nil {
			if value, err := parseDecimal(*line.TryAmount); err == nil {
				tryTotal.Add(tryTotal, value)
			}
		}
		if line.USDAmount != nil {
			if value, err := parseDecimal(*line.USDAmount); err == nil {
				usdTotal.Add(usdTotal, value)
			}
		}
	}
	run.Totals = domain.Totals{TRY: stringPointer(formatDecimal(tryTotal)), USD: stringPointer(formatDecimal(usdTotal))}
	return run, nil
}

func resultHash(request domain.Request, lines []resultLine, totals domain.Totals) string {
	ids := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		ids = append(ids, map[string]any{"snapshot_line_id": line.SnapshotLineID, "price_revision_id": line.PriceRevisionID, "price_quote_unit": line.PriceQuoteUnit, "try_fx_path": line.TryFXPath, "usd_fx_path": line.USDFXPath, "native_amount": line.NativeAmount, "try_amount": line.TryAmount, "usd_amount": line.USDAmount, "state": line.State})
	}
	payload, _ := json.Marshal(map[string]any{"request": request, "lines": ids, "totals": totals})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func parseDecimal(value string) (*big.Rat, error) {
	result := new(big.Rat)
	if _, ok := result.SetString(strings.TrimSpace(value)); !ok {
		return nil, ErrInvalidRequest
	}
	return result, nil
}

func formatDecimal(value *big.Rat) string {
	if value == nil {
		return ""
	}
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
	sign := ""
	digits := quotient.String()
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	if len(digits) <= 18 {
		digits = strings.Repeat("0", 19-len(digits)) + digits
	}
	point := len(digits) - 18
	return sign + digits[:point] + "." + digits[point:]
}

// roundedDecimal applies the same half-away-from-zero scale reduction used
// before values are stored in NUMERIC(38,18), so totals are sums of persisted
// line values rather than sums of higher-precision intermediates.
func roundedDecimal(value *big.Rat) *big.Rat {
	rounded, err := parseDecimal(formatDecimal(value))
	if err != nil {
		return new(big.Rat)
	}
	return rounded
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
func stringPointer(value string) *string { return &value }
