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

func TestFetchBootstrap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/bootstrap" {
			http.Error(w, "not found", 404)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"account": map[string]interface{}{
				"organizationMemberships": []interface{}{
					map[string]interface{}{"organization": map[string]interface{}{"uuid": "org-uuid-abc"}},
				},
			},
		})
	}))
	defer srv.Close()

	orgUUID, err := api.FetchBootstrapFromURL(srv.URL+"/api/bootstrap", "test-token")
	if err != nil {
		t.Fatalf("FetchBootstrap: %v", err)
	}
	if orgUUID != "org-uuid-abc" {
		t.Errorf("orgUUID: got %q, want %q", orgUUID, "org-uuid-abc")
	}
}

func TestFetchUsage(t *testing.T) {
	resetTime := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"raw_limits": map[string]interface{}{
				"message_limit":      50,
				"messages_remaining": 20,
				"window_resets_at":   resetTime.Format(time.RFC3339),
			},
		})
	}))
	defer srv.Close()

	usage, err := api.FetchUsageFromURL(srv.URL, "org-uuid-abc", "test-token")
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
	if usage.MessagesLimit != 50 {
		t.Errorf("MessagesLimit: got %d", usage.MessagesLimit)
	}
	if usage.MessagesUsed != 30 { // limit - remaining = 50 - 20
		t.Errorf("MessagesUsed: got %d", usage.MessagesUsed)
	}
	if !usage.ResetAt.Equal(resetTime) {
		t.Errorf("ResetAt: got %v, want %v", usage.ResetAt, resetTime)
	}
}
