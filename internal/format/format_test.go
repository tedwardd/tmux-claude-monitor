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

func TestDisplayOptionsHideBar(t *testing.T) {
	e := makeEntry(50.0, time.Hour, "")
	opts := format.DisplayOptions{Bar: false, Session: true, Reset: true, Extra: true}
	out := format.StatusLineWithOptions(e, opts)
	if strings.Contains(out, "█") || strings.Contains(out, "░") {
		t.Errorf("bar should be hidden, got: %q", out)
	}
	if !strings.Contains(out, "50%") {
		t.Errorf("session pct should be present, got: %q", out)
	}
}

func TestDisplayOptionsHideSession(t *testing.T) {
	e := makeEntry(50.0, time.Hour, "")
	opts := format.DisplayOptions{Bar: true, Session: false, Reset: false, Extra: false}
	out := format.StatusLineWithOptions(e, opts)
	if strings.Contains(out, "50%") {
		t.Errorf("session pct should be hidden, got: %q", out)
	}
}

func TestDisplayOptionsHideExtra(t *testing.T) {
	e := makeEntry(50.0, time.Hour, "")
	e.ExtraUsageEnabled = true
	e.ExtraUsedDollars = 5.0
	e.ExtraLimitDollars = 50.0
	opts := format.DisplayOptions{Bar: true, Session: true, Reset: true, Extra: false}
	out := format.StatusLineWithOptions(e, opts)
	if strings.Contains(out, "$") {
		t.Errorf("extra should be hidden, got: %q", out)
	}
}

func TestDisplayOptionsDefaultMatchesStatusLine(t *testing.T) {
	e := makeEntry(60.0, 2*time.Hour, "")
	if format.StatusLine(e) != format.StatusLineWithOptions(e, format.DefaultDisplayOptions()) {
		t.Error("StatusLine and StatusLineWithOptions with defaults should produce identical output")
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

// A transient fetch failure must not blank the bar. The 15 minute staleness
// threshold exists to absorb gaps, but it never applied because the daemon
// replaced the reading with an error-only entry, so one bad poll out of a few
// hundred a day showed "??" until the next success.
func TestStatusLineKeepsLastReadingThroughTransientError(t *testing.T) {
	e := makeEntry(42.0, time.Hour, "")
	e.LastError = "usage rate-limited (HTTP 429)"

	got := format.StatusLine(e)
	if strings.Contains(got, "??") {
		t.Errorf("a recent reading with a transient error must still render, got %q", got)
	}
	if !strings.Contains(got, "42%") {
		t.Errorf("expected the preserved reading in %q", got)
	}
}

// Once the reading is genuinely old, "??" is correct even with data present.
func TestStatusLineFallsBackWhenStaleDespiteReading(t *testing.T) {
	e := makeEntry(42.0, time.Hour, "")
	e.FetchedAt = time.Now().Add(-30 * time.Minute).UTC()
	e.LastError = "usage rate-limited (HTTP 429)"

	if got := format.StatusLine(e); !strings.Contains(got, "??") {
		t.Errorf("a stale reading must fall back, got %q", got)
	}
}

// A hard failure with nothing to show still blanks.
func TestStatusLineFallsBackWhenNoReadingAtAll(t *testing.T) {
	e := cache.Entry{Error: "read credentials: no such file"}
	if got := format.StatusLine(e); !strings.Contains(got, "??") {
		t.Errorf("expected fallback with no reading, got %q", got)
	}
}
