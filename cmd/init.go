package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"claude-monitor/internal/api"
	"claude-monitor/internal/cache"
	"claude-monitor/internal/config"
	"claude-monitor/internal/format"
)

func runInit() {
	var force, discover bool
	for _, arg := range os.Args[2:] {
		switch arg {
		case "--force":
			force = true
		case "--discover":
			discover = true
		}
	}

	fmt.Println("=== claude-monitor init ===")

	// Step 1: Verify auth
	fmt.Println("\n[1/5] Verifying auth...")
	cfg, _ := config.Load()
	credPath := expandHome(cfg.CredentialsPath)
	creds, err := api.LoadCredentials(credPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		fmt.Fprintf(os.Stderr, "Make sure Claude Code is installed and you are logged in.\n")
		os.Exit(1)
	}
	fmt.Println("  Auth token found.")

	if discover {
		runDiscover(creds.AccessToken)
		return
	}

	// Step 2: Verify usage endpoint and show current quota
	fmt.Println("\n[2/5] Fetching usage from Anthropic API...")
	usage, err := api.FetchUsage(creds.AccessToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: usage fetch failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Tip: run 'claude-monitor init --discover' to inspect the raw API response.\n")
		os.Exit(1)
	}
	entry := cache.Entry{
		FetchedAt:          time.Now().UTC(),
		SessionUtilization: usage.SessionUtilization,
		SessionResetsAt:    usage.SessionResetsAt,
		WeeklyUtilization:  usage.WeeklyUtilization,
		WeeklyResetsAt:     usage.WeeklyResetsAt,
		ExtraUsageEnabled:  usage.ExtraUsageEnabled,
		ExtraUsedDollars:   usage.ExtraUsedDollars,
		ExtraLimitDollars:  usage.ExtraLimitDollars,
		ExtraUtilization:   usage.ExtraUtilization,
	}
	fmt.Printf("  Current usage: %s\n", format.StatusLine(entry))

	// Step 3: Write config
	fmt.Println("\n[3/5] Writing config...")
	if !force && configExists() {
		fmt.Printf("  Already exists at %s, skipping.\n", config.Path())
	} else {
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: save config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("  Config written to: %s\n", config.Path())
	}

	// Step 4: Patch tmux config
	fmt.Println("\n[4/5] Patching tmux config...")
	tmuxConf := tmuxConfigPath()
	if !force && tmuxAlreadyPatched(tmuxConf) {
		fmt.Println("  tmux config already patched, skipping.")
	} else {
		if err := patchTmuxConfig(tmuxConf); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: patch tmux: %v\n", err)
			os.Exit(1)
		}
	}

	// Step 5: Install and start the platform service
	fmt.Printf("\n[5/5] Installing %s...\n", serviceDescription)
	if !force && serviceAlreadyInstalled() {
		fmt.Println("  Service already installed, skipping.")
	} else {
		if err := installService(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: service setup: %v\n", err)
			os.Exit(1)
		}
	}

	// Reload tmux
	if out, err := exec.Command("tmux", "source", tmuxConf).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: tmux reload failed: %v: %s\n", err, out)
	} else {
		fmt.Println("  tmux config reloaded.")
	}

	fmt.Printf(`
=== Setup complete! ===

Service auto-starts on login. To manage it:
%s
Keybinding: <prefix> F5  →  manual refresh
`, serviceHints)
}

func configExists() bool {
	_, err := os.Stat(config.Path())
	return err == nil
}

func tmuxAlreadyPatched(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "# claude-monitor begin")
}

// tmuxConfigPath returns the config file to patch, preferring one that already
// exists over the XDG default.
func tmuxConfigPath() string {
	candidates := []string{"~/.config/tmux/tmux.conf", "~/.tmux.conf"}
	for _, c := range candidates {
		if _, err := os.Stat(expandHome(c)); err == nil {
			return expandHome(c)
		}
	}
	return expandHome(candidates[0])
}

// defaultStatusRight is used when the user's tmux config has no status-right to
// preserve. Load average has no portable source.
func defaultStatusRight() string {
	loadavg := "#(cut -d ' ' -f 1-3 /proc/loadavg)"
	if runtime.GOOS == "darwin" {
		loadavg = "#(sysctl -n vm.loadavg | cut -d ' ' -f 2-4)"
	}
	return "#[fg=yellow]" + loadavg + "#[default] #[fg=white]%H:%M#[default]"
}

// statusRightRe matches a quoted status-right assignment. Each quote style needs
// its own alternative: RE2 has no backreferences, and a combined ['"] class lets a
// value opened with ' terminate on an embedded ", which truncates the capture and
// leaves the rest of the line behind.
var statusRightRe = regexp.MustCompile(`(?m)^set\s+-g\s+status-right\s+(?:'([^']*)'|"([^"]*)")[ \t]*$`)

// escapeTmuxDoubleQuoted prepares a value for embedding in a double-quoted tmux
// string. tmux expands backslash escapes there, so the escape character has to be
// doubled before the quotes are escaped.
func escapeTmuxDoubleQuoted(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `"`, `\"`)
}

// backupTmuxConfig keeps a copy before rewriting, since this edits a file the user
// maintains by hand.
func backupTmuxConfig(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.WriteFile(path+".claude-monitor.bak", data, 0644)
}

func patchTmuxConfig(path string) error {
	const markerBegin = "# claude-monitor begin"
	const markerEnd = "# claude-monitor end"

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)

	if err := backupTmuxConfig(path); err != nil {
		return fmt.Errorf("back up config: %w", err)
	}

	existingRight := defaultStatusRight()
	if match := statusRightRe.FindStringSubmatch(content); match != nil {
		// Exactly one of the two quote alternatives captures.
		existingRight = match[1] + match[2]
		content = statusRightRe.ReplaceAllString(content, "")
	}
	existingRight = escapeTmuxDoubleQuoted(existingRight)

	exe := "claude-monitor"
	block := fmt.Sprintf(`

%s
set -g status-right-length 200
set -g status-right "%s #(  %s status)"
bind-key F5 run-shell "%s refresh"
%s
`, markerBegin, existingRight, exe, exe, markerEnd)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(block)
	if err == nil {
		fmt.Printf("  Patched: %s\n", path)
	}
	return err
}

func runDiscover(token string) {
	fmt.Println("\n--- DISCOVER MODE: printing raw usage API response ---")
	fmt.Println("\n[usage] GET https://api.anthropic.com/api/oauth/usage")
	printRawResponse("https://api.anthropic.com/api/oauth/usage", token)
	fmt.Println("\nUse the output above to verify field paths in internal/api/client.go")
}

func printRawResponse(url, token string) interface{} {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  build request: %v\n", err)
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("User-Agent", "claude-monitor/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  request failed: %v\n", err)
		return nil
	}
	defer resp.Body.Close()

	var pretty interface{}
	if err := json.NewDecoder(resp.Body).Decode(&pretty); err != nil {
		fmt.Fprintf(os.Stderr, "  decode failed (HTTP %d): %v\n", resp.StatusCode, err)
		return nil
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(pretty)
	return pretty
}
