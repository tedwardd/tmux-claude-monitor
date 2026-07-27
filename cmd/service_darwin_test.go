//go:build darwin

package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// plutil is the authority on whether launchd will accept the agent file.
func TestRenderPlistIsValid(t *testing.T) {
	plist := renderPlist("/opt/homebrew/bin/claude-monitor", "/Users/someone/Library/Logs/claude-monitor.log")

	path := filepath.Join(t.TempDir(), "agent.plist")
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	out, err := exec.Command("plutil", "-lint", path).CombinedOutput()
	if err != nil {
		t.Fatalf("plutil -lint rejected the agent file: %v\n%s\n%s", err, out, plist)
	}

	for _, want := range []string{
		"<string>com.github.tedwardd.claude-monitor</string>",
		"<string>/opt/homebrew/bin/claude-monitor</string>",
		"<string>daemon</string>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist missing %q:\n%s", want, plist)
		}
	}
}

func TestRenderPlistEscapesPath(t *testing.T) {
	plist := renderPlist("/tmp/claude & co/claude-monitor", "/tmp/log")

	if strings.Contains(plist, "/tmp/claude & co/") {
		t.Error("ampersand in program path left unescaped")
	}
	if !strings.Contains(plist, "/tmp/claude &amp; co/claude-monitor") {
		t.Errorf("expected escaped program path:\n%s", plist)
	}

	path := filepath.Join(t.TempDir(), "agent.plist")
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		t.Fatalf("write plist: %v", err)
	}
	if out, err := exec.Command("plutil", "-lint", path).CombinedOutput(); err != nil {
		t.Fatalf("plutil -lint rejected the agent file: %v\n%s", err, out)
	}
}

// A claude-monitor on PATH that resolves to some other binary must not be
// recorded in the agent.
func TestServiceProgramIgnoresUnrelatedPathEntry(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "versioned", "claude-monitor")
	if err := os.MkdirAll(filepath.Dir(real), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(real, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	link := filepath.Join(binDir, "claude-monitor")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	t.Setenv("PATH", binDir)

	got, err := serviceProgram()
	if err != nil {
		t.Fatalf("serviceProgram: %v", err)
	}
	exe, _ := os.Executable()
	resolved, _ := filepath.EvalSymlinks(exe)
	if got != resolved {
		t.Errorf("got %q, want the running executable %q", got, resolved)
	}
}
