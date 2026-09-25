package reconciliation

import (
	"math/big"
	"testing"
)

func TestReconciliationDefaultToleranceAndZeroExternalNAV(t *testing.T) {
	zero := new(big.Rat)
	if got := formatDecimal(defaultTolerance(zero)); got != "0.010000000000000000" {
		t.Fatalf("zero default tolerance=%s", got)
	}
	large, _ := parseDecimal("1000")
	if got := formatDecimal(defaultTolerance(large)); got != "0.100000000000000000" {
		t.Fatalf("large default tolerance=%s", got)
	}
}

func TestReconciliationExactArithmeticAndInclusiveTolerance(t *testing.T) {
	external, _ := parseDecimal("100.000000000000000001")
	valuation, _ := parseDecimal("100")
	difference := new(big.Rat).Abs(new(big.Rat).Sub(external, valuation))
	tolerance, _ := parseDecimal("0.000000000000000001")
	if difference.Cmp(tolerance) != 0 || difference.Cmp(tolerance) > 0 {
		t.Fatalf("difference=%s tolerance=%s", formatDecimal(difference), formatDecimal(tolerance))
	}
	if formatDecimal(new(big.Rat).Quo(difference, external)) != "0.000000000000000000" {
		t.Fatalf("relative difference was not persisted at NUMERIC(38,18) scale")
	}
}

func TestReconciliationDecimalAndUUIDValidation(t *testing.T) {
	if !validDecimal("-0.125000000000000000") || validDecimal("01") || validDecimal("NaN") {
		t.Fatal("decimal validation accepted an invalid representation")
	}
	if !validUUID("00000000-0000-0000-0000-000000000001") || validUUID("not-a-uuid") {
		t.Fatal("UUID validation mismatch")
	}
}
