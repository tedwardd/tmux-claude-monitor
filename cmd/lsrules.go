package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// The one endpoint the daemon talks to. Kept here as the single place the rule
// group and the client agree on.
const (
	usageHost = "api.anthropic.com"
	usagePort = "443"
)

type lsRule struct {
	Action      string `json:"action"`
	Process     string `json:"process"`
	Direction   string `json:"direction"`
	Protocol    string `json:"protocol,omitempty"`
	Ports       string `json:"ports,omitempty"`
	RemoteHosts string `json:"remote-hosts,omitempty"`
	Remote      string `json:"remote,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

type lsGroup struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Rules       []lsRule `json:"rules"`
}

// executablePaths returns the paths this binary can legitimately be identified
// by, most stable first. Little Snitch matches a rule against the full
// executable path, and a Homebrew install is reachable both through a fixed
// symlink in the prefix and through the versioned directory it resolves to.
// Emitting both keeps the rule scoped to this binary without depending on which
// one the firewall records.
func executablePaths() ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}

	paths := []string{resolved}
	if linked, err := exec.LookPath(filepath.Base(resolved)); err == nil {
		if abs, err := filepath.Abs(linked); err == nil && abs != resolved {
			if same, err := filepath.EvalSymlinks(abs); err == nil && same == resolved {
				paths = append([]string{abs}, paths...)
			}
		}
	}
	return paths, nil
}

func buildLSGroup(paths []string, strict bool) lsGroup {
	g := lsGroup{
		Name: "claude-monitor",
		Description: "Every outbound connection claude-monitor makes. The daemon " +
			"contacts one endpoint, the Anthropic OAuth usage API, and nothing else. " +
			"Generated for this install by `claude-monitor lsrules`, so the rules name " +
			"this binary rather than granting access to anything else on the machine.",
	}

	for _, p := range paths {
		g.Rules = append(g.Rules, lsRule{
			Action:      "allow",
			Process:     p,
			Direction:   "outgoing",
			Protocol:    "tcp",
			Ports:       usagePort,
			RemoteHosts: usageHost,
			Notes: "GET https://" + usageHost + "/api/oauth/usage. Sent once per poll " +
				"interval (5 minutes by default), on `claude-monitor refresh`, and on wake " +
				"from sleep. The only connection the daemon opens.",
		})
	}

	notes := "Nothing else is needed, so denying the rest makes the allow rules above " +
		"an enforceable boundary rather than a description."
	if !strict {
		notes += " Disabled by default: enable it, or regenerate with --strict, once you " +
			"are satisfied the allow rules cover your setup."
	}
	for _, p := range paths {
		g.Rules = append(g.Rules, lsRule{
			Action:    "deny",
			Process:   p,
			Direction: "outgoing",
			Remote:    "any",
			Disabled:  !strict,
			Notes:     notes,
		})
	}
	return g
}

func runLSRules() {
	strict := false
	for _, arg := range os.Args[2:] {
		if arg == "--strict" {
			strict = true
		}
	}

	paths, err := executablePaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "lsrules: locate executable: %v\n", err)
		os.Exit(1)
	}

	data, err := json.MarshalIndent(buildLSGroup(paths, strict), "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "lsrules: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}
