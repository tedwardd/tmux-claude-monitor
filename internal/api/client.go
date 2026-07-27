package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const usageURL = "https://api.anthropic.com/api/oauth/usage"

var httpClient = &http.Client{Timeout: 30 * time.Second}

type Credentials struct {
	AccessToken string
}

// UsageData holds the session (5-hour) quota from the Anthropic OAuth usage API.
// Utilization is a percentage 0–100 of quota consumed.
type UsageData struct {
	SessionUtilization float64
	SessionResetsAt    time.Time
	WeeklyUtilization  float64
	WeeklyResetsAt     time.Time

	ExtraUsageEnabled   bool
	ExtraUsedDollars    float64
	ExtraLimitDollars   float64
	ExtraUtilization    float64
}

type credentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

func ReadCredentials(path string) (Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	return parseCredentials(data)
}

func parseCredentials(data []byte) (Credentials, error) {
	var cf credentialsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return Credentials{}, fmt.Errorf("parse credentials: %w", err)
	}
	if cf.ClaudeAiOauth.AccessToken == "" {
		return Credentials{}, fmt.Errorf("accessToken not found in claudeAiOauth")
	}
	return Credentials{AccessToken: cf.ClaudeAiOauth.AccessToken}, nil
}

func FetchUsage(token string) (UsageData, error) {
	return FetchUsageFromURL(usageURL, token)
}

func FetchUsageFromURL(url, token string) (UsageData, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return UsageData{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-monitor/1.0")

	resp, err := httpClient.Do(req)
	if err != nil {
		return UsageData{}, fmt.Errorf("usage request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return UsageData{}, fmt.Errorf("usage rate-limited (HTTP 429)")
	}
	if resp.StatusCode != 200 {
		return UsageData{}, fmt.Errorf("usage returned HTTP %d", resp.StatusCode)
	}

	var body struct {
		FiveHour *struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		} `json:"five_hour"`
		SevenDay *struct {
			Utilization float64 `json:"utilization"`
			ResetsAt    string  `json:"resets_at"`
		} `json:"seven_day"`
		ExtraUsage *struct {
			IsEnabled    bool    `json:"is_enabled"`
			MonthlyLimit float64 `json:"monthly_limit"`
			UsedCredits  float64 `json:"used_credits"`
			Utilization  float64 `json:"utilization"`
			DecimalPlaces int    `json:"decimal_places"`
		} `json:"extra_usage"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return UsageData{}, fmt.Errorf("parse usage: %w", err)
	}

	var d UsageData
	if body.FiveHour != nil {
		d.SessionUtilization = body.FiveHour.Utilization
		d.SessionResetsAt, _ = time.Parse(time.RFC3339Nano, body.FiveHour.ResetsAt)
		if d.SessionResetsAt.IsZero() {
			d.SessionResetsAt, _ = time.Parse(time.RFC3339, body.FiveHour.ResetsAt)
		}
	}
	if body.SevenDay != nil {
		d.WeeklyUtilization = body.SevenDay.Utilization
		d.WeeklyResetsAt, _ = time.Parse(time.RFC3339Nano, body.SevenDay.ResetsAt)
		if d.WeeklyResetsAt.IsZero() {
			d.WeeklyResetsAt, _ = time.Parse(time.RFC3339, body.SevenDay.ResetsAt)
		}
	}
	if body.ExtraUsage != nil {
		d.ExtraUsageEnabled = body.ExtraUsage.IsEnabled
		d.ExtraUtilization = body.ExtraUsage.Utilization
		scale := 100.0
		if body.ExtraUsage.DecimalPlaces == 0 {
			scale = 1.0
		}
		d.ExtraUsedDollars = body.ExtraUsage.UsedCredits / scale
		d.ExtraLimitDollars = body.ExtraUsage.MonthlyLimit / scale
	}
	return d, nil
}
