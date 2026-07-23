package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	bootstrapURL = "https://claude.ai/api/bootstrap"
	usageDomain  = "https://claude.ai"
)

// Field path constants — documenting the discovered API response structure.
// If the live API changes, run `init --discover` to inspect the raw response.
const (
	usageFieldLimit  = "raw_limits.message_limit"
	usageFieldUsed   = "raw_limits.messages_remaining" // computed: limit - remaining
	usageFieldReset  = "raw_limits.window_resets_at"
)

type Credentials struct {
	AccessToken string
}

type UsageData struct {
	MessagesUsed  int
	MessagesLimit int
	ResetAt       time.Time
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
	var cf credentialsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return Credentials{}, fmt.Errorf("parse credentials: %w", err)
	}
	if cf.ClaudeAiOauth.AccessToken == "" {
		return Credentials{}, fmt.Errorf("accessToken not found in claudeAiOauth")
	}
	return Credentials{AccessToken: cf.ClaudeAiOauth.AccessToken}, nil
}

func FetchBootstrap(token string) (string, error) {
	return FetchBootstrapFromURL(bootstrapURL, token)
}

func FetchBootstrapFromURL(url, token string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("bootstrap request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("bootstrap returned HTTP %d", resp.StatusCode)
	}

	// Response shape: {"account":{"organizationMemberships":[{"organization":{"uuid":"..."}}]}}
	// Adjust field paths here if the real API differs — run init --discover to inspect raw response.
	var body struct {
		Account struct {
			OrganizationMemberships []struct {
				Organization struct {
					UUID string `json:"uuid"`
				} `json:"organization"`
			} `json:"organizationMemberships"`
		} `json:"account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("parse bootstrap: %w", err)
	}
	if len(body.Account.OrganizationMemberships) == 0 {
		return "", fmt.Errorf("no organizations found in bootstrap response")
	}
	return body.Account.OrganizationMemberships[0].Organization.UUID, nil
}

func FetchUsage(token, orgUUID string) (UsageData, error) {
	return FetchUsageFromURL(usageDomain, orgUUID, token)
}

func FetchUsageFromURL(baseURL, orgUUID, token string) (UsageData, error) {
	url := fmt.Sprintf("%s/api/organizations/%s/usage", baseURL, orgUUID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return UsageData{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return UsageData{}, fmt.Errorf("usage request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return UsageData{}, fmt.Errorf("usage returned HTTP %d", resp.StatusCode)
	}

	// Response shape (adjust if real API differs — run init --discover):
	// {"raw_limits":{"message_limit":50,"messages_remaining":20,"window_resets_at":"2026-07-23T16:30:00Z"}}
	var body struct {
		RawLimits struct {
			MessageLimit      int    `json:"message_limit"`
			MessagesRemaining int    `json:"messages_remaining"`
			WindowResetsAt    string `json:"window_resets_at"`
		} `json:"raw_limits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return UsageData{}, fmt.Errorf("parse usage: %w", err)
	}

	resetAt, err := time.Parse(time.RFC3339, body.RawLimits.WindowResetsAt)
	if err != nil {
		resetAt = time.Time{}
	}

	used := body.RawLimits.MessageLimit - body.RawLimits.MessagesRemaining
	if used < 0 {
		used = 0
	}

	return UsageData{
		MessagesUsed:  used,
		MessagesLimit: body.RawLimits.MessageLimit,
		ResetAt:       resetAt,
	}, nil
}
