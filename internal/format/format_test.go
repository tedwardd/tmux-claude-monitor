package format_test

import (
	"strings"
	"testing"
	"time"

	"claude-monitor/internal/cache"
	"claude-monitor/internal/format"
)

func makeEntry(sessionPct float64, resetOffset time.Duration, errStr string) cache.Entry {
	return cache.Entry{
		FetchedAt:          time.Now(),
		SessionUtilization: sessionPct,
		SessionResetsAt:    time.Now().Add(resetOffset),
		Error:              errStr,
	}
}

func TestGreenWhenBelowSeventy(t *testing.T) {
	e := makeEntry(60.0, 2*time.Hour, "")
	out := format.StatusLine(e)
	if !strings.Contains(out, "#[fg=green]") {
		t.Errorf("expected green, got: %q", out)
	}
	if !strings.Contains(out, "60%") {
		t.Errorf("expected 60%%, got: %q", out)
	}
}

func TestYellowAt70Percent(t *testing.T) {
	e := makeEntry(70.0, 2*time.Hour, "")
	out := format.StatusLine(e)
	if !strings.Contains(out, "#[fg=yellow]") {
		t.Errorf("expected yellow, got: %q", out)
	}
}

func TestRedAt90Percent(t *testing.T) {
	e := makeEntry(90.0, 2*time.Hour, "")
	out := format.StatusLine(e)
	if !strings.Contains(out, "#[fg=red]") {
		t.Errorf("expected red, got: %q", out)
	}
}

func TestErrorShowsFallback(t *testing.T) {
	e := cache.Entry{FetchedAt: time.Now(), Error: "network error"}
	out := format.StatusLine(e)
	if !strings.Contains(out, "colour244") {
		t.Errorf("expected grey fallback, got: %q", out)
	}
	if !strings.Contains(out, "??") {
		t.Errorf("expected ??, got: %q", out)
	}
}

func TestStaleShowsFallback(t *testing.T) {
	e := cache.Entry{
		FetchedAt:          time.Now().Add(-20 * time.Minute),
		SessionUtilization: 10.0,
	}
	out := format.StatusLine(e)
	if !strings.Contains(out, "colour244") {
		t.Errorf("expected grey fallback for stale, got: %q", out)
	}
}

func TestBlockBar6Wide(t *testing.T) {
	e := makeEntry(100.0, time.Hour, "") // 100% — all blocks filled
	out := format.StatusLine(e)
	if !strings.Contains(out, "██████") {
		t.Errorf("expected 6 filled blocks, got: %q", out)
	}
}

func TestResetTimeInOutput(t *testing.T) {
	resetAt := time.Date(2026, 7, 23, 14, 30, 0, 0, time.Local)
	e := cache.Entry{
		FetchedAt:          time.Now(),
		SessionUtilization: 20.0,
		SessionResetsAt:    resetAt,
	}
	out := format.StatusLine(e)
	if !strings.Contains(out, "↺14:30") {
		t.Errorf("expected reset time, got: %q", out)
	}
}
