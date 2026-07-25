package config_test

import (
	"path/filepath"
	"testing"

	"claude-monitor/internal/config"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg := config.Config{
		PollIntervalSeconds: 120,
		CachePath:           "~/.cache/claude-monitor/status.json",
		CredentialsPath:     "~/.claude/.credentials.json",
	}

	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != cfg {
		t.Errorf("got %+v, want %+v", got, cfg)
	}
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	def := config.DefaultConfig()
	if got.PollIntervalSeconds != def.PollIntervalSeconds {
		t.Errorf("PollIntervalSeconds: got %d, want %d", got.PollIntervalSeconds, def.PollIntervalSeconds)
	}
}

func TestPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	want := filepath.Join(dir, ".config", "claude-monitor", "config.json")
	if got := config.Path(); got != want {
		t.Errorf("Path: got %q, want %q", got, want)
	}
}
