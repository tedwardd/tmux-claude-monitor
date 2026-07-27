package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTmuxConfigPathDefaultsToXDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := filepath.Join(home, ".config", "tmux", "tmux.conf")
	if got := tmuxConfigPath(); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTmuxConfigPathFindsLegacyDotfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	legacy := filepath.Join(home, ".tmux.conf")
	if err := os.WriteFile(legacy, nil, 0644); err != nil {
		t.Fatalf("write legacy conf: %v", err)
	}

	if got := tmuxConfigPath(); got != legacy {
		t.Errorf("got %q, want %q", got, legacy)
	}
}

func TestTmuxConfigPathPrefersXDGWhenBothExist(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	xdg := filepath.Join(home, ".config", "tmux", "tmux.conf")
	if err := os.MkdirAll(filepath.Dir(xdg), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, p := range []string{xdg, filepath.Join(home, ".tmux.conf")} {
		if err := os.WriteFile(p, nil, 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	if got := tmuxConfigPath(); got != xdg {
		t.Errorf("got %q, want %q", got, xdg)
	}
}

func TestDefaultStatusRightUsesPlatformLoadavg(t *testing.T) {
	got := defaultStatusRight()

	want, unwanted := "/proc/loadavg", "sysctl"
	if runtime.GOOS == "darwin" {
		want, unwanted = "sysctl -n vm.loadavg", "/proc"
	}
	if !strings.Contains(got, want) {
		t.Errorf("status-right %q should read load average via %q", got, want)
	}
	if strings.Contains(got, unwanted) {
		t.Errorf("status-right %q should not reference %q on %s", got, unwanted, runtime.GOOS)
	}
}

// A single-quoted status-right containing double quotes truncated the capture at
// the inner quote and left the rest of the line orphaned in the config.
func TestPatchTmuxConfigPreservesQuotesInsideValue(t *testing.T) {
	const original = `set -g status-right '#[fg=yellow]#(cut -d " " -f 1-3 /proc/loadavg)#[default] #[fg=white]%H:%M#[default]'`

	home := t.TempDir()
	t.Setenv("HOME", home)
	conf := filepath.Join(home, "tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g status-left 'x'\n"+original+"\nsetw -g mouse on\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}

	if err := patchTmuxConfig(conf); err != nil {
		t.Fatalf("patchTmuxConfig: %v", err)
	}
	data, err := os.ReadFile(conf)
	if err != nil {
		t.Fatalf("read conf: %v", err)
	}
	got := string(data)

	// The whole original value has to survive, not just the part before the quote.
	for _, want := range []string{`-f 1-3 /proc/loadavg`, `%H:%M`} {
		if !strings.Contains(got, want) {
			t.Errorf("patched config dropped %q:\n%s", want, got)
		}
	}
	// No orphaned remainder left behind by a partial match.
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), `" -f`) {
			t.Errorf("orphaned line remainder left in config: %q", line)
		}
	}
	if strings.Contains(got, `#(cut -d  #(`) {
		t.Errorf("status-right was truncated mid-command:\n%s", got)
	}
}

func TestPatchTmuxConfigWritesBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	conf := filepath.Join(home, "tmux.conf")
	const original = "setw -g mouse on\n"
	if err := os.WriteFile(conf, []byte(original), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}

	if err := patchTmuxConfig(conf); err != nil {
		t.Fatalf("patchTmuxConfig: %v", err)
	}

	backup, err := os.ReadFile(conf + ".claude-monitor.bak")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != original {
		t.Errorf("backup should hold the pre-patch content, got %q", backup)
	}
}

// tmux itself is the authority on whether the generated block parses, which is
// what the quote and backslash escaping has to get right.
func TestPatchTmuxConfigOutputParsesInTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	conf := filepath.Join(home, "tmux.conf")
	// Quotes and a backslash sequence that tmux would read as an octal escape.
	const original = `set -g status-right '#[fg=yellow]#(cut -d " " -f 1-3 /proc/loadavg)#[default] \1 %H:%M'`
	if err := os.WriteFile(conf, []byte(original+"\n"), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	if err := patchTmuxConfig(conf); err != nil {
		t.Fatalf("patchTmuxConfig: %v", err)
	}

	socket := "cmtest-parse"
	exec.Command("tmux", "-L", socket, "kill-server").Run()
	if err := exec.Command("tmux", "-L", socket, "-f", "/dev/null", "new-session", "-d", "sleep 5").Run(); err != nil {
		t.Skipf("could not start test tmux server: %v", err)
	}
	defer exec.Command("tmux", "-L", socket, "kill-server").Run()

	out, err := exec.Command("tmux", "-L", socket, "source-file", conf).CombinedOutput()
	if err != nil || len(out) > 0 {
		patched, _ := os.ReadFile(conf)
		t.Errorf("tmux rejected the generated config: %v\n%s\n--- config ---\n%s", err, out, patched)
	}
}

func TestEscapeTmuxDoubleQuoted(t *testing.T) {
	// Backslashes must be doubled first, or escaping the quote produces a stray
	// escape that tmux reads as an octal escape.
	got := escapeTmuxDoubleQuoted(`a\1 "b"`)
	if want := `a\\1 \"b\"`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// patchTmuxConfig must create the config directory; init used to fail outright
// when ~/.config/tmux did not exist.
func TestPatchTmuxConfigCreatesMissingDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	conf := filepath.Join(home, ".config", "tmux", "tmux.conf")
	if err := patchTmuxConfig(conf); err != nil {
		t.Fatalf("patchTmuxConfig: %v", err)
	}

	data, err := os.ReadFile(conf)
	if err != nil {
		t.Fatalf("read patched conf: %v", err)
	}
	for _, want := range []string{"# claude-monitor begin", "claude-monitor status", "# claude-monitor end"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("patched conf missing %q:\n%s", want, data)
		}
	}
}
