package cmd

import (
	"fmt"
	"os"
	"path/filepath"
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
	case "doctor":
		runDoctor()
	case "lsrules":
		runLSRules()
	case "--help", "-h", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

// sharedPIDPath returns the canonical PID file path used by both daemon and refresh.
func sharedPIDPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "claude-monitor", "daemon.pid")
}

func printUsage() {
	fmt.Print(`Usage: claude-monitor <command>

Commands:
  init     One-time setup: verify auth, patch tmux config, start daemon
  daemon   Start background poller (writes cache every 5 min)
  status   Print tmux-formatted usage string (reads cache)
  refresh  Signal daemon for immediate fetch
  doctor   Diagnose why the status bar is not showing a reading
  lsrules  Explain the Little Snitch rule group; --subscribe to add it
`)
}
