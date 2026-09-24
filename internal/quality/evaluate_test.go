package quality

import (
	"encoding/json"
	"testing"
	"time"
)

const businessPolicy = `{"version":"1","expected":"business_daily","max_age":"72h","availability_lag":"24h","holiday_dates":["2024-07-04"]}`

func instant(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return t
}

func sample(date string, revised bool, flags string) Sample {
	return Sample{ObservationTime: instant(date), Revised: revised, QualityFlags: json.RawMessage(flags)}
}

func TestQualityEvaluateSkipsBusinessHolidayWeekendAndNotYetDueRelease(t *testing.T) {
	got := Evaluate("daily", "", json.RawMessage(businessPolicy), instant("2024-07-03T00:00:00Z"), instant("2024-07-07T00:00:00Z"), instant("2024-07-05T12:00:00Z"), []Sample{
		sample("2024-07-03T00:00:00Z", false, `{}`),
	}, true)
	if got.Classification != Fresh || got.State != Valid {
		t.Fatalf("got %+v, want fresh/valid", got)
	}
	if got.PolicyVersion != "1" || !got.EvaluatedAt.Equal(instant("2024-07-05T12:00:00Z")) {
		t.Fatalf("policy version/cutoff not recorded: %+v", got)
	}
}

func TestQualityEvaluateMissingExpectedBusinessDayIsPartialAndBlocksRequired(t *testing.T) {
	got := Evaluate("daily", "", json.RawMessage(businessPolicy), instant("2024-07-03T00:00:00Z"), instant("2024-07-06T00:00:00Z"), instant("2024-07-06T00:00:00Z"), []Sample{
		sample("2024-07-03T00:00:00Z", false, `{}`),
	}, true)
	if got.Classification != Partial || got.State != Blocked {
		t.Fatalf("got %+v, want partial/blocked", got)
	}
	if len(got.Reasons) == 0 || got.Reasons[0].Code != "EXPECTED_PERIOD_GAP" {
		t.Fatalf("expected structured gap reason, got %+v", got.Reasons)
	}
	optional := Evaluate("daily", "", json.RawMessage(businessPolicy), instant("2024-07-03T00:00:00Z"), instant("2024-07-06T00:00:00Z"), instant("2024-07-06T00:00:00Z"), []Sample{
		sample("2024-07-03T00:00:00Z", false, `{}`),
	}, false)
	if optional.Classification != Partial || optional.State != Degraded {
		t.Fatalf("got %+v, want optional partial/degraded", optional)
	}
}

func TestQualityEvaluateWeeklyMonthlyAndQuarterlyCalendarSlots(t *testing.T) {
	tests := []struct {
		name      string
		frequency string
		policy    string
		from      string
		to        string
		cutoff    string
		observed  string
	}{
		{name: "weekly Friday", frequency: "weekly", policy: `{"version":"w1","max_age":"240h"}`, from: "2024-01-08T00:00:00Z", to: "2024-01-15T00:00:00Z", cutoff: "2024-01-15T00:00:00Z", observed: "2024-01-12T00:00:00Z"},
		{name: "monthly first day", frequency: "monthly", policy: `{"version":"m1","max_age":"1080h"}`, from: "2024-01-01T00:00:00Z", to: "2024-02-01T00:00:00Z", cutoff: "2024-02-01T00:00:00Z", observed: "2024-01-01T00:00:00Z"},
		{name: "quarterly first day", frequency: "quarterly", policy: `{"version":"q1","max_age":"3000h"}`, from: "2024-01-01T00:00:00Z", to: "2024-04-01T00:00:00Z", cutoff: "2024-04-01T00:00:00Z", observed: "2024-01-01T00:00:00Z"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.frequency, "", json.RawMessage(tc.policy), instant(tc.from), instant(tc.to), instant(tc.cutoff), []Sample{sample(tc.observed, false, `{}`)}, true)
			if got.Classification != Fresh || got.State != Valid {
				t.Fatalf("got %+v, want fresh/valid", got)
			}
		})
	}
}

func TestQualityEvaluateUsesSourceTimezoneForCalendarDates(t *testing.T) {
	policy := json.RawMessage(`{"version":"tz-v1","expected":"business_daily","max_age":"72h","holiday_dates":["2024-07-04"]}`)
	got := Evaluate("daily", "Europe/Istanbul", policy, instant("2024-07-04T21:00:00Z"), instant("2024-07-06T21:00:00Z"), instant("2024-07-07T21:00:00Z"), []Sample{
		sample("2024-07-04T21:00:00Z", false, `{}`),
	}, true)
	if got.Classification != Fresh || got.State != Valid {
		t.Fatalf("source-local Friday should satisfy the business calendar: %+v", got)
	}
}

func TestQualityEvaluateStaleAndMissingOptionalInputsDegrade(t *testing.T) {
	stale := Evaluate("daily", "", json.RawMessage(`{"version":"2","max_age":"24h"}`), instant("2024-01-01T00:00:00Z"), instant("2024-01-03T00:00:00Z"), instant("2024-01-04T00:00:00Z"), []Sample{sample("2024-01-01T00:00:00Z", false, `{}`)}, false)
	if stale.Classification != Stale || stale.State != Degraded {
		t.Fatalf("got %+v, want stale/degraded", stale)
	}
	missing := Evaluate("daily", "", json.RawMessage(`{"version":"2","max_age":"24h"}`), instant("2024-01-01T00:00:00Z"), instant("2024-01-02T00:00:00Z"), instant("2024-01-03T00:00:00Z"), nil, false)
	if missing.Classification != Missing || missing.State != Degraded {
		t.Fatalf("got %+v, want missing/degraded", missing)
	}
	irregularMissing := Evaluate("irregular", "", json.RawMessage(`{"version":"2","expected":"irregular","max_age":"24h"}`), instant("2024-01-01T00:00:00Z"), instant("2024-01-02T00:00:00Z"), instant("2024-01-03T00:00:00Z"), nil, false)
	if irregularMissing.Classification != Missing || len(irregularMissing.Reasons) != 1 || irregularMissing.Reasons[0].Code != "NO_ELIGIBLE_OBSERVATIONS" {
		t.Fatalf("irregular missing result lacks an explicit reason: %+v", irregularMissing)
	}
	requiredStale := Evaluate("daily", "", json.RawMessage(`{"version":"2","max_age":"24h"}`), instant("2024-01-01T00:00:00Z"), instant("2024-01-03T00:00:00Z"), instant("2024-01-04T00:00:00Z"), []Sample{sample("2024-01-01T00:00:00Z", false, `{}`)}, true)
	if requiredStale.Classification != Stale || requiredStale.State != Blocked {
		t.Fatalf("got %+v, want required stale/blocked", requiredStale)
	}
	requiredMissing := Evaluate("daily", "", json.RawMessage(`{"version":"2","max_age":"24h"}`), instant("2024-01-01T00:00:00Z"), instant("2024-01-02T00:00:00Z"), instant("2024-01-03T00:00:00Z"), nil, true)
	if requiredMissing.Classification != Missing || requiredMissing.State != Blocked {
		t.Fatalf("got %+v, want required missing/blocked", requiredMissing)
	}
}

func TestQualityEvaluateSuspectAndRevisedSnapshots(t *testing.T) {
	suspect := Evaluate("irregular", "", json.RawMessage(`{"version":"3","expected":"irregular","max_age":"24h"}`), instant("2024-01-01T00:00:00Z"), instant("2024-01-02T00:00:00Z"), instant("2024-01-02T00:00:00Z"), []Sample{sample("2024-01-01T12:00:00Z", false, `{"suspect":true}`)}, true)
	if suspect.Classification != Suspect || suspect.State != Blocked {
		t.Fatalf("got %+v, want suspect/blocked", suspect)
	}
	revised := Evaluate("irregular", "", json.RawMessage(`{"version":"3","expected":"irregular","max_age":"24h"}`), instant("2024-01-01T00:00:00Z"), instant("2024-01-02T00:00:00Z"), instant("2024-01-02T00:00:00Z"), []Sample{sample("2024-01-01T12:00:00Z", true, `{}`)}, true)
	if revised.Classification != Revised || revised.State != Valid {
		t.Fatalf("got %+v, want revised/valid", revised)
	}
}

func TestQualityEvaluateInvalidPolicyFailsClosedAndAggregateCannotHideBlocked(t *testing.T) {
	missingVersion := Evaluate("daily", "", json.RawMessage(`{"max_age":"24h"}`), instant("2024-01-01T00:00:00Z"), instant("2024-01-02T00:00:00Z"), instant("2024-01-02T00:00:00Z"), []Sample{sample("2024-01-01T00:00:00Z", false, `{}`)}, false)
	if missingVersion.Classification != Suspect || missingVersion.State != Blocked || missingVersion.Reasons[0].Code != "QUALITY_POLICY_INVALID" {
		t.Fatalf("invalid policy was not fail-closed: %+v", missingVersion)
	}
	if got := Aggregate([]State{Valid, Degraded, Blocked}); got != Blocked {
		t.Fatalf("aggregate=%s want blocked", got)
	}
	if got := Aggregate([]State{Valid, Degraded}); got != Degraded {
		t.Fatalf("aggregate=%s want degraded", got)
	}
}
