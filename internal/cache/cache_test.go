package cache_test

import (
	"errors"
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

// A transient failure must leave the previous reading in place, since the status
// line tolerates a gap up to the staleness threshold.
func TestRecordFailureKeepsLastReading(t *testing.T) {
	p := tempCachePath(t)
	good := cache.Entry{
		FetchedAt:          time.Now().UTC(),
		SessionUtilization: 42,
		SessionResetsAt:    time.Now().Add(time.Hour).UTC(),
	}
	if err := cache.WriteToPath(p, good); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := cache.RecordFailure(p, errors.New("usage rate-limited (HTTP 429)")); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}

	got, err := cache.ReadFromPath(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.SessionUtilization != 42 {
		t.Errorf("reading discarded: SessionUtilization = %v, want 42", got.SessionUtilization)
	}
	if !got.FetchedAt.Equal(good.FetchedAt) {
		t.Error("FetchedAt moved; it must keep marking the last success so staleness stays meaningful")
	}
	if got.Error != "" {
		t.Errorf("Error must stay empty so the reading still renders, got %q", got.Error)
	}
	if got.LastError == "" {
		t.Error("LastError not recorded")
	}
	if got.LastErrorAt.IsZero() {
		t.Error("LastErrorAt not recorded")
	}
}

// With nothing worth keeping there is no reading to protect, so the failure is
// recorded as hard and the bar correctly blanks.
func TestRecordFailureWithNoPriorReading(t *testing.T) {
	p := tempCachePath(t)
	if err := cache.RecordFailure(p, errors.New("read credentials: nope")); err != nil {
		t.Fatalf("RecordFailure: %v", err)
	}
	got, err := cache.ReadFromPath(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Error == "" {
		t.Error("expected a hard Error when there was no prior reading")
	}
	if got.HasReading() {
		t.Error("HasReading must be false for an error-only entry")
	}
}

// A success has to clear the previous failure, or the note would linger forever.
func TestSuccessClearsPriorFailure(t *testing.T) {
	p := tempCachePath(t)
	cache.WriteToPath(p, cache.Entry{FetchedAt: time.Now().UTC(), SessionUtilization: 10})
	cache.RecordFailure(p, errors.New("boom"))
	cache.WriteToPath(p, cache.Entry{FetchedAt: time.Now().UTC(), SessionUtilization: 20})

	got, _ := cache.ReadFromPath(p)
	if got.LastError != "" {
		t.Errorf("LastError survived a success: %q", got.LastError)
	}
}
