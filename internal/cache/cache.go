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
	FetchedAt     time.Time `json:"fetched_at"`
	MessagesUsed  int       `json:"messages_used"`
	MessagesLimit int       `json:"messages_limit"`
	ResetAt       time.Time `json:"reset_at"`
	Error         string    `json:"error"`
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
