package cmd

import (
	"os"
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
