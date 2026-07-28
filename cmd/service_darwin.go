//go:build darwin

package cmd

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	serviceDescription = "launchd user agent"
	launchdLabel       = "com.github.tedwardd.claude-monitor"
	launchdLogPath     = "~/Library/Logs/claude-monitor.log"

	launchctlTimeout = 15 * time.Second
)

const daemonRestartHint = "restart it: launchctl kickstart -k gui/$(id -u)/" + launchdLabel

const serviceHints = `  launchctl print gui/$(id -u)/com.github.tedwardd.claude-monitor
  launchctl kickstart -k gui/$(id -u)/com.github.tedwardd.claude-monitor
  tail -f ~/Library/Logs/claude-monitor.log
`

const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>daemon</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ThrottleInterval</key>
	<integer>30</integer>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`

func launchAgentPath() string {
	return expandHome("~/Library/LaunchAgents/" + launchdLabel + ".plist")
}

func renderPlist(program, logPath string) string {
	return fmt.Sprintf(plistTemplate, launchdLabel, xmlEscape(program), xmlEscape(logPath), xmlEscape(logPath))
}

// xmlEscape guards against paths containing characters that would break the
// plist, since the program path comes from the filesystem.
func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func serviceAlreadyInstalled() bool {
	_, err := os.Stat(launchAgentPath())
	return err == nil
}

func installService() error {
	program, err := serviceProgram()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}

	logPath := expandHome(launchdLogPath)
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	plistPath := launchAgentPath()
	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(plistPath, []byte(renderPlist(program, logPath)), 0644); err != nil {
		return fmt.Errorf("write agent file: %w", err)
	}
	fmt.Printf("  Agent file: %s\n", plistPath)

	domain := fmt.Sprintf("gui/%d", os.Getuid())
	target := domain + "/" + launchdLabel

	// Drop any previously loaded copy so bootstrap doesn't fail on the label.
	launchctl("bootout", target)
	launchctl("enable", target)

	if out, err := launchctl("bootstrap", domain, plistPath); err != nil {
		// bootstrap is unavailable before macOS 10.11.
		out2, err2 := launchctl("load", "-w", plistPath)
		if err2 != nil {
			return fmt.Errorf("launchctl bootstrap: %v: %s (load fallback: %v: %s)",
				err, strings.TrimSpace(string(out)), err2, strings.TrimSpace(string(out2)))
		}
	}

	// RunAtLoad already started it; kickstart only covers a job left disabled, so a
	// failure here is not worth aborting a setup that otherwise succeeded.
	if out, err := launchctl("kickstart", target); err != nil {
		fmt.Fprintf(os.Stderr, "  WARNING: launchctl kickstart: %v: %s\n", err, strings.TrimSpace(string(out)))
	}

	fmt.Println("  Agent loaded and started.")
	return nil
}

// launchctl runs a launchctl subcommand under a deadline. bootout waits for the
// running daemon to terminate, and the daemon does not act on SIGTERM while a
// fetch is in flight, so an unbounded call can stall init indefinitely.
func launchctl(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), launchctlTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "launchctl", args...).CombinedOutput()
	if ctx.Err() != nil {
		return out, fmt.Errorf("timed out after %s", launchctlTimeout)
	}
	return out, err
}

// serviceProgram returns the executable path to record in the agent. A stable
// PATH entry is preferred over the resolved binary so a Homebrew upgrade doesn't
// leave the agent pointing into an old versioned directory.
func serviceProgram() (string, error) {
	paths, err := executablePaths()
	if err != nil {
		return "", err
	}
	return paths[0], nil
}
