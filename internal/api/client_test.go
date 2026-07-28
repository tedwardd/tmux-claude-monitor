package api_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestParseRetryAfterSeconds(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"30", 30 * time.Second},
		{"1", time.Second},
		{"", 0},
		{"garbage", 0},
		{"0", 0},                  // no useful hint; caller falls back to its own backoff
		{"-5", 0},                 // nonsense must not become a negative interval
		{"999999", 1 * time.Hour}, // clamped, so a bad header cannot stall polling
	} {
		if got := api.ParseRetryAfterForTest(tc.header); got != tc.want {
			t.Errorf("Retry-After %q = %v, want %v", tc.header, got, tc.want)
		}
	}
}

// Ticker.Reset panics on a non-positive interval, so this must hold for anything
// the header can contain.
func TestParseRetryAfterNeverNegative(t *testing.T) {
	for _, h := range []string{"-1", "-99999", "0", "", "nonsense", "Thu, 01 Jan 1970 00:00:00 GMT"} {
		if got := api.ParseRetryAfterForTest(h); got < 0 {
			t.Errorf("Retry-After %q produced %v", h, got)
		}
	}
}

func TestRateLimitErrorMentionsTheWait(t *testing.T) {
	withWait := (&api.RateLimitError{RetryAfter: 45 * time.Second}).Error()
	if !strings.Contains(withWait, "429") || !strings.Contains(withWait, "45s") {
		t.Errorf("expected the status and the wait in %q", withWait)
	}
	if bare := (&api.RateLimitError{}).Error(); !strings.Contains(bare, "429") {
		t.Errorf("expected the status in %q", bare)
	}
}

// 401 and 403 have to be distinguishable, since they are the only failures a
// credential reload can fix.
func TestFetchUsageReturnsAuthErrorForRejectedToken(t *testing.T) {
	for _, code := range []int{401, 403} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		_, err := api.FetchUsageFromURL(srv.URL, "stale-token")
		srv.Close()

		var authErr *api.AuthError
		if !errors.As(err, &authErr) {
			t.Errorf("HTTP %d gave %v, want an *api.AuthError", code, err)
			continue
		}
		if authErr.StatusCode != code {
			t.Errorf("AuthError.StatusCode = %d, want %d", authErr.StatusCode, code)
		}
		if !strings.Contains(err.Error(), fmt.Sprint(code)) {
			t.Errorf("error text %q should name the status", err)
		}
	}
}

// Other statuses must not be mistaken for auth failures, or a reload would run
// for something a reload cannot fix.
func TestFetchUsageDoesNotTreatOtherStatusesAsAuth(t *testing.T) {
	for _, code := range []int{400, 404, 500, 503} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
		}))
		_, err := api.FetchUsageFromURL(srv.URL, "tok")
		srv.Close()

		var authErr *api.AuthError
		if errors.As(err, &authErr) {
			t.Errorf("HTTP %d was classified as an auth failure", code)
		}
		if err == nil {
			t.Errorf("HTTP %d returned no error", code)
		}
	}
}
