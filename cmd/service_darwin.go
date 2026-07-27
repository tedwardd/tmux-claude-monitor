//go:build darwin

package cmd

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	serviceDescription = "launchd user agent"
	launchdLabel       = "com.github.tedwardd.claude-monitor"
	launchdLogPath     = "~/Library/Logs/claude-monitor.log"
)

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
	exec.Command("launchctl", "bootout", target).Run()
	exec.Command("launchctl", "enable", target).Run()

	if out, err := exec.Command("launchctl", "bootstrap", domain, plistPath).CombinedOutput(); err != nil {
		// bootstrap is unavailable before macOS 10.11.
		out2, err2 := exec.Command("launchctl", "load", "-w", plistPath).CombinedOutput()
		if err2 != nil {
			return fmt.Errorf("launchctl bootstrap: %v: %s (load fallback: %v: %s)",
				err, strings.TrimSpace(string(out)), err2, strings.TrimSpace(string(out2)))
		}
	}

	if out, err := exec.Command("launchctl", "kickstart", "-k", target).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl kickstart: %v: %s", err, strings.TrimSpace(string(out)))
	}

	fmt.Println("  Agent loaded and started.")
	return nil
}

// serviceProgram returns the executable path to record in the agent. A stable
// PATH entry is preferred over the resolved binary so a Homebrew upgrade doesn't
// leave the agent pointing into an old versioned directory.
func serviceProgram() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	if linked, err := exec.LookPath(filepath.Base(resolved)); err == nil {
		if abs, err := filepath.Abs(linked); err == nil {
			if same, err := filepath.EvalSymlinks(abs); err == nil && same == resolved {
				return abs, nil
			}
		}
	}
	return resolved, nil
}
