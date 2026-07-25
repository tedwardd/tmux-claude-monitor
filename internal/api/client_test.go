package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"claude-monitor/internal/api"
)

func TestReadCredentials(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, ".credentials.json")

	creds := map[string]interface{}{
		"claudeAiOauth": map[string]interface{}{
			"accessToken": "sk-test-token-abc123",
		},
	}
	data, _ := json.Marshal(creds)
	os.WriteFile(credPath, data, 0600)

	got, err := api.ReadCredentials(credPath)
	if err != nil {
		t.Fatalf("ReadCredentials: %v", err)
	}
	if got.AccessToken != "sk-test-token-abc123" {
		t.Errorf("AccessToken: got %q", got.AccessToken)
	}
}

func TestReadCredentialsMissingFile(t *testing.T) {
	_, err := api.ReadCredentials("/tmp/no-such-file-xyz.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestFetchUsage(t *testing.T) {
	sessionReset := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	weeklyReset := time.Now().Add(7 * 24 * time.Hour).UTC().Truncate(time.Second)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
			http.Error(w, "missing beta header", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"five_hour": map[string]interface{}{
				"utilization": 84.0,
				"resets_at":   sessionReset.Format(time.RFC3339),
			},
			"seven_day": map[string]interface{}{
				"utilization": 15.0,
				"resets_at":   weeklyReset.Format(time.RFC3339),
			},
		})
	}))
	defer srv.Close()

	usage, err := api.FetchUsageFromURL(srv.URL, "test-token")
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
	if usage.SessionUtilization != 84.0 {
		t.Errorf("SessionUtilization: got %f, want 84.0", usage.SessionUtilization)
	}
	if usage.WeeklyUtilization != 15.0 {
		t.Errorf("WeeklyUtilization: got %f, want 15.0", usage.WeeklyUtilization)
	}
	if !usage.SessionResetsAt.Equal(sessionReset) {
		t.Errorf("SessionResetsAt: got %v, want %v", usage.SessionResetsAt, sessionReset)
	}
	if !usage.WeeklyResetsAt.Equal(weeklyReset) {
		t.Errorf("WeeklyResetsAt: got %v, want %v", usage.WeeklyResetsAt, weeklyReset)
	}
}

func TestFetchUsageHTTP401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", 401)
	}))
	defer srv.Close()

	_, err := api.FetchUsageFromURL(srv.URL, "bad-token")
	if err == nil {
		t.Error("expected error for HTTP 401")
	}
}
