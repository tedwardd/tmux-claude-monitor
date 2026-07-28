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

	// Error means there is nothing to display: no credentials, no reading ever.
	Error string `json:"error"`

	// LastError is a failed fetch that left an earlier reading intact. The status
	// line keeps showing that reading until FetchedAt goes stale, so a transient
	// failure does not blank the display.
	LastError   string    `json:"last_error,omitempty"`
	LastErrorAt time.Time `json:"last_error_at,omitempty"`
}

// HasReading reports whether the entry holds a usable measurement.
func (e Entry) HasReading() bool {
	return !e.FetchedAt.IsZero() && e.Error == ""
}

// RecordFailure notes a failed fetch without discarding the last good reading.
// Blanking the display on a single bad poll wasted the staleness threshold that
// exists to absorb exactly these gaps.
func RecordFailure(p string, fetchErr error) error {
	e, err := ReadFromPath(p)
	if err != nil || !e.HasReading() {
		return WriteToPath(p, Entry{FetchedAt: time.Now().UTC(), Error: fetchErr.Error()})
	}
	e.LastError = fetchErr.Error()
	e.LastErrorAt = time.Now().UTC()
	return WriteToPath(p, e)
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
