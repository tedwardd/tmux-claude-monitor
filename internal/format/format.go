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

func FallbackNoCache() string {
	return fallbackNoCache
}

func StatusLine(e cache.Entry) string {
	if e.Error != "" || cache.IsStale(e) {
		return fallback
	}

	pct := int(e.SessionUtilization)
	color := colorCode(pct)
	bar := blockBar(pct)
	reset := e.SessionResetsAt.Local().Format("15:04")

	s := fmt.Sprintf("%sClaude: %s %d%% ↺%s#[default]", color, bar, pct, reset)

	if e.ExtraUsageEnabled {
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
