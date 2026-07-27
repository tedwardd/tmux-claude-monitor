package cache_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"claude-monitor/internal/cache"
)

func tempCachePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "status.json")
}

func TestWriteRead(t *testing.T) {
	p := tempCachePath(t)
	now := time.Now().UTC().Truncate(time.Second)
	sessionReset := now.Add(2 * time.Hour)
	weeklyReset := now.Add(7 * 24 * time.Hour)

	entry := cache.Entry{
		FetchedAt:          now,
		SessionUtilization: 84.0,
		SessionResetsAt:    sessionReset,
		WeeklyUtilization:  15.0,
		WeeklyResetsAt:     weeklyReset,
		Error:              "",
	}

	if err := cache.WriteToPath(p, entry); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := cache.ReadFromPath(p)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got.SessionUtilization != 84.0 {
		t.Errorf("SessionUtilization: got %f", got.SessionUtilization)
	}
	if got.WeeklyUtilization != 15.0 {
		t.Errorf("WeeklyUtilization: got %f", got.WeeklyUtilization)
	}
	if !got.FetchedAt.Equal(now) {
		t.Errorf("FetchedAt: got %v, want %v", got.FetchedAt, now)
	}
	if !got.SessionResetsAt.Equal(sessionReset) {
		t.Errorf("SessionResetsAt: got %v, want %v", got.SessionResetsAt, sessionReset)
	}
}

func TestIsStale(t *testing.T) {
	fresh := cache.Entry{FetchedAt: time.Now().Add(-5 * time.Minute)}
	old := cache.Entry{FetchedAt: time.Now().Add(-20 * time.Minute)}

	if cache.IsStale(fresh) {
		t.Error("fresh entry should not be stale")
	}
	if !cache.IsStale(old) {
		t.Error("old entry should be stale")
	}
}

func TestReadMissingReturnsError(t *testing.T) {
	_, err := cache.ReadFromPath("/tmp/does-not-exist-xyz.json")
	if err == nil {
		t.Error("expected error reading missing cache file")
	}
}

func TestWriteCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "subdir", "status.json")

	entry := cache.Entry{
		FetchedAt:          time.Now().UTC(),
		SessionUtilization: 50.0,
	}

	if err := cache.WriteToPath(p, entry); err != nil {
		t.Fatalf("Write with missing parent dir: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

// The daemon can now exit while a write is in flight, so a write must either
// land whole or leave the previous cache untouched.
func TestWriteToPathReplacesAtomically(t *testing.T) {
	p := tempCachePath(t)
	first := cache.Entry{FetchedAt: time.Now().UTC(), SessionUtilization: 11}
	if err := cache.WriteToPath(p, first); err != nil {
		t.Fatalf("first write: %v", err)
	}

	second := cache.Entry{FetchedAt: time.Now().UTC(), SessionUtilization: 22}
	if err := cache.WriteToPath(p, second); err != nil {
		t.Fatalf("second write: %v", err)
	}

	got, err := cache.ReadFromPath(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SessionUtilization != 22 {
		t.Errorf("SessionUtilization = %v, want 22", got.SessionUtilization)
	}
}

func TestWriteToPathLeavesNoTempFiles(t *testing.T) {
	p := tempCachePath(t)
	for i := 0; i < 5; i++ {
		if err := cache.WriteToPath(p, cache.Entry{FetchedAt: time.Now().UTC()}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != filepath.Base(p) {
			t.Errorf("stray file left behind: %s", e.Name())
		}
	}
}

func TestWriteToPathKeepsOwnerOnlyPermissions(t *testing.T) {
	p := tempCachePath(t)
	if err := cache.WriteToPath(p, cache.Entry{FetchedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("write: %v", err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// CreateTemp makes files 0600; the rename must not loosen that.
	if perm := fi.Mode().Perm(); perm != 0600 {
		t.Errorf("permissions = %o, want 600", perm)
	}
}
