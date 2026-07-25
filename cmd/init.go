package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"claude-monitor/internal/api"
	"claude-monitor/internal/cache"
	"claude-monitor/internal/config"
	"claude-monitor/internal/format"
)

func runInit() {
	discover := len(os.Args) > 2 && os.Args[2] == "--discover"

	fmt.Println("=== claude-monitor init ===")

	// Step 1: Verify auth
	fmt.Println("\n[1/5] Verifying auth...")
	cfg, _ := config.Load()
	credPath := expandHome(cfg.CredentialsPath)
	creds, err := api.ReadCredentials(credPath)
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
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: save config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Config written to: %s\n", config.Path())

	// Step 4: Patch tmux config
	fmt.Println("\n[4/5] Patching tmux config...")
	tmuxConf := expandHome("~/.config/tmux/tmux.conf")
	if err := patchTmuxConfig(tmuxConf); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: patch tmux: %v\n", err)
		os.Exit(1)
	}

	// Step 5: Install and start systemd user service
	fmt.Println("\n[5/5] Installing systemd user service...")
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: locate executable: %v\n", err)
		os.Exit(1)
	}
	if err := installSystemdService(exe); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: systemd setup: %v\n", err)
		os.Exit(1)
	}

	// Reload tmux
	if out, err := exec.Command("tmux", "source", tmuxConf).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: tmux reload failed: %v: %s\n", err, out)
	} else {
		fmt.Println("  tmux config reloaded.")
	}

	fmt.Println(`
=== Setup complete! ===

Service auto-starts on login. To manage it:
  systemctl --user status claude-monitor
  systemctl --user restart claude-monitor
  journalctl --user -u claude-monitor -f

Keybinding: <prefix> F5  →  manual refresh`)
}

const unitTemplate = `[Unit]
Description=Claude usage monitor
After=network.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
ExecStart=%s daemon
Restart=on-failure
RestartSec=30

[Install]
WantedBy=default.target
`

func installSystemdService(exe string) error {
	unitDir := expandHome("~/.config/systemd/user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("create unit dir: %w", err)
	}

	unitPath := filepath.Join(unitDir, "claude-monitor.service")
	unit := fmt.Sprintf(unitTemplate, exe)
	if err := os.WriteFile(unitPath, []byte(unit), 0644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	fmt.Printf("  Unit file: %s\n", unitPath)

	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "claude-monitor"},
		{"--user", "restart", "claude-monitor"},
	} {
		out, err := exec.Command("systemctl", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	fmt.Println("  Service enabled and started.")
	return nil
}

func patchTmuxConfig(path string) error {
	const markerBegin = "# claude-monitor begin"
	const markerEnd = "# claude-monitor end"

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)

	if strings.Contains(content, markerBegin) {
		fmt.Println("  tmux config already patched, skipping.")
		return nil
	}

	// Extract current status-right value
	re := regexp.MustCompile(`(?m)^set\s+-g\s+status-right\s+'([^']*)'`)
	match := re.FindStringSubmatch(content)
	existingRight := "#[fg=yellow]#(cut -d ' ' -f 1-3 /proc/loadavg)#[default] #[fg=white]%H:%M#[default]"
	if len(match) > 1 {
		existingRight = match[1]
		content = re.ReplaceAllString(content, "")
	}

	exe, _ := os.Executable()
	if exe == "" {
		exe = "claude-monitor"
	}
	block := fmt.Sprintf(`

%s
set -g status-right-length 200
set -g status-right '%s #(  %s status)'
bind-key F5 run-shell '%s refresh'
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
