package cmd

import (
	"fmt"

	"claude-monitor/internal/cache"
	"claude-monitor/internal/config"
	"claude-monitor/internal/format"
)

func runStatus() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Print(format.StatusLine(cache.Entry{Error: err.Error()}))
		return
	}

	p := cache.Path(cfg.CachePath)
	entry, err := cache.ReadFromPath(p)
	if err != nil {
		// Cache missing — daemon not started yet
		fmt.Print(format.FallbackNoCache())
		return
	}

	fmt.Print(format.StatusLine(entry))
}
