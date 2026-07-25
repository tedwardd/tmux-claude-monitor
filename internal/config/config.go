package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
	CachePath           string `json:"cache_path"`
	CredentialsPath     string `json:"credentials_path"`
}

func DefaultConfig() Config {
	return Config{
		PollIntervalSeconds: 300,
		CachePath:           "~/.cache/claude-monitor/status.json",
		CredentialsPath:     "~/.claude/.credentials.json",
	}
}

func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claude-monitor", "config.json")
}

func Load() (Config, error) {
	p := Path()
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(cfg Config) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

// ExpandPath replaces a leading ~/ with the user home directory.
func ExpandPath(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, p[2:])
}
