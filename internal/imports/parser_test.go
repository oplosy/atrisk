package imports

import "testing"

const validPositions = "account_id,instrument_id,quantity,total_cost_basis,modified_duration_years,convexity_years_squared\n11111111-1111-4111-8111-111111111111,22222222-2222-4222-8222-222222222222,1.25,,4.2,0\n"

func TestParsePositionsStrictAndBOM(t *testing.T) {
	r := Parse(append([]byte{0xef, 0xbb, 0xbf}, []byte(validPositions)...), KindPositions)
	if !r.Valid || r.RowCount != 1 || len(r.Positions) != 1 {
		t.Fatalf("result=%+v", r)
	}
}

func TestParseRejectsUnsafeAndDuplicateRows(t *testing.T) {
	body := "account_id,instrument_id,quantity,total_cost_basis,modified_duration_years,convexity_years_squared\n11111111-1111-4111-8111-111111111111,22222222-2222-4222-8222-222222222222,=1,,,\n11111111-1111-4111-8111-111111111111,22222222-2222-4222-8222-222222222222,1,,,\n"
	r := Parse([]byte(body), KindPositions)
	if r.Valid || len(r.Diagnostics) < 2 {
		t.Fatalf("result=%+v", r)
	}
}

func TestParseManualPriceExactAndTimestamp(t *testing.T) {
	body := "instrument_id,quote_currency,observation_time,price,source_known_at\n22222222-2222-4222-8222-222222222222,USDT,2026-01-02T03:04:05+02:00,1.000000000000000001,2026-01-02T03:05:05Z\n"
	r := Parse([]byte(body), KindManualPrices)
	if !r.Valid || len(r.Prices) != 1 || r.Prices[0].Price != "1.000000000000000001" {
		t.Fatalf("result=%+v", r)
	}
}
