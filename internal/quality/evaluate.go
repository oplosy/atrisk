// Package quality classifies persisted series snapshots without using wall-clock state.
package quality

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Classification string

const (
	Fresh   Classification = "fresh"
	Stale   Classification = "stale"
	Missing Classification = "missing"
	Partial Classification = "partial"
	Suspect Classification = "suspect"
	Revised Classification = "revised"
)

type State string

const (
	Valid    State = "valid"
	Degraded State = "degraded"
	Blocked  State = "blocked"
)

type Policy struct {
	Version         string   `json:"version"`
	Expected        string   `json:"expected"`
	MaxAge          string   `json:"max_age"`
	AvailabilityLag string   `json:"availability_lag,omitempty"`
	HolidayDates    []string `json:"holiday_dates,omitempty"`
}

type Sample struct {
	ObservationTime time.Time
	QualityFlags    json.RawMessage
	Revised         bool
}

type Reason struct {
	Code          string   `json:"code"`
	Message       string   `json:"message"`
	AffectedDates []string `json:"affected_dates,omitempty"`
}

type Result struct {
	Classification Classification `json:"classification"`
	State          State          `json:"state"`
	PolicyVersion  string         `json:"policy_version,omitempty"`
	EvaluatedAt    time.Time      `json:"evaluated_at"`
	Reasons        []Reason       `json:"reasons"`
}

var ErrInvalidPolicy = errors.New("invalid freshness policy")

// Evaluate applies a series policy to one immutable system-as-of snapshot.
func Evaluate(frequency, sourceTimezone string, rawPolicy json.RawMessage, from, to, cutoff time.Time, samples []Sample, required bool) Result {
	result := Result{Classification: Suspect, State: Blocked, EvaluatedAt: cutoff.UTC(), Reasons: []Reason{}}
	policy, maxAge, lag, holidays, location, err := parsePolicy(frequency, sourceTimezone, rawPolicy)
	if err != nil {
		result.Reasons = append(result.Reasons, Reason{Code: "QUALITY_POLICY_INVALID", Message: err.Error()})
		return result
	}
	result.PolicyVersion = policy.Version

	byDate := make(map[string]Sample, len(samples))
	var latest time.Time
	var hasSuspect, hasRevision, hasExplicitMissing bool
	suspectDates := []string{}
	revisionDates := []string{}
	explicitMissingDates := []string{}
	for _, sample := range samples {
		at := sample.ObservationTime.UTC()
		key := at.In(location).Format("2006-01-02")
		if sample.Revised {
			hasRevision = true
			revisionDates = append(revisionDates, key)
		}
		if flagsContainSuspect(sample.QualityFlags) {
			hasSuspect = true
			suspectDates = append(suspectDates, key)
		}
		if flagsContainMissing(sample.QualityFlags) {
			hasExplicitMissing = true
			explicitMissingDates = append(explicitMissingDates, key)
			continue
		}
		if prior, ok := byDate[key]; !ok || at.After(prior.ObservationTime) {
			byDate[key] = sample
		}
		if at.After(latest) {
			latest = at
		}
	}

	if hasSuspect {
		result.Reasons = append(result.Reasons, Reason{Code: "SOURCE_QUALITY_SUSPECT", Message: "one or more persisted revisions carry a suspect quality flag", AffectedDates: uniqueDates(suspectDates)})
	}
	if hasExplicitMissing {
		result.Reasons = append(result.Reasons, Reason{Code: "SOURCE_MARKED_MISSING", Message: "the source explicitly marked an expected value as missing", AffectedDates: uniqueDates(explicitMissingDates)})
	}
	if !latest.IsZero() && cutoff.UTC().Sub(latest) > maxAge {
		result.Reasons = append(result.Reasons, Reason{Code: "LATEST_OBSERVATION_STALE", Message: fmt.Sprintf("latest observation is older than policy max_age %s", maxAge), AffectedDates: []string{latest.In(location).Format("2006-01-02")}})
	}
	if hasRevision {
		result.Reasons = append(result.Reasons, Reason{Code: "OBSERVATION_REVISED", Message: "one or more observation dates have differing immutable revisions", AffectedDates: uniqueDates(revisionDates)})
	}

	missingDates := expectedMissing(policy.Expected, from.In(location), to.In(location), cutoff.In(location), lag, holidays, byDate, location)
	if len(missingDates) > 0 {
		code, message := "EXPECTED_PERIOD_GAP", "one or more expected periods have no eligible observation"
		if len(byDate) == 0 {
			code, message = "EXPECTED_DATA_MISSING", "no eligible observation exists for the requested window"
		}
		result.Reasons = append(result.Reasons, Reason{Code: code, Message: message, AffectedDates: missingDates})
	}
	if len(byDate) == 0 && len(missingDates) == 0 && !hasExplicitMissing {
		result.Reasons = append(result.Reasons, Reason{
			Code:          "NO_ELIGIBLE_OBSERVATIONS",
			Message:       "no observation is eligible in the requested system-as-of window",
			AffectedDates: []string{from.In(location).Format("2006-01-02"), to.Add(-time.Nanosecond).In(location).Format("2006-01-02")},
		})
	}

	switch {
	case hasSuspect:
		result.Classification = Suspect
	case len(byDate) == 0:
		result.Classification = Missing
	case cutoff.UTC().Sub(latest) > maxAge:
		result.Classification = Stale
	case len(missingDates) > 0 || hasExplicitMissing:
		result.Classification = Partial
	case hasRevision:
		result.Classification = Revised
	default:
		result.Classification = Fresh
	}

	switch result.Classification {
	case Missing, Stale, Partial, Suspect:
		if required {
			result.State = Blocked
		} else {
			result.State = Degraded
		}
	default:
		result.State = Valid
	}
	return result
}

func parsePolicy(frequency, sourceTimezone string, raw json.RawMessage) (Policy, time.Duration, time.Duration, map[string]struct{}, *time.Location, error) {
	var p Policy
	location := time.UTC
	if strings.TrimSpace(sourceTimezone) != "" {
		var err error
		location, err = time.LoadLocation(sourceTimezone)
		if err != nil {
			return p, 0, 0, nil, nil, fmt.Errorf("%w: invalid source timezone %q", ErrInvalidPolicy, sourceTimezone)
		}
	}
	if len(raw) == 0 || !json.Valid(raw) {
		return p, 0, 0, nil, nil, fmt.Errorf("%w: missing or malformed JSON", ErrInvalidPolicy)
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, 0, 0, nil, nil, fmt.Errorf("%w: %v", ErrInvalidPolicy, err)
	}
	p.Version = strings.TrimSpace(p.Version)
	if p.Version == "" {
		return p, 0, 0, nil, nil, fmt.Errorf("%w: version is required", ErrInvalidPolicy)
	}
	if p.Expected == "" {
		p.Expected = defaultExpected(frequency)
	}
	if !validExpected(p.Expected) {
		return p, 0, 0, nil, nil, fmt.Errorf("%w: unsupported expected schedule %q", ErrInvalidPolicy, p.Expected)
	}
	maxAge, err := parseDuration(p.MaxAge)
	if err != nil || maxAge <= 0 {
		return p, 0, 0, nil, nil, fmt.Errorf("%w: max_age must be a positive duration", ErrInvalidPolicy)
	}
	lag := time.Duration(0)
	if p.AvailabilityLag != "" {
		lag, err = parseDuration(p.AvailabilityLag)
		if err != nil || lag < 0 {
			return p, 0, 0, nil, nil, fmt.Errorf("%w: availability_lag must be a non-negative duration", ErrInvalidPolicy)
		}
	}
	holidaySet := make(map[string]struct{}, len(p.HolidayDates))
	for _, date := range p.HolidayDates {
		if _, err := time.Parse("2006-01-02", date); err != nil {
			return p, 0, 0, nil, nil, fmt.Errorf("%w: holiday_dates entry %q must be YYYY-MM-DD", ErrInvalidPolicy, date)
		}
		holidaySet[date] = struct{}{}
	}
	return p, maxAge, lag, holidaySet, location, nil
}

func parseDuration(value string) (time.Duration, error) {
	if strings.HasPrefix(value, "P") && strings.HasSuffix(value, "D") {
		duration, err := time.ParseDuration(strings.TrimSuffix(strings.TrimPrefix(value, "P"), "D") + "h")
		if err != nil {
			return 0, err
		}
		return duration * 24, nil
	}
	return time.ParseDuration(value)
}

func defaultExpected(frequency string) string {
	switch strings.ToLower(strings.TrimSpace(frequency)) {
	case "daily", "day":
		return "calendar_daily"
	case "business_daily":
		return "business_daily"
	case "weekly", "week":
		return "weekly"
	case "monthly", "month":
		return "monthly"
	case "quarterly", "quarter":
		return "quarterly"
	case "irregular":
		return "irregular"
	default:
		return ""
	}
}

func validExpected(value string) bool {
	switch value {
	case "calendar_daily", "business_daily", "weekly", "monthly", "quarterly", "irregular":
		return true
	default:
		return false
	}
}

func expectedMissing(expected string, from, to, cutoff time.Time, lag time.Duration, holidays map[string]struct{}, samples map[string]Sample, location *time.Location) []string {
	if expected == "irregular" {
		return nil
	}
	start := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, location)
	if from.After(start) {
		start = start.AddDate(0, 0, 1)
	}
	end := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, location)
	if to.Equal(end) {
		end = end.AddDate(0, 0, -1)
	}
	cutoff = cutoff.In(location)
	missing := []string{}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		if !isExpectedDate(expected, day, holidays) || day.Add(lag).After(cutoff) {
			continue
		}
		key := day.Format("2006-01-02")
		if _, ok := samples[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

func isExpectedDate(expected string, day time.Time, holidays map[string]struct{}) bool {
	date := day.Format("2006-01-02")
	if _, holiday := holidays[date]; holiday {
		return false
	}
	switch expected {
	case "calendar_daily":
		return true
	case "business_daily":
		return day.Weekday() != time.Saturday && day.Weekday() != time.Sunday
	case "weekly":
		return day.Weekday() == time.Friday
	case "monthly":
		return day.Day() == 1
	case "quarterly":
		return day.Day() == 1 && (day.Month() == time.January || day.Month() == time.April || day.Month() == time.July || day.Month() == time.October)
	default:
		return false
	}
}

func flagsContainSuspect(raw json.RawMessage) bool {
	if !json.Valid(raw) {
		return true
	}
	var flags map[string]any
	if err := json.Unmarshal(raw, &flags); err != nil {
		return true
	}
	if flags["suspect"] == true || flags["invalid"] == true {
		return true
	}
	if quality, ok := flags["quality"].(string); ok && quality == "suspect" {
		return true
	}
	return false
}

func flagsContainMissing(raw json.RawMessage) bool {
	var flags map[string]any
	if err := json.Unmarshal(raw, &flags); err != nil {
		return false
	}
	return flags["missing"] == true || flags["missing_expected_period"] == true
}

func uniqueDates(dates []string) []string {
	sort.Strings(dates)
	unique := dates[:0]
	for _, date := range dates {
		if len(unique) == 0 || unique[len(unique)-1] != date {
			unique = append(unique, date)
		}
	}
	return unique
}

// Aggregate combines input states while preserving the most restrictive state.
func Aggregate(states []State) State {
	aggregate := Valid
	for _, state := range states {
		switch state {
		case Blocked:
			return Blocked
		case Degraded:
			aggregate = Degraded
		case Valid:
		default:
			return Blocked
		}
	}
	return aggregate
}
