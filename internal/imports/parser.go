// Package imports contains the bounded, strict CSV import boundary.
package imports

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	SchemaVersion    = "1.0"
	MaxBytes         = 10 << 20
	MaxRows          = 25000
	MaxDiagnostics   = 100
	KindPositions    = "positions"
	KindManualPrices = "manual-prices"
)

var (
	positionHeaders = []string{"account_id", "instrument_id", "quantity", "total_cost_basis", "modified_duration_years", "convexity_years_squared"}
	priceHeaders    = []string{"instrument_id", "quote_currency", "observation_time", "price", "source_known_at"}
	uuidPattern     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	decimalPattern  = regexp.MustCompile(`^[+-]?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)
	unitPattern     = regexp.MustCompile(`^[A-Z0-9]+$`)
)

type Diagnostic struct {
	Row     int    `json:"row"`
	Column  string `json:"column"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Result struct {
	Kind                 string
	ContentSHA256        string
	RowCount             int
	Valid                bool
	Diagnostics          []Diagnostic
	DiagnosticsTruncated bool
	Positions            []PositionRow
	Prices               []PriceRow
}
type PositionRow struct{ AccountID, InstrumentID, Quantity, TotalCostBasis, ModifiedDurationYears, ConvexityYearsSquared string }
type PriceRow struct {
	InstrumentID, QuoteCurrency string
	ObservationTime             time.Time
	Price, SourceKnownAt        string
	SourceKnownAtTime           *time.Time
}

func Parse(body []byte, kind string) Result {
	r := Result{Kind: kind, ContentSHA256: digest(body), Diagnostics: make([]Diagnostic, 0)}
	add := func(row int, col, code, msg string) {
		if len(r.Diagnostics) < MaxDiagnostics {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{row, col, code, msg})
		} else {
			r.DiagnosticsTruncated = true
		}
	}
	if kind != KindPositions && kind != KindManualPrices {
		add(0, "", "unknown_import_kind", "import kind is unsupported")
		return r
	}
	if len(body) > MaxBytes {
		add(0, "", "file_too_large", "CSV file exceeds 10 MiB")
		return r
	}
	if !utf8.Valid(body) {
		add(0, "", "invalid_utf8", "CSV must be valid UTF-8")
		return r
	}
	if bytes.IndexByte(body, 0) >= 0 || hasForbiddenControl(body) {
		add(0, "", "control_byte", "CSV contains a forbidden control byte")
		return r
	}
	if bytes.HasPrefix(body, []byte{0xef, 0xbb, 0xbf}) {
		body = body[3:]
	}
	reader := csv.NewReader(bytes.NewReader(body))
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false
	reader.LazyQuotes = false
	expected := positionHeaders
	if kind == KindManualPrices {
		expected = priceHeaders
	}
	headers, err := reader.Read()
	if err == io.EOF {
		add(1, "", "missing_header", "CSV header is required")
		return r
	}
	if err != nil {
		add(1, "", "invalid_csv", "CSV header is not a valid RFC 4180 record")
		return r
	}
	if !sameHeaders(headers, expected) {
		add(1, "", "invalid_header", "CSV headers must match the ordered template exactly")
		return r
	}
	seen := make(map[string]struct{})
	rows := 0
	for {
		record, readErr := reader.Read()
		if readErr == io.EOF {
			break
		}
		rows++
		if rows > MaxRows {
			add(rows+1, "", "too_many_rows", "CSV exceeds 25,000 data rows")
			break
		}
		if readErr != nil {
			add(rows+1, "", "invalid_csv", "CSV record is not a valid RFC 4180 record")
			continue
		}
		if len(record) != len(expected) {
			add(rows+1, "", "wrong_cell_count", "CSV record has the wrong number of cells")
			continue
		}
		if kind == KindPositions {
			parsePosition(record, rows+1, &r, add, seen)
		} else {
			parsePrice(record, rows+1, &r, add, seen)
		}
	}
	r.RowCount = rows
	r.Valid = len(r.Diagnostics) == 0 && !r.DiagnosticsTruncated && rows > 0
	return r
}

func digest(body []byte) string { sum := sha256.Sum256(body); return hex.EncodeToString(sum[:]) }
func hasForbiddenControl(body []byte) bool {
	for _, b := range body {
		if b < 0x20 && b != '\r' && b != '\n' {
			return true
		}
		if b == 0x7f {
			return true
		}
	}
	return false
}
func sameHeaders(got, expected []string) bool {
	if len(got) != len(expected) {
		return false
	}
	for i := range expected {
		if got[i] != expected[i] {
			return false
		}
	}
	return true
}
func rejectText(value string) bool {
	if value == "" {
		return false
	}
	switch value[0] {
	case '=', '+', '@':
		return true
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
func addCell(add func(int, string, string, string), row int, col, value string, required bool) bool {
	if required && value == "" {
		add(row, col, "required", "cell is required")
		return false
	}
	if rejectText(value) {
		add(row, col, "unsafe_text", "cell contains an unsafe formula or control prefix")
		return false
	}
	return true
}
func validDecimal(value string, optional bool) bool {
	if value == "" {
		return optional
	}
	if !decimalPattern.MatchString(value) {
		return false
	}
	raw := strings.TrimPrefix(strings.TrimPrefix(value, "+"), "-")
	p := strings.SplitN(raw, ".", 2)
	frac := ""
	if len(p) == 2 {
		frac = p[1]
	}
	integer := strings.TrimLeft(p[0], "0")
	if integer == "" {
		integer = "0"
	}
	return len(integer) <= 20 && len(frac) <= 18
}
func validUUID(value string) bool { return uuidPattern.MatchString(value) }
func parsePosition(record []string, row int, r *Result, add func(int, string, string, string), seen map[string]struct{}) {
	for i, v := range record {
		if i == 2 || i == 3 || i == 4 || i == 5 {
			continue
		}
		addCell(add, row, positionHeaders[i], v, true)
	}
	if !validUUID(record[0]) {
		add(row, "account_id", "invalid_uuid", "cell must be a UUID")
	}
	if !validUUID(record[1]) {
		add(row, "instrument_id", "invalid_uuid", "cell must be a UUID")
	}
	for _, i := range []int{2, 3, 4, 5} {
		if !validDecimal(record[i], i != 2) {
			add(row, positionHeaders[i], "invalid_decimal", "cell must be an exact decimal with at most 20 integer and 18 fractional digits")
		}
	}
	key := record[0] + "\x00" + record[1]
	if _, ok := seen[key]; ok {
		add(row, "", "duplicate_row", "position key is duplicated")
	} else {
		seen[key] = struct{}{}
	}
	if len(r.Diagnostics) < MaxDiagnostics {
		r.Positions = append(r.Positions, PositionRow{record[0], record[1], record[2], record[3], record[4], record[5]})
	}
}
func parsePrice(record []string, row int, r *Result, add func(int, string, string, string), seen map[string]struct{}) {
	addCell(add, row, "instrument_id", record[0], true)
	addCell(add, row, "quote_currency", record[1], true)
	addCell(add, row, "observation_time", record[2], true)
	addCell(add, row, "price", record[3], true)
	if !validUUID(record[0]) {
		add(row, "instrument_id", "invalid_uuid", "cell must be a UUID")
	}
	if !unitPattern.MatchString(record[1]) {
		add(row, "quote_currency", "invalid_unit", "unit code must be uppercase ASCII letters or digits")
	}
	if !validDecimal(record[3], false) || isNonPositive(record[3]) {
		add(row, "price", "invalid_price", "price must be a positive exact decimal")
	}
	obs, err := parseRFC3339(record[2])
	if err != nil {
		add(row, "observation_time", "invalid_timestamp", "timestamp must be RFC 3339 with an explicit offset")
	}
	var known *time.Time
	if record[4] != "" {
		if addCell(add, row, "source_known_at", record[4], false) {
			t, e := parseRFC3339(record[4])
			if e != nil {
				add(row, "source_known_at", "invalid_timestamp", "timestamp must be RFC 3339 with an explicit offset")
			} else {
				known = &t
			}
		}
	}
	key := strings.Join([]string{record[0], record[1], record[2], record[4]}, "\x00")
	if _, ok := seen[key]; ok {
		add(row, "", "duplicate_row", "price revision key is duplicated")
	} else {
		seen[key] = struct{}{}
	}
	if len(r.Diagnostics) < MaxDiagnostics {
		r.Prices = append(r.Prices, PriceRow{record[0], record[1], obs, record[3], record[4], known})
	}
}
func isNonPositive(value string) bool {
	if strings.HasPrefix(value, "-") {
		return true
	}
	value = strings.TrimPrefix(strings.TrimPrefix(value, "+"), "-")
	value = strings.TrimLeft(value, "0")
	return value == "" || strings.HasPrefix(value, ".") && strings.Trim(strings.TrimPrefix(value, "."), "0") == ""
}
func parseRFC3339(value string) (time.Time, error) {
	if len(value) < 2 || !(strings.HasSuffix(value, "Z") || strings.ContainsAny(value[len(value)-6:], "+-")) {
		return time.Time{}, errors.New("explicit offset required")
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	return t.UTC(), err
}
