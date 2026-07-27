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

const userStatusRight = `set -g status-right '#[fg=yellow]#(cut -d " " -f 1-3 /proc/loadavg)#[default] #[fg=white]%H:%M#[default]'`

func writeConf(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	conf := filepath.Join(home, "tmux.conf")
	if err := os.WriteFile(conf, []byte(body), 0644); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	return conf
}

// The user's own status-right line must survive untouched. It used to be deleted
// and folded into the generated block, which lost content when the value held a
// quote and made re-patching unable to recover it.
func TestPatchTmuxConfigLeavesUserStatusRightAlone(t *testing.T) {
	conf := writeConf(t, "set -g status-left 'x'\n"+userStatusRight+"\nsetw -g mouse on\n")

	if err := patchTmuxConfig(conf); err != nil {
		t.Fatalf("patchTmuxConfig: %v", err)
	}
	data, _ := os.ReadFile(conf)
	got := string(data)

	if !strings.Contains(got, userStatusRight) {
		t.Errorf("user's status-right line was modified:\n%s", got)
	}
	if !strings.Contains(got, `set -ga status-right " #(  claude-monitor status)"`) {
		t.Errorf("append line missing:\n%s", got)
	}
	// With a value to append to, no synthesised default should be written.
	if strings.Contains(got, "sysctl -n vm.loadavg") || strings.Contains(got, "-f 1-3 /proc/loadavg)#[default] #[fg=white]%H:%M#[default] #(") {
		t.Errorf("should not have written its own status-right:\n%s", got)
	}
}

// Re-patching is what `init --force` does; it used to stack a second block and
// replace the preserved value with the platform default.
func TestPatchTmuxConfigIsIdempotent(t *testing.T) {
	conf := writeConf(t, userStatusRight+"\n")

	for i := 1; i <= 3; i++ {
		if err := patchTmuxConfig(conf); err != nil {
			t.Fatalf("patch %d: %v", i, err)
		}
	}
	data, _ := os.ReadFile(conf)
	got := string(data)

	if n := strings.Count(got, "# claude-monitor begin"); n != 1 {
		t.Errorf("got %d blocks, want 1:\n%s", n, got)
	}
	if n := strings.Count(got, "claude-monitor status"); n != 1 {
		t.Errorf("got %d status calls, want 1:\n%s", n, got)
	}
	if n := strings.Count(got, "bind-key F5"); n != 1 {
		t.Errorf("got %d F5 bindings, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, userStatusRight) {
		t.Errorf("user's status-right lost across re-patching:\n%s", got)
	}
}

func TestPatchTmuxConfigSuppliesDefaultWhenNoneSet(t *testing.T) {
	conf := writeConf(t, "setw -g mouse on\n")

	if err := patchTmuxConfig(conf); err != nil {
		t.Fatalf("patchTmuxConfig: %v", err)
	}
	data, _ := os.ReadFile(conf)
	got := string(data)

	if !strings.Contains(got, "set -g status-right \"") {
		t.Errorf("expected a default status-right when the config sets none:\n%s", got)
	}
	if !strings.Contains(got, "set -ga status-right") {
		t.Errorf("append line missing:\n%s", got)
	}
}

// Sourcing the config twice must not append the segment twice, which is the risk
// that comes with `set -ga`.
func TestPatchTmuxConfigSurvivesRepeatedSourcing(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}

	for name, body := range map[string]string{
		"user has status-right": userStatusRight + "\n",
		"config sets none":      "setw -g mouse on\n",
	} {
		t.Run(name, func(t *testing.T) {
			conf := writeConf(t, body)
			if err := patchTmuxConfig(conf); err != nil {
				t.Fatalf("patchTmuxConfig: %v", err)
			}

			socket := "cmtest-src"
			exec.Command("tmux", "-L", socket, "kill-server").Run()
			if err := exec.Command("tmux", "-L", socket, "-f", "/dev/null", "new-session", "-d", "sleep 10").Run(); err != nil {
				t.Skipf("could not start tmux: %v", err)
			}
			defer exec.Command("tmux", "-L", socket, "kill-server").Run()

			for i := 0; i < 3; i++ {
				if out, err := exec.Command("tmux", "-L", socket, "source-file", conf).CombinedOutput(); err != nil || len(out) > 0 {
					t.Fatalf("source %d failed: %v: %s", i, err, out)
				}
			}

			out, err := exec.Command("tmux", "-L", socket, "show-options", "-gv", "status-right").Output()
			if err != nil {
				t.Fatalf("show-options: %v", err)
			}
			if n := strings.Count(string(out), "claude-monitor status"); n != 1 {
				t.Errorf("after 3 sources status-right holds %d copies: %s", n, out)
			}
		})
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
