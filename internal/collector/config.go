// Package collector contains durable schedule claiming and provider dispatch.
package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

type FileConfig struct {
	DatabaseURLEnv string          `json:"database_url_env"`
	Archive        ArchiveConfig   `json:"archive"`
	Schedules      []ScheduleInput `json:"schedules"`
}

type ArchiveConfig struct {
	Endpoint     string `json:"endpoint"`
	Region       string `json:"region"`
	Bucket       string `json:"bucket"`
	AccessKeyEnv string `json:"access_key_env"`
	SecretKeyEnv string `json:"secret_key_env"`
}

type ScheduleInput struct {
	Name            string          `json:"name"`
	Provider        string          `json:"provider"`
	SourceID        string          `json:"source_id"`
	DatasetID       string          `json:"dataset_id,omitempty"`
	SeriesID        string          `json:"series_id,omitempty"`
	InstrumentID    string          `json:"instrument_id,omitempty"`
	CredentialEnv   string          `json:"credential_env,omitempty"`
	BaseURL         string          `json:"base_url,omitempty"`
	Request         json.RawMessage `json:"request"`
	Interval        time.Duration   `json:"-"`
	IntervalSeconds int             `json:"interval_seconds"`
	MaxAttempts     int             `json:"max_attempts"`
	LeaseSeconds    int             `json:"lease_seconds"`
	NextRunAt       *time.Time      `json:"next_run_at,omitempty"`
}

type Schedule struct {
	ScheduleInput
	RequestFingerprint string
	Configuration      []byte
}

func LoadConfig(path string) (FileConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FileConfig{}, fmt.Errorf("read collector config: %w", err)
	}
	var config FileConfig
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return FileConfig{}, fmt.Errorf("decode collector config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return FileConfig{}, err
	}
	return config, nil
}

func (c FileConfig) Validate() error {
	if !envNamePattern.MatchString(strings.TrimSpace(c.DatabaseURLEnv)) {
		return errors.New("database_url_env must be a valid environment variable name")
	}
	if err := c.Archive.Validate(); err != nil {
		return fmt.Errorf("archive configuration: %w", err)
	}
	if len(c.Schedules) == 0 {
		return errors.New("at least one collector schedule is required")
	}
	seen := make(map[string]struct{}, len(c.Schedules))
	for i := range c.Schedules {
		if _, exists := seen[c.Schedules[i].Name]; exists {
			return fmt.Errorf("schedule %q is duplicated", c.Schedules[i].Name)
		}
		seen[c.Schedules[i].Name] = struct{}{}
		if _, err := NormalizeSchedule(c.Schedules[i]); err != nil {
			return fmt.Errorf("schedule %d: %w", i, err)
		}
	}
	return nil
}

func (c ArchiveConfig) Validate() error {
	u, err := url.Parse(strings.TrimSpace(c.Endpoint))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("endpoint must be an HTTP(S) URL without credentials or query parameters")
	}
	if strings.TrimSpace(c.Region) == "" || strings.TrimSpace(c.Bucket) == "" {
		return errors.New("endpoint, region, and bucket are required")
	}
	if !envNamePattern.MatchString(strings.TrimSpace(c.AccessKeyEnv)) || !envNamePattern.MatchString(strings.TrimSpace(c.SecretKeyEnv)) {
		return errors.New("access_key_env and secret_key_env must be environment variable names")
	}
	return nil
}

func NormalizeSchedule(input ScheduleInput) (Schedule, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Provider = strings.ToLower(strings.TrimSpace(input.Provider))
	if input.Name == "" || strings.ContainsAny(input.Name, "\r\n") {
		return Schedule{}, errors.New("schedule name is required")
	}
	if input.Provider != "fred" && input.Provider != "tcmb" && input.Provider != "binance" {
		return Schedule{}, fmt.Errorf("unsupported provider %q", input.Provider)
	}
	if !uuidPattern.MatchString(input.SourceID) {
		return Schedule{}, errors.New("source_id must be a UUID")
	}
	if input.Provider != "binance" && !envNamePattern.MatchString(strings.TrimSpace(input.CredentialEnv)) {
		return Schedule{}, errors.New("credential_env must name an environment variable")
	}
	if input.BaseURL != "" {
		base, err := url.Parse(input.BaseURL)
		if err != nil || base.Scheme != "https" || base.Hostname() == "" || base.User != nil || base.RawQuery != "" {
			return Schedule{}, errors.New("base_url must be HTTPS without credentials or query parameters")
		}
	}
	for field, value := range map[string]string{"dataset_id": input.DatasetID, "series_id": input.SeriesID, "instrument_id": input.InstrumentID} {
		if value != "" && !uuidPattern.MatchString(value) {
			return Schedule{}, fmt.Errorf("%s must be a UUID", field)
		}
	}
	if (input.Provider == "fred" || input.Provider == "tcmb") && input.SeriesID == "" {
		return Schedule{}, fmt.Errorf("%s schedules require series_id", input.Provider)
	}
	if input.Provider == "binance" && input.InstrumentID == "" {
		return Schedule{}, errors.New("binance schedules require instrument_id")
	}
	if len(input.Request) == 0 || string(input.Request) == "null" || string(input.Request) == "{}" {
		return Schedule{}, errors.New("request is required")
	}
	var request map[string]json.RawMessage
	if err := json.Unmarshal(input.Request, &request); err != nil || request == nil {
		return Schedule{}, errors.New("request must be a JSON object")
	}
	if err := validateRequestFields(input.Provider, request); err != nil {
		return Schedule{}, err
	}
	if input.IntervalSeconds <= 0 || input.IntervalSeconds > 31*24*60*60 {
		return Schedule{}, errors.New("interval_seconds must be between 1 and 2678400")
	}
	if input.MaxAttempts <= 0 || input.MaxAttempts > 20 {
		return Schedule{}, errors.New("max_attempts must be between 1 and 20")
	}
	if input.LeaseSeconds <= 0 || input.LeaseSeconds > 24*60*60 {
		return Schedule{}, errors.New("lease_seconds must be between 1 and 86400")
	}
	requestPayload := struct {
		Provider      string                     `json:"provider"`
		SourceID      string                     `json:"source_id"`
		DatasetID     string                     `json:"dataset_id,omitempty"`
		SeriesID      string                     `json:"series_id,omitempty"`
		InstrumentID  string                     `json:"instrument_id,omitempty"`
		Request       map[string]json.RawMessage `json:"request"`
		CredentialEnv string                     `json:"credential_env,omitempty"`
		BaseURL       string                     `json:"base_url,omitempty"`
		Interval      int                        `json:"interval_seconds"`
		MaxAttempts   int                        `json:"max_attempts"`
		LeaseSeconds  int                        `json:"lease_seconds"`
	}{Provider: input.Provider, SourceID: input.SourceID, DatasetID: input.DatasetID, SeriesID: input.SeriesID, InstrumentID: input.InstrumentID, Request: request, CredentialEnv: input.CredentialEnv, BaseURL: input.BaseURL, Interval: input.IntervalSeconds, MaxAttempts: input.MaxAttempts, LeaseSeconds: input.LeaseSeconds}
	normalizedRequest, err := json.Marshal(requestPayload)
	if err != nil {
		return Schedule{}, err
	}
	digest := sha256.Sum256(normalizedRequest)
	return Schedule{ScheduleInput: input, RequestFingerprint: hex.EncodeToString(digest[:]), Configuration: normalizedRequest}, nil
}

func validateRequestFields(provider string, request map[string]json.RawMessage) error {
	allowed := map[string]struct{}{}
	switch provider {
	case "fred":
		for _, key := range []string{"series_id", "realtime_start", "realtime_end", "vintage_dates", "observation_start", "observation_end", "units", "frequency", "aggregation_method", "output_type", "limit", "offset", "max_pages"} {
			allowed[key] = struct{}{}
		}
	case "tcmb":
		for _, key := range []string{"series", "startdate", "enddate", "frequency", "aggregationtypes", "formulas", "decimalseparator", "max_pages"} {
			allowed[key] = struct{}{}
		}
	case "binance":
		for _, key := range []string{"symbol", "symbols", "start_time", "end_time", "limit", "max_pages"} {
			allowed[key] = struct{}{}
		}
	}
	for key := range request {
		lower := strings.ToLower(strings.TrimSpace(key))
		if strings.Contains(lower, "api") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || strings.Contains(lower, "credential") || strings.Contains(lower, "key") {
			return fmt.Errorf("request field %q cannot contain credentials; use credential_env", key)
		}
		if _, ok := allowed[lower]; !ok {
			return fmt.Errorf("unsupported %s request field %q", provider, key)
		}
	}
	return nil
}
