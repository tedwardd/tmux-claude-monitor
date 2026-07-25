package config_test

import (
	"path/filepath"
	"reflect"
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
	if !reflect.DeepEqual(got, cfg) {
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

func TestShowsEmptyMeansAll(t *testing.T) {
	cfg := config.Config{}
	for _, item := range []string{"bar", "session", "reset", "extra"} {
		if !cfg.Shows(item) {
			t.Errorf("empty Display: Shows(%q) = false, want true", item)
		}
	}
}

func TestShowsFiltersCorrectly(t *testing.T) {
	cfg := config.Config{Display: []string{"bar", "session"}}
	if !cfg.Shows("bar") {
		t.Error("Shows(bar) = false, want true")
	}
	if !cfg.Shows("session") {
		t.Error("Shows(session) = false, want true")
	}
	if cfg.Shows("reset") {
		t.Error("Shows(reset) = true, want false")
	}
	if cfg.Shows("extra") {
		t.Error("Shows(extra) = true, want false")
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
