//go:build !darwin

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const serviceDescription = "systemd user service"

const serviceHints = `  systemctl --user status claude-monitor
  systemctl --user restart claude-monitor
  journalctl --user -u claude-monitor -f
`

const unitTemplate = `[Unit]
Description=Claude usage monitor
After=network.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
ExecStart=claude-monitor daemon
Restart=on-failure
RestartSec=30

[Install]
WantedBy=default.target
`

func serviceAlreadyInstalled() bool {
	home, _ := os.UserHomeDir()
	userUnit := filepath.Join(home, ".config", "systemd", "user", "claude-monitor.service")
	sysUnit := "/usr/lib/systemd/user/claude-monitor.service"
	_, errUser := os.Stat(userUnit)
	_, errSys := os.Stat(sysUnit)
	// If a package-installed system unit exists, remove any stale user-level unit
	// that would shadow it (user units take precedence over system units).
	if errSys == nil && errUser == nil {
		if err := os.Remove(userUnit); err == nil {
			fmt.Println("  Removed stale user unit shadowing package-installed service.")
			exec.Command("systemctl", "--user", "daemon-reload").Run()
		}
	}
	return errUser == nil || errSys == nil
}

func installService() error {
	unitDir := expandHome("~/.config/systemd/user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("create unit dir: %w", err)
	}

	unitPath := filepath.Join(unitDir, "claude-monitor.service")
	if err := os.WriteFile(unitPath, []byte(unitTemplate), 0644); err != nil {
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
