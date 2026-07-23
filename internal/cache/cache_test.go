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
	reset := now.Add(2 * time.Hour)

	entry := cache.Entry{
		FetchedAt:     now,
		MessagesUsed:  30,
		MessagesLimit: 50,
		ResetAt:       reset,
		Error:         "",
	}

	if err := cache.WriteToPath(p, entry); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := cache.ReadFromPath(p)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if got.MessagesUsed != 30 || got.MessagesLimit != 50 {
		t.Errorf("got %+v", got)
	}
	if !got.FetchedAt.Equal(now) {
		t.Errorf("FetchedAt: got %v, want %v", got.FetchedAt, now)
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
		FetchedAt:     time.Now().UTC(),
		MessagesUsed:  1,
		MessagesLimit: 50,
	}

	if err := cache.WriteToPath(p, entry); err != nil {
		t.Fatalf("Write with missing parent dir: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("file not created: %v", err)
	}
}
