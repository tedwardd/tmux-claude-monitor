package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
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
	fmt.Println("\n[1/6] Verifying auth...")
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

	// Step 2: Bootstrap — get org UUID
	fmt.Println("\n[2/6] Fetching organization info...")
	orgUUID, err := api.FetchBootstrap(creds.AccessToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: bootstrap failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Tip: run 'claude-monitor init --discover' to inspect the raw API response.\n")
		os.Exit(1)
	}
	fmt.Printf("  Organization UUID: %s\n", orgUUID)

	// Verify usage endpoint works and show current quota
	usage, err := api.FetchUsage(creds.AccessToken, orgUUID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: usage fetch failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "Tip: run 'claude-monitor init --discover' to inspect the raw API response.\n")
		os.Exit(1)
	}
	entry := cache.Entry{
		FetchedAt:     time.Now().UTC(),
		MessagesUsed:  usage.MessagesUsed,
		MessagesLimit: usage.MessagesLimit,
		ResetAt:       usage.ResetAt,
	}
	fmt.Printf("  Current usage: %s\n", format.StatusLine(entry))

	// Step 3: Write config
	fmt.Println("\n[3/6] Writing config...")
	cfg.OrgUUID = orgUUID
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: save config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Config written to: %s\n", config.Path())

	// Step 4: Patch tmux config
	fmt.Println("\n[4/6] Patching tmux config...")
	tmuxConf := expandHome("~/.config/tmux/tmux.conf")
	if err := patchTmuxConfig(tmuxConf); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: patch tmux: %v\n", err)
		os.Exit(1)
	}

	// Step 5: Start daemon
	fmt.Println("\n[5/6] Starting daemon...")
	if err := startDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: could not start daemon: %v\n", err)
	} else {
		fmt.Println("  Daemon started.")
	}

	// Step 6: Reload tmux
	fmt.Println("\n[6/6] Reloading tmux config...")
	if out, err := exec.Command("tmux", "source", tmuxConf).CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: tmux reload failed: %v: %s\n", err, out)
	} else {
		fmt.Println("  tmux config reloaded.")
	}

	// Print zshrc snippet
	exe, _ := os.Executable()
	fmt.Printf(`
=== Setup complete! ===

Add this line to ~/.zshrc to auto-start the daemon on login:

  pgrep -x claude-monitor >/dev/null || %s daemon &

Keybinding: <prefix> F5  →  manual refresh
`, exe)
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
		// Remove the old status-right line — we'll replace it in the block
		content = re.ReplaceAllString(content, "")
	}

	exe, _ := os.Executable()
	block := fmt.Sprintf(`

%s
set -g status-right '%s #(  %s status)'
bind-key F5 run-shell '%s refresh'
%s
`, markerBegin, existingRight, exe, exe, markerEnd)

	// Write the updated content (original minus old status-right)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}

	// Append the claude-monitor block
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

func startDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func runDiscover(token string) {
	fmt.Println("\n--- DISCOVER MODE: printing raw API responses ---")

	fmt.Println("\n[bootstrap] GET https://claude.ai/api/bootstrap")
	printRawResponse("https://claude.ai/api/bootstrap", token)

	fmt.Println("\nUse the output above to identify correct field paths in internal/api/client.go")
}

func printRawResponse(url, token string) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  build request: %v\n", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  request failed: %v\n", err)
		return
	}
	defer resp.Body.Close()

	var pretty interface{}
	if err := json.NewDecoder(resp.Body).Decode(&pretty); err != nil {
		fmt.Fprintf(os.Stderr, "  decode failed: %v\n", err)
		return
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(pretty)
}
