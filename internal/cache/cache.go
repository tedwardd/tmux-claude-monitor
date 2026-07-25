package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const staleThreshold = 15 * time.Minute

type Entry struct {
	FetchedAt          time.Time `json:"fetched_at"`
	SessionUtilization float64   `json:"session_utilization"`
	SessionResetsAt    time.Time `json:"session_resets_at"`
	WeeklyUtilization  float64   `json:"weekly_utilization"`
	WeeklyResetsAt     time.Time `json:"weekly_resets_at"`
	ExtraUsageEnabled  bool      `json:"extra_usage_enabled"`
	ExtraUsedDollars   float64   `json:"extra_used_dollars"`
	ExtraLimitDollars  float64   `json:"extra_limit_dollars"`
	ExtraUtilization   float64   `json:"extra_utilization"`
	Error              string    `json:"error"`
}

func IsStale(e Entry) bool {
	return time.Since(e.FetchedAt) > staleThreshold
}

func Path(cachePath string) string {
	if strings.HasPrefix(cachePath, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, cachePath[2:])
	}
	return cachePath
}

func WriteToPath(p string, e Entry) error {
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

func ReadFromPath(p string) (Entry, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, err
	}
	return e, nil
}
