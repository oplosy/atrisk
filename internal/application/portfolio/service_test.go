package portfolio

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestParseDecimalPreservesEighteenFractionalDigits(t *testing.T) {
	value, err := parseDecimal("123.456789012345678901", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := numericString(value); got != "123.456789012345678901" {
		t.Fatalf("got %q", got)
	}
	if _, err := parseDecimal("123.4567890123456789012", false); err == nil {
		t.Fatal("accepted more than 18 fractional digits")
	}
	if _, err := parseDecimal("not-a-decimal", false); err == nil {
		t.Fatal("accepted malformed decimal")
	}
	if value, err := parseDecimal("", true); err != nil || value.Valid {
		t.Fatalf("optional decimal: value=%+v err=%v", value, err)
	}
}

func TestPortfolioMetadataAndDatabaseErrorMapping(t *testing.T) {
	if got := decodeMetadata([]byte(`{"name":"manual"}`))["name"]; got != "manual" {
		t.Fatalf("metadata=%v", got)
	}
	if got := decodeMetadata([]byte(`null`)); len(got) != 0 {
		t.Fatalf("null metadata=%v", got)
	}
	if got := mapDatabaseError(databaseError("23505")); got != ErrConflict {
		t.Fatalf("mapped error=%v", got)
	}
}

// databaseError gives the mapping test a pgconn error without requiring a live
// PostgreSQL connection.
func databaseError(code string) error {
	return &pgconn.PgError{Code: code}
}
