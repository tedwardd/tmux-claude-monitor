package format

import (
	"fmt"
	"strings"

	"claude-monitor/internal/cache"
)

const (
	barWidth = 6
	fallback = "#[fg=colour244]Claude: ??#[default]"
)

func StatusLine(e cache.Entry) string {
	if e.Error != "" || cache.IsStale(e) {
		return fallback
	}

	pct := 0
	if e.MessagesLimit > 0 {
		pct = (e.MessagesUsed * 100) / e.MessagesLimit
	}

	color := colorCode(pct)
	bar := blockBar(e.MessagesUsed, e.MessagesLimit)
	reset := e.ResetAt.Local().Format("15:04")

	return fmt.Sprintf("%sClaude: %s %d%% ↺%s#[default]", color, bar, pct, reset)
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

func blockBar(used, limit int) string {
	if limit == 0 {
		return strings.Repeat("░", barWidth)
	}
	filled := (used * barWidth) / limit
	if filled > barWidth {
		filled = barWidth
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
}
