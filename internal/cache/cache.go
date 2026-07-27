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
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}

	// Write then rename, so a daemon killed mid-write leaves the previous cache
	// intact rather than a truncated file for the status command to choke on.
	tmp, err := os.CreateTemp(dir, ".status-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
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
