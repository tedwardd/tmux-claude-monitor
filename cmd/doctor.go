package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"claude-monitor/internal/api"
	"claude-monitor/internal/cache"
	"claude-monitor/internal/config"
)

// check is one diagnostic line. Bad marks the thing that needs attention, so the
// output leads with the cause rather than making the reader infer it.
type check struct {
	name   string
	detail string
	bad    bool
	// note carries advice without being a failure: something worth knowing that
	// is not itself broken, such as a second usage monitor sharing the token.
	note bool
	fix  string
}

func runDoctor() {
	var checks []check

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		checks = append(checks, check{
			name: "config", detail: cfgErr.Error(), bad: true,
			fix: "Run: claude-monitor init",
		})
	} else {
		checks = append(checks, check{name: "config", detail: config.Path()})
	}

	checks = append(checks, cacheChecks(cfg)...)
	checks = append(checks, daemonCheck())
	checks = append(checks, credentialsCheck(cfg))
	checks = append(checks, competingPollersCheck())

	var failures, advice []check
	fmt.Println("claude-monitor doctor")
	fmt.Println()
	for _, c := range checks {
		mark := "ok  "
		switch {
		case c.bad:
			mark = "FAIL"
			failures = append(failures, c)
		case c.note:
			mark = "note"
			advice = append(advice, c)
		}
		fmt.Printf("  [%s] %-13s %s\n", mark, c.name, c.detail)
	}

	fmt.Println()
	if len(failures) == 0 && len(advice) == 0 {
		fmt.Println("Nothing wrong found. If the status bar still shows ??, the reading")
		fmt.Println("may have gone stale since this ran; try: claude-monitor refresh")
		return
	}

	if len(failures) > 0 {
		fmt.Printf("%d problem(s), most likely cause first:\n\n", len(failures))
		for i, p := range failures {
			fmt.Printf("  %d. %s\n     %s\n", i+1, p.detail, p.fix)
		}
	}
	for _, a := range advice {
		fmt.Printf("\nnote: %s\n      %s\n", a.detail, a.fix)
	}
	if len(failures) > 0 {
		os.Exit(1)
	}
}

func cacheChecks(cfg config.Config) []check {
	p := cache.Path(cfg.CachePath)
	entry, err := cache.ReadFromPath(p)
	if err != nil {
		return []check{{
			name: "cache", detail: "no cache file at " + p, bad: true,
			fix: "The daemon has never written a reading. Start it: claude-monitor init",
		}}
	}

	out := []check{{name: "cache", detail: p}}

	// A hard-error entry carries no measurement, and its FetchedAt is when the
	// failure was recorded, so it says nothing about the last success.
	if entry.Error != "" {
		out = append(out, check{
			name: "reading", detail: "none stored, so the bar shows ??", bad: true,
			fix: "Resolve the fetch error below, then: claude-monitor refresh",
		})
		return append(out, check{
			name: "last fetch", detail: entry.Error, bad: true,
			fix: fixForError(entry.Error),
		})
	}

	age := time.Since(entry.FetchedAt).Truncate(time.Second)
	reading := fmt.Sprintf("last success %s ago", age)
	if cache.IsStale(entry) {
		out = append(out, check{
			name: "reading", detail: reading + " (stale, so the bar shows ??)", bad: true,
			fix: "The daemon is not completing fetches. See the errors below, then: claude-monitor refresh",
		})
	} else {
		out = append(out, check{name: "reading", detail: reading})
	}

	switch {
	case entry.LastError != "":
		// Not a failure in itself: the previous reading is still on display.
		out = append(out, check{
			name: "last fetch",
			detail: fmt.Sprintf("failed %s ago, previous reading still shown: %s",
				time.Since(entry.LastErrorAt).Truncate(time.Second), entry.LastError),
		})
	default:
		out = append(out, check{name: "last fetch", detail: "succeeded"})
	}
	return out
}

func daemonCheck() check {
	pidPath := sharedPIDPath()
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return check{
			name: "daemon", detail: "no PID file at " + pidPath, bad: true,
			fix: "The daemon is not running. Start it: claude-monitor init",
		}
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return check{
			name: "daemon", detail: "unreadable PID file", bad: true,
			fix: "Remove " + pidPath + " and re-run: claude-monitor init",
		}
	}
	if proc, err := os.FindProcess(pid); err != nil || proc.Signal(syscall.Signal(0)) != nil {
		return check{
			name: "daemon", detail: fmt.Sprintf("PID %d is not running", pid), bad: true,
			fix: daemonRestartHint,
		}
	}
	return check{name: "daemon", detail: fmt.Sprintf("running (PID %d)", pid)}
}

func credentialsCheck(cfg config.Config) check {
	if _, err := api.LoadCredentials(expandHome(cfg.CredentialsPath)); err != nil {
		return check{
			name: "credentials", detail: err.Error(), bad: true,
			fix: "Re-authenticate: claude logout && claude login, then " + daemonRestartHint,
		}
	}
	return check{name: "credentials", detail: "readable"}
}

// competingPollersCheck looks for other processes known to poll the same usage
// endpoint. They share the OAuth token, so they share its rate limit, which is
// the usual source of an otherwise unexplained 429.
func competingPollersCheck() check {
	out, err := exec.Command("ps", "-Ao", "comm=").Output()
	if err != nil {
		return check{name: "other pollers", detail: "could not enumerate processes"}
	}

	var found []string
	for _, line := range strings.Split(string(out), "\n") {
		for _, known := range []string{"ClaudeBar", "CodeBurn"} {
			if strings.Contains(line, known) && !contains(found, known) {
				found = append(found, known)
			}
		}
	}
	if len(found) == 0 {
		return check{name: "other pollers", detail: "none found"}
	}
	return check{
		name:   "other pollers",
		note:   true,
		detail: strings.Join(found, ", ") + " also running",
		fix:    "These poll the same endpoint with the same token, so they share its rate limit. Quit them, or raise poll_interval_seconds in " + config.Path() + ".",
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// fixForError maps the failures seen in practice to the action that resolves
// them, so the common cases do not need a trip to the README.
func fixForError(msg string) string {
	switch {
	case strings.Contains(msg, "429"):
		return "Rate limited, usually because something else polls this endpoint with the same token. " +
			"It clears itself; if it persists, quit other usage monitors or raise poll_interval_seconds."
	case strings.Contains(msg, "HTTP 401"), strings.Contains(msg, "HTTP 403"):
		return "The token is rejected. Run: claude logout && claude login, then " + daemonRestartHint
	case strings.Contains(msg, "credentials"):
		return "Run: claude logout && claude login, then " + daemonRestartHint
	case strings.Contains(msg, "bad file descriptor"), strings.Contains(msg, "connection refused"):
		return "The connection was blocked, which on macOS usually means a firewall deny rule. " +
			"Check Little Snitch for a deny rule under claude-monitor and remove it."
	case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "timeout"):
		return "The request timed out. A firewall prompt awaiting an answer looks exactly like this; " +
			"otherwise check general connectivity."
	default:
		return "See the README section on the status bar showing ??."
	}
}
