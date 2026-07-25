package format

import (
	"fmt"
	"strings"

	"claude-monitor/internal/cache"
)

const (
	barWidth        = 6
	fallback        = "#[fg=colour244]Claude: ??#[default]"
	fallbackNoCache = "#[fg=colour244]Claude: --#[default]"
)

// DisplayOptions controls which components appear in the status line.
type DisplayOptions struct {
	Bar     bool
	Session bool
	Reset   bool
	Extra   bool
}

// DefaultDisplayOptions returns options that show all components.
func DefaultDisplayOptions() DisplayOptions {
	return DisplayOptions{Bar: true, Session: true, Reset: true, Extra: true}
}

func FallbackNoCache() string {
	return fallbackNoCache
}

// StatusLine renders with all components shown.
func StatusLine(e cache.Entry) string {
	return StatusLineWithOptions(e, DefaultDisplayOptions())
}

// StatusLineWithOptions renders only the components enabled in opts.
func StatusLineWithOptions(e cache.Entry, opts DisplayOptions) string {
	if e.Error != "" || cache.IsStale(e) {
		return fallback
	}

	pct := int(e.SessionUtilization)
	color := colorCode(pct)

	var parts []string
	if opts.Bar {
		parts = append(parts, blockBar(pct))
	}
	if opts.Session {
		parts = append(parts, fmt.Sprintf("%d%%", pct))
	}
	if opts.Reset {
		parts = append(parts, "↺"+e.SessionResetsAt.Local().Format("15:04"))
	}

	s := color + "Claude: " + strings.Join(parts, " ") + "#[default]"

	if opts.Extra && e.ExtraUsageEnabled {
		extraColor := colorCode(int(e.ExtraUtilization))
		s += fmt.Sprintf(" %s+$%.2f/$%.2f#[default]", extraColor, e.ExtraUsedDollars, e.ExtraLimitDollars)
	}

	return s
}

func colorCode(pct int) string {
	switch {
	case pct >= 90:
		return "#[fg=red]"
	case pct >= 70:
		return "#[fg=yellow]"
	default:
		return "#[fg=green]"
	}
}

func blockBar(pct int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := (pct * barWidth) / 100
	return strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
}
