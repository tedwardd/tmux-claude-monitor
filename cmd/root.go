package cmd

import (
	"fmt"
	"os"
)

func Execute() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "daemon":
		runDaemon()
	case "status":
		runStatus()
	case "refresh":
		runRefresh()
	case "init":
		runInit()
	case "--help", "-h", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`Usage: claude-monitor <command>

Commands:
  init     One-time setup: verify auth, patch tmux config, start daemon
  daemon   Start background poller (writes cache every 5 min)
  status   Print tmux-formatted usage string (reads cache)
  refresh  Signal daemon for immediate fetch
`)
}
