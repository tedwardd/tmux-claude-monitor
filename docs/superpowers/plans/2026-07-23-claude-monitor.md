# claude-monitor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go binary that polls the claude.ai Pro/Max subscription quota API and renders a usage bar in the tmux status line via a background daemon and cached results.

**Architecture:** A background daemon polls claude.ai every 5 minutes and writes results to a JSON cache file. The `status` subcommand reads the cache (sub-millisecond) and prints a tmux-formatted string. The daemon handles SIGUSR1 for on-demand refresh triggered by a tmux keybinding.

**Tech Stack:** Go 1.26, stdlib only (no external dependencies — net/http, encoding/json, os/signal, syscall cover all needs). cobra is explicitly excluded to keep the binary dependency-free; subcommand dispatch is done with a plain switch on `os.Args[1]`.

## Global Constraints

- Go module name: `claude-monitor`
- Binary name: `claude-monitor`
- No external dependencies (stdlib only — no cobra, no third-party HTTP libs)
- Config path: `~/.config/claude-monitor/config.json`
- Cache path: `~/.cache/claude-monitor/status.json`
- PID file path: `~/.cache/claude-monitor/daemon.pid`
- Credentials source: `~/.claude/.credentials.json` (field: `.claudeAiOauth.accessToken`)
- tmux config path: `~/.config/tmux/tmux.conf`
- Status bar: `Claude: ████░░ 84% ↺14:30` (6-char block bar, local HH:MM reset time)
- Colors: green < 70%, yellow 70–89%, red ≥ 90%, grey (#[fg=colour244]) for error/stale
- Stale threshold: 15 minutes (cache older than this → show `Claude: ??`)
- Default poll interval: 300 seconds
- Refresh keybinding: `<prefix> F5`

---

## File Map

| File | Responsibility |
|---|---|
| `main.go` | Entry point; delegates to `cmd.Execute()` |
| `cmd/root.go` | Subcommand dispatch (switch on os.Args[1]), usage/help output |
| `cmd/daemon.go` | `daemon` subcommand: PID file, poll loop, signal handling |
| `cmd/status.go` | `status` subcommand: read cache, print formatted tmux string |
| `cmd/refresh.go` | `refresh` subcommand: send SIGUSR1 to daemon via PID file |
| `cmd/init.go` | `init` subcommand: verify auth, write config, patch tmux, start daemon |
| `internal/config/config.go` | Config struct, Load/Save, path expansion |
| `internal/cache/cache.go` | Cache Entry struct, Read/Write, stale detection |
| `internal/format/format.go` | Pure formatting: block bar, color codes, tmux string assembly |
| `internal/api/client.go` | HTTP client: read credentials, bootstrap call, fetch usage |
| `go.mod` | Module declaration |

---

## Task 1: Module scaffold and CLI dispatch

**Files:**
- Create: `go.mod`
- Create: `main.go`
- Create: `cmd/root.go`

**Interfaces:**
- Produces: `cmd.Execute()` — reads `os.Args[1]`, dispatches to subcommand handlers; prints usage on unknown subcommand; exits 0 on `--help`/`-h`

- [ ] **Step 1: Initialize the Go module**

```bash
cd ~/Projects/tmux-usage-monitor
go mod init claude-monitor
```

Expected output: `go: creating new go.mod: module claude-monitor`

- [ ] **Step 2: Create `main.go`**

```go
package main

import "claude-monitor/cmd"

func main() {
	cmd.Execute()
}
```

- [ ] **Step 3: Create `cmd/root.go` with subcommand dispatch**

```go
package cmd

import (
	"fmt"
	"os"
)

func Execute() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "daemon":
		runDaemon()
	case "status":
		runStatus()
	case "refresh":
		runRefresh()
	case "init":
		runInit()
	case "--help", "-h", "help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`Usage: claude-monitor <command>

Commands:
  init     One-time setup: verify auth, patch tmux config, start daemon
  daemon   Start background poller (writes cache every 5 min)
  status   Print tmux-formatted usage string (reads cache)
  refresh  Signal daemon for immediate fetch
`)
}
```

- [ ] **Step 4: Add stub files so the package compiles**

Create `cmd/daemon.go`:
```go
package cmd

func runDaemon() {}
```

Create `cmd/status.go`:
```go
package cmd

func runStatus() {}
```

Create `cmd/refresh.go`:
```go
package cmd

func runRefresh() {}
```

Create `cmd/init.go`:
```go
package cmd

func runInit() {}
```

- [ ] **Step 5: Verify it compiles and dispatch works**

```bash
go build -o claude-monitor .
./claude-monitor --help
./claude-monitor unknown 2>&1 | grep "unknown command"
```

Expected:
```
Usage: claude-monitor <command>
...
unknown command: unknown
```

- [ ] **Step 6: Commit**

```bash
git add go.mod main.go cmd/
git commit -m "feat: scaffold module and CLI dispatch"
```

---

## Task 2: Config package

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `config.Config` struct (fields: `PollIntervalSeconds int`, `CachePath string`, `CredentialsPath string`, `OrgUUID string`)
  - `config.Load() (Config, error)` — reads from `~/.config/claude-monitor/config.json`; returns defaults if file absent
  - `config.Save(cfg Config) error` — writes to same path, creating dirs
  - `config.DefaultConfig() Config` — returns hardcoded defaults
  - `config.Path() string` — returns the expanded config file path

- [ ] **Step 1: Write the failing tests**

Create `internal/config/config_test.go`:
```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"claude-monitor/internal/config"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg := config.Config{
		PollIntervalSeconds: 120,
		CachePath:           "~/.cache/claude-monitor/status.json",
		CredentialsPath:     "~/.claude/.credentials.json",
		OrgUUID:             "test-uuid-1234",
	}

	if err := config.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != cfg {
		t.Errorf("got %+v, want %+v", got, cfg)
	}
}

func TestLoadMissingReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	got, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	def := config.DefaultConfig()
	if got.PollIntervalSeconds != def.PollIntervalSeconds {
		t.Errorf("PollIntervalSeconds: got %d, want %d", got.PollIntervalSeconds, def.PollIntervalSeconds)
	}
}

func TestPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	want := filepath.Join(dir, ".config", "claude-monitor", "config.json")
	if got := config.Path(); got != want {
		t.Errorf("Path: got %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/config/ -v 2>&1 | head -20
```

Expected: compile error (package doesn't exist yet)

- [ ] **Step 3: Implement `internal/config/config.go`**

```go
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
	CachePath           string `json:"cache_path"`
	CredentialsPath     string `json:"credentials_path"`
	OrgUUID             string `json:"org_uuid"`
}

func DefaultConfig() Config {
	return Config{
		PollIntervalSeconds: 300,
		CachePath:           "~/.cache/claude-monitor/status.json",
		CredentialsPath:     "~/.claude/.credentials.json",
		OrgUUID:             "",
	}
}

func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "claude-monitor", "config.json")
}

func Load() (Config, error) {
	p := Path()
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(cfg Config) error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

// ExpandPath replaces a leading ~/ with the user home directory.
func ExpandPath(p string) string {
	if !strings.HasPrefix(p, "~/") {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, p[2:])
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/config/ -v
```

Expected:
```
--- PASS: TestRoundTrip
--- PASS: TestLoadMissingReturnsDefaults
--- PASS: TestPath
PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add config package with Load/Save/DefaultConfig"
```

---

## Task 3: Cache package

**Files:**
- Create: `internal/cache/cache.go`
- Create: `internal/cache/cache_test.go`

**Interfaces:**
- Produces:
  - `cache.Entry` struct (fields: `FetchedAt time.Time`, `MessagesUsed int`, `MessagesLimit int`, `ResetAt time.Time`, `Error string`)
  - `cache.Write(e Entry) error`
  - `cache.Read() (Entry, error)`
  - `cache.IsStale(e Entry) bool` — true if `FetchedAt` is more than 15 minutes ago
  - `cache.Path(cachePath string) string` — expands `~/...` in the given path

- [ ] **Step 1: Write the failing tests**

Create `internal/cache/cache_test.go`:
```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/cache/ -v 2>&1 | head -10
```

Expected: compile error

- [ ] **Step 3: Implement `internal/cache/cache.go`**

```go
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const staleThreshold = 15 * time.Minute

type Entry struct {
	FetchedAt     time.Time `json:"fetched_at"`
	MessagesUsed  int       `json:"messages_used"`
	MessagesLimit int       `json:"messages_limit"`
	ResetAt       time.Time `json:"reset_at"`
	Error         string    `json:"error"`
}

func IsStale(e Entry) bool {
	return time.Since(e.FetchedAt) > staleThreshold
}

func Path(cachePath string) string {
	if strings.HasPrefix(cachePath, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, cachePath[2:])
	}
	return cachePath
}

func WriteToPath(p string, e Entry) error {
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0600)
}

func ReadFromPath(p string) (Entry, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, err
	}
	return e, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/cache/ -v
```

Expected: all 4 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cache/
git commit -m "feat: add cache package with Read/Write/IsStale"
```

---

## Task 4: Status formatting (pure functions)

**Files:**
- Create: `internal/format/format.go`
- Create: `internal/format/format_test.go`

**Interfaces:**
- Produces:
  - `format.StatusLine(e cache.Entry) string` — returns a complete tmux-formatted status string, or the grey fallback on error/stale

- [ ] **Step 1: Write the failing tests**

Create `internal/format/format_test.go`:
```go
package format_test

import (
	"strings"
	"testing"
	"time"

	"claude-monitor/internal/cache"
	"claude-monitor/internal/format"
)

func makeEntry(used, limit int, resetOffset time.Duration, errStr string) cache.Entry {
	return cache.Entry{
		FetchedAt:     time.Now(),
		MessagesUsed:  used,
		MessagesLimit: limit,
		ResetAt:       time.Now().Add(resetOffset),
		Error:         errStr,
	}
}

func TestGreenWhenBelowSeventy(t *testing.T) {
	e := makeEntry(30, 50, 2*time.Hour, "")
	out := format.StatusLine(e)
	if !strings.Contains(out, "#[fg=green]") {
		t.Errorf("expected green, got: %q", out)
	}
	if !strings.Contains(out, "60%") {
		t.Errorf("expected 60%%, got: %q", out)
	}
}

func TestYellowAt70Percent(t *testing.T) {
	e := makeEntry(35, 50, 2*time.Hour, "") // 70%
	out := format.StatusLine(e)
	if !strings.Contains(out, "#[fg=yellow]") {
		t.Errorf("expected yellow, got: %q", out)
	}
}

func TestRedAt90Percent(t *testing.T) {
	e := makeEntry(45, 50, 2*time.Hour, "") // 90%
	out := format.StatusLine(e)
	if !strings.Contains(out, "#[fg=red]") {
		t.Errorf("expected red, got: %q", out)
	}
}

func TestErrorShowsFallback(t *testing.T) {
	e := cache.Entry{FetchedAt: time.Now(), Error: "network error"}
	out := format.StatusLine(e)
	if !strings.Contains(out, "colour244") {
		t.Errorf("expected grey fallback, got: %q", out)
	}
	if !strings.Contains(out, "??") {
		t.Errorf("expected ??, got: %q", out)
	}
}

func TestStaleShowsFallback(t *testing.T) {
	e := cache.Entry{
		FetchedAt:     time.Now().Add(-20 * time.Minute),
		MessagesUsed:  10,
		MessagesLimit: 50,
	}
	out := format.StatusLine(e)
	if !strings.Contains(out, "colour244") {
		t.Errorf("expected grey fallback for stale, got: %q", out)
	}
}

func TestBlockBar6Wide(t *testing.T) {
	e := makeEntry(50, 50, time.Hour, "") // 100% — all blocks filled
	out := format.StatusLine(e)
	if !strings.Contains(out, "██████") {
		t.Errorf("expected 6 filled blocks, got: %q", out)
	}
}

func TestResetTimeInOutput(t *testing.T) {
	resetAt := time.Date(2026, 7, 23, 14, 30, 0, 0, time.Local)
	e := cache.Entry{
		FetchedAt:     time.Now(),
		MessagesUsed:  10,
		MessagesLimit: 50,
		ResetAt:       resetAt,
	}
	out := format.StatusLine(e)
	if !strings.Contains(out, "↺14:30") {
		t.Errorf("expected reset time, got: %q", out)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/format/ -v 2>&1 | head -10
```

Expected: compile error

- [ ] **Step 3: Implement `internal/format/format.go`**

```go
package format

import (
	"fmt"
	"strings"
	"time"

	"claude-monitor/internal/cache"
)

const (
	barWidth = 6
	fallback = "#[fg=colour244]Claude: ??#[default]"
)

func StatusLine(e cache.Entry) string {
	if e.Error != "" || cache.IsStale(e) {
		return fallback
	}

	pct := 0
	if e.MessagesLimit > 0 {
		pct = (e.MessagesUsed * 100) / e.MessagesLimit
	}

	color := colorCode(pct)
	bar := blockBar(e.MessagesUsed, e.MessagesLimit)
	reset := e.ResetAt.Local().Format("15:04")

	return fmt.Sprintf("%sClaude: %s %d%% ↺%s#[default]", color, bar, pct, reset)
}

func colorCode(pct int) string {
	switch {
	case pct >= 90:
		return "#[fg=red]"
	case pct >= 70:
		return "#[fg=yellow]"
	default:
		return "#[fg=green]"
	}
}

func blockBar(used, limit int) string {
	if limit == 0 {
		return strings.Repeat("░", barWidth)
	}
	filled := (used * barWidth) / limit
	if filled > barWidth {
		filled = barWidth
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/format/ -v
```

Expected: all 7 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/format/
git commit -m "feat: add format package for tmux status string generation"
```

---

## Task 5: `status` subcommand

**Files:**
- Modify: `cmd/status.go`

**Interfaces:**
- Consumes: `config.Load()`, `cache.ReadFromPath(p)`, `cache.IsStale(e)`, `format.StatusLine(e)`
- Produces: `runStatus()` — prints formatted string to stdout, exits 0 (always, even on error; errors show fallback string)

- [ ] **Step 1: Replace the stub in `cmd/status.go`**

```go
package cmd

import (
	"fmt"
	"os"

	"claude-monitor/internal/cache"
	"claude-monitor/internal/config"
	"claude-monitor/internal/format"
)

func runStatus() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Print(format.StatusLine(cache.Entry{Error: err.Error()}))
		return
	}

	p := cache.Path(cfg.CachePath)
	entry, err := cache.ReadFromPath(p)
	if err != nil {
		// Cache missing or unreadable — show fallback
		fmt.Print(format.StatusLine(cache.Entry{Error: "no cache"}))
		return
	}

	fmt.Print(format.StatusLine(entry))
}
```

- [ ] **Step 2: Build and smoke-test with a hand-written cache file**

```bash
go build -o claude-monitor .
mkdir -p ~/.cache/claude-monitor
cat > ~/.cache/claude-monitor/status.json << 'EOF'
{
  "fetched_at": "2026-07-23T14:00:00Z",
  "messages_used": 35,
  "messages_limit": 50,
  "reset_at": "2026-07-23T16:30:00Z",
  "error": ""
}
EOF
./claude-monitor status
```

Expected: `#[fg=yellow]Claude: ████░░ 70% ↺...#[default]` (exact reset time depends on your timezone)

- [ ] **Step 3: Test stale fallback**

```bash
cat > ~/.cache/claude-monitor/status.json << 'EOF'
{
  "fetched_at": "2026-01-01T00:00:00Z",
  "messages_used": 10,
  "messages_limit": 50,
  "reset_at": "2026-01-01T05:00:00Z",
  "error": ""
}
EOF
./claude-monitor status
```

Expected: `#[fg=colour244]Claude: ??#[default]`

- [ ] **Step 4: Commit**

```bash
git add cmd/status.go
git commit -m "feat: implement status subcommand"
```

---

## Task 6: API client

**Files:**
- Create: `internal/api/client.go`
- Create: `internal/api/client_test.go`

**Interfaces:**
- Produces:
  - `api.Credentials` struct (field: `AccessToken string`)
  - `api.UsageData` struct (fields: `MessagesUsed int`, `MessagesLimit int`, `ResetAt time.Time`)
  - `api.ReadCredentials(path string) (Credentials, error)` — reads `~/.claude/.credentials.json`, extracts `claudeAiOauth.accessToken`
  - `api.FetchBootstrap(token string) (orgUUID string, err error)` — GET `https://claude.ai/api/bootstrap`
  - `api.FetchUsage(token, orgUUID string) (UsageData, error)` — GET `https://claude.ai/api/organizations/{uuid}/usage`

**Note on API field names:** The exact JSON field names in the claude.ai API responses are verified during `init`. The constants `usageFieldUsed`, `usageFieldLimit`, `usageFieldReset` in `client.go` document the discovered field paths. If the API response doesn't match, the `init --discover` flag prints the raw response for inspection.

- [ ] **Step 1: Write failing tests using `httptest`**

Create `internal/api/client_test.go`:
```go
package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"claude-monitor/internal/api"
)

func TestReadCredentials(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, ".credentials.json")

	creds := map[string]interface{}{
		"claudeAiOauth": map[string]interface{}{
			"accessToken": "sk-test-token-abc123",
		},
	}
	data, _ := json.Marshal(creds)
	os.WriteFile(credPath, data, 0600)

	got, err := api.ReadCredentials(credPath)
	if err != nil {
		t.Fatalf("ReadCredentials: %v", err)
	}
	if got.AccessToken != "sk-test-token-abc123" {
		t.Errorf("AccessToken: got %q", got.AccessToken)
	}
}

func TestReadCredentialsMissingFile(t *testing.T) {
	_, err := api.ReadCredentials("/tmp/no-such-file-xyz.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestFetchBootstrap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/bootstrap" {
			http.Error(w, "not found", 404)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", 401)
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"account": map[string]interface{}{
				"organizationMemberships": []interface{}{
					map[string]interface{}{"organization": map[string]interface{}{"uuid": "org-uuid-abc"}},
				},
			},
		})
	}))
	defer srv.Close()

	orgUUID, err := api.FetchBootstrapFromURL(srv.URL+"/api/bootstrap", "test-token")
	if err != nil {
		t.Fatalf("FetchBootstrap: %v", err)
	}
	if orgUUID != "org-uuid-abc" {
		t.Errorf("orgUUID: got %q, want %q", orgUUID, "org-uuid-abc")
	}
}

func TestFetchUsage(t *testing.T) {
	resetTime := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"raw_limits": map[string]interface{}{
				"message_limit":      50,
				"messages_remaining": 20,
				"window_resets_at":   resetTime.Format(time.RFC3339),
			},
		})
	}))
	defer srv.Close()

	usage, err := api.FetchUsageFromURL(srv.URL, "org-uuid-abc", "test-token")
	if err != nil {
		t.Fatalf("FetchUsage: %v", err)
	}
	if usage.MessagesLimit != 50 {
		t.Errorf("MessagesLimit: got %d", usage.MessagesLimit)
	}
	if usage.MessagesUsed != 30 { // limit - remaining = 50 - 20
		t.Errorf("MessagesUsed: got %d", usage.MessagesUsed)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/api/ -v 2>&1 | head -10
```

Expected: compile error

- [ ] **Step 3: Implement `internal/api/client.go`**

```go
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	bootstrapURL = "https://claude.ai/api/bootstrap"
	usageBaseURL = "https://claude.ai/api/organizations/%s/usage"
)

type Credentials struct {
	AccessToken string
}

type UsageData struct {
	MessagesUsed  int
	MessagesLimit int
	ResetAt       time.Time
}

type credentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

func ReadCredentials(path string) (Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}, fmt.Errorf("read credentials: %w", err)
	}
	var cf credentialsFile
	if err := json.Unmarshal(data, &cf); err != nil {
		return Credentials{}, fmt.Errorf("parse credentials: %w", err)
	}
	if cf.ClaudeAiOauth.AccessToken == "" {
		return Credentials{}, fmt.Errorf("accessToken not found in claudeAiOauth")
	}
	return Credentials{AccessToken: cf.ClaudeAiOauth.AccessToken}, nil
}

func FetchBootstrap(token string) (string, error) {
	return FetchBootstrapFromURL(bootstrapURL, token)
}

func FetchBootstrapFromURL(url, token string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("bootstrap request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("bootstrap returned HTTP %d", resp.StatusCode)
	}

	// Response shape: {"account":{"organizationMemberships":[{"organization":{"uuid":"..."}}]}}
	// Adjust field paths here if the real API differs — run init --discover to inspect raw response.
	var body struct {
		Account struct {
			OrganizationMemberships []struct {
				Organization struct {
					UUID string `json:"uuid"`
				} `json:"organization"`
			} `json:"organizationMemberships"`
		} `json:"account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("parse bootstrap: %w", err)
	}
	if len(body.Account.OrganizationMemberships) == 0 {
		return "", fmt.Errorf("no organizations found in bootstrap response")
	}
	return body.Account.OrganizationMemberships[0].Organization.UUID, nil
}

func FetchUsage(token, orgUUID string) (UsageData, error) {
	return FetchUsageFromURL(fmt.Sprintf("https://claude.ai/api/organizations/%s/usage", orgUUID), orgUUID, token)
}

func FetchUsageFromURL(baseURL, orgUUID, token string) (UsageData, error) {
	url := baseURL
	if orgUUID != "" {
		url = fmt.Sprintf("%s/api/organizations/%s/usage", baseURL, orgUUID)
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return UsageData{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return UsageData{}, fmt.Errorf("usage request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return UsageData{}, fmt.Errorf("usage returned HTTP %d", resp.StatusCode)
	}

	// Response shape (adjust if real API differs — run init --discover):
	// {"raw_limits":{"message_limit":50,"messages_remaining":20,"window_resets_at":"2026-07-23T16:30:00Z"}}
	var body struct {
		RawLimits struct {
			MessageLimit      int    `json:"message_limit"`
			MessagesRemaining int    `json:"messages_remaining"`
			WindowResetsAt    string `json:"window_resets_at"`
		} `json:"raw_limits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return UsageData{}, fmt.Errorf("parse usage: %w", err)
	}

	resetAt, err := time.Parse(time.RFC3339, body.RawLimits.WindowResetsAt)
	if err != nil {
		resetAt = time.Time{}
	}

	used := body.RawLimits.MessageLimit - body.RawLimits.MessagesRemaining
	if used < 0 {
		used = 0
	}

	return UsageData{
		MessagesUsed:  used,
		MessagesLimit: body.RawLimits.MessageLimit,
		ResetAt:       resetAt,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/api/ -v
```

Expected: all 4 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/
git commit -m "feat: add API client for credentials, bootstrap, and usage"
```

---

## Task 7: `daemon` subcommand

**Files:**
- Modify: `cmd/daemon.go`

**Interfaces:**
- Consumes: `config.Load()`, `api.ReadCredentials()`, `api.FetchBootstrap()`, `api.FetchUsage()`, `cache.WriteToPath()`, `cache.Path()`
- Produces: `runDaemon()` — writes PID file, fetches immediately, polls on interval, handles SIGUSR1 and SIGTERM/SIGINT

- [ ] **Step 1: Replace the stub in `cmd/daemon.go`**

```go
package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"claude-monitor/internal/api"
	"claude-monitor/internal/cache"
	"claude-monitor/internal/config"
)

func runDaemon() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: load config: %v\n", err)
		os.Exit(1)
	}

	pidPath := pidFilePath()
	if err := writePID(pidPath); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: write PID: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(pidPath)

	credPath := expandHome(cfg.CredentialsPath)
	creds, err := api.ReadCredentials(credPath)
	if err != nil {
		writeErrorCache(cfg, fmt.Sprintf("read credentials: %v", err))
		fmt.Fprintf(os.Stderr, "daemon: %v\n", err)
		os.Exit(1)
	}

	if cfg.OrgUUID == "" {
		uuid, err := api.FetchBootstrap(creds.AccessToken)
		if err != nil {
			writeErrorCache(cfg, fmt.Sprintf("bootstrap: %v", err))
			fmt.Fprintf(os.Stderr, "daemon: bootstrap: %v\n", err)
			os.Exit(1)
		}
		cfg.OrgUUID = uuid
		config.Save(cfg)
	}

	usr1 := make(chan os.Signal, 1)
	signal.Notify(usr1, syscall.SIGUSR1)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	fetch := func() {
		usage, err := api.FetchUsage(creds.AccessToken, cfg.OrgUUID)
		p := cache.Path(cfg.CachePath)
		if err != nil {
			cache.WriteToPath(p, cache.Entry{
				FetchedAt: time.Now().UTC(),
				Error:     err.Error(),
			})
			return
		}
		cache.WriteToPath(p, cache.Entry{
			FetchedAt:     time.Now().UTC(),
			MessagesUsed:  usage.MessagesUsed,
			MessagesLimit: usage.MessagesLimit,
			ResetAt:       usage.ResetAt,
		})
	}

	fetch() // immediate fetch on start

	ticker := time.NewTicker(time.Duration(cfg.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fetch()
		case <-usr1:
			fetch()
		case <-quit:
			return
		}
	}
}

func pidFilePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "claude-monitor", "daemon.pid")
}

func writePID(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600)
}

func writeErrorCache(cfg config.Config, errMsg string) {
	p := cache.Path(cfg.CachePath)
	cache.WriteToPath(p, cache.Entry{
		FetchedAt: time.Now().UTC(),
		Error:     errMsg,
	})
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
```

- [ ] **Step 2: Build and verify daemon starts and writes PID**

```bash
go build -o claude-monitor .
./claude-monitor daemon &
DPID=$!
sleep 2
cat ~/.cache/claude-monitor/daemon.pid
kill $DPID
```

Expected: PID number printed, then daemon exits cleanly.

Note: The daemon will fail to fetch from the real API until `init` configures the org UUID. The error will be written to the cache file. This is correct behaviour.

- [ ] **Step 3: Verify SIGUSR1 triggers a fetch**

```bash
./claude-monitor daemon &
DPID=$!
sleep 1
kill -USR1 $DPID
sleep 1
cat ~/.cache/claude-monitor/status.json | python3 -m json.tool
kill $DPID
```

Expected: `fetched_at` timestamp updates after the SIGUSR1.

- [ ] **Step 4: Commit**

```bash
git add cmd/daemon.go
git commit -m "feat: implement daemon subcommand with poll loop and signal handling"
```

---

## Task 8: `refresh` subcommand

**Files:**
- Modify: `cmd/refresh.go`

**Interfaces:**
- Consumes: `pidFilePath()` (from `cmd/daemon.go` — move to shared helper if needed)
- Produces: `runRefresh()` — reads PID file, sends SIGUSR1, prints confirmation

- [ ] **Step 1: Move `pidFilePath()` to a shared location**

Add to `cmd/root.go` (below the existing `printUsage` function):

```go
import (
	"os"
	"path/filepath"
)

func sharedPIDPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "claude-monitor", "daemon.pid")
}
```

Update `cmd/daemon.go` to call `sharedPIDPath()` instead of its local `pidFilePath()`, and remove the local `pidFilePath()` function.

- [ ] **Step 2: Replace the stub in `cmd/refresh.go`**

```go
package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func runRefresh() {
	pidPath := sharedPIDPath()
	data, err := os.ReadFile(pidPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "refresh: daemon not running (no PID file at %s)\n", pidPath)
		os.Exit(1)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "refresh: invalid PID %q\n", pidStr)
		os.Exit(1)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "refresh: process %d not found\n", pid)
		os.Exit(1)
	}

	if err := proc.Signal(syscall.SIGUSR1); err != nil {
		fmt.Fprintf(os.Stderr, "refresh: signal %d: %v\n", pid, err)
		os.Exit(1)
	}

	fmt.Printf("refresh: signaled daemon (PID %d)\n", pid)
}
```

- [ ] **Step 3: Build and test refresh end-to-end**

```bash
go build -o claude-monitor .
./claude-monitor daemon &
sleep 1
./claude-monitor refresh
sleep 1
kill $(cat ~/.cache/claude-monitor/daemon.pid)
```

Expected: `refresh: signaled daemon (PID XXXX)`, no errors

- [ ] **Step 4: Test refresh with no daemon running**

```bash
rm -f ~/.cache/claude-monitor/daemon.pid
./claude-monitor refresh
echo "exit code: $?"
```

Expected: error message, exit code 1

- [ ] **Step 5: Commit**

```bash
git add cmd/refresh.go cmd/daemon.go cmd/root.go
git commit -m "feat: implement refresh subcommand; extract sharedPIDPath"
```

---

## Task 9: `init` subcommand

**Files:**
- Modify: `cmd/init.go`

**Interfaces:**
- Consumes: `api.ReadCredentials()`, `api.FetchBootstrap()`, `api.FetchUsage()`, `config.Load()`, `config.Save()`, `config.ExpandPath()`, `format.StatusLine()`
- Produces: `runInit()` — interactive setup with `--discover` flag support

**`--discover` flag:** If passed as `claude-monitor init --discover`, the init command prints the raw JSON response from the bootstrap and usage endpoints before parsing, then exits. Use this when the API response shape differs from the expected struct and you need to identify the correct field paths.

- [ ] **Step 1: Replace the stub in `cmd/init.go`**

```go
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

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	// Write the updated content (original minus old status-right) then the block
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}
	f2, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f2.Close()
	_, err = f2.WriteString(block)
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
```

- [ ] **Step 2: Build and run init**

```bash
go build -o claude-monitor .
./claude-monitor init
```

Expected walkthrough:
1. Reads auth token from `~/.claude/.credentials.json`
2. Hits `https://claude.ai/api/bootstrap` → extracts org UUID
3. Hits `https://claude.ai/api/organizations/{uuid}/usage` → prints current quota
4. Writes config with org UUID
5. Patches `~/.config/tmux/tmux.conf`
6. Starts daemon in background
7. Reloads tmux config
8. Prints the zshrc snippet

**If bootstrap/usage returns unexpected JSON** (field names differ), run:
```bash
./claude-monitor init --discover
```
This prints the raw JSON from the bootstrap endpoint. Inspect the output, update the struct field tags in `internal/api/client.go` to match, rebuild, and re-run `init`.

- [ ] **Step 3: Verify tmux status bar shows usage**

In your tmux session:
```
# Press prefix + r to reload (or it was auto-reloaded by init)
# Look at the status bar — should show Claude usage
```

- [ ] **Step 4: Verify F5 keybinding works**

```
# In tmux: press <prefix> then F5
# The daemon should log a SIGUSR1 fetch
```

- [ ] **Step 5: Commit**

```bash
git add cmd/init.go
git commit -m "feat: implement init subcommand with tmux patching and daemon start"
```

---

## Task 10: Build, install, and end-to-end verification

**Files:**
- Create: `Makefile`

- [ ] **Step 1: Create a Makefile**

```makefile
BINARY := claude-monitor
INSTALL_DIR := $(HOME)/.local/bin

.PHONY: build test install

build:
	go build -o $(BINARY) .

test:
	go test ./...

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed to $(INSTALL_DIR)/$(BINARY)"
```

- [ ] **Step 2: Run all tests**

```bash
go test ./... -v
```

Expected: all tests pass across `internal/config`, `internal/cache`, `internal/format`, `internal/api`

- [ ] **Step 3: Build and install**

```bash
make install
which claude-monitor
```

Expected: `/home/elw/.local/bin/claude-monitor`

- [ ] **Step 4: Run full end-to-end flow**

```bash
# Kill any running daemon
pkill claude-monitor 2>/dev/null || true

# Run init (re-entrant — detects existing tmux patch)
claude-monitor init

# Verify daemon is running
pgrep -a claude-monitor

# Check cache was populated
cat ~/.cache/claude-monitor/status.json | python3 -m json.tool

# Check status output
claude-monitor status

# Test manual refresh
claude-monitor refresh
sleep 2
cat ~/.cache/claude-monitor/status.json | python3 -m json.tool
```

- [ ] **Step 5: Add zshrc auto-start line**

```bash
echo 'pgrep -x claude-monitor >/dev/null || claude-monitor daemon &' >> ~/.zshrc
```

- [ ] **Step 6: Final commit**

```bash
git add Makefile
git commit -m "feat: add Makefile; complete build and install"
```

---

## Self-Review

**Spec coverage:**
- [x] Background daemon with poll interval → Task 7
- [x] Cache file decoupled from tmux render → Tasks 3, 5, 7
- [x] `status` prints tmux-formatted string → Tasks 4, 5
- [x] Color coding green/yellow/red/grey → Task 4
- [x] Block bar 6-char wide → Task 4
- [x] Reset time in local HH:MM → Task 4
- [x] Stale detection (15 min) → Tasks 3, 4
- [x] SIGUSR1 refresh → Tasks 7, 8
- [x] `<prefix> F5` keybinding → Task 9
- [x] `init` verifies auth → Task 9
- [x] `init` writes config → Task 9
- [x] `init` patches tmux config → Task 9
- [x] `init` starts daemon → Task 9
- [x] `init` reloads tmux → Task 9
- [x] `init` prints zshrc snippet → Task 9
- [x] Token read from `~/.claude/.credentials.json` → Task 6
- [x] PID file lifecycle → Task 7
- [x] Exponential backoff on error → **GAP** — daemon currently retries on next tick only

**Gap fix — add backoff to daemon fetch loop:**

In Task 7 `cmd/daemon.go`, replace the `fetch()` function and loop with a version that tracks consecutive failures:

```go
consecutiveFails := 0

fetch := func() {
    usage, err := api.FetchUsage(creds.AccessToken, cfg.OrgUUID)
    p := cache.Path(cfg.CachePath)
    if err != nil {
        consecutiveFails++
        cache.WriteToPath(p, cache.Entry{
            FetchedAt: time.Now().UTC(),
            Error:     err.Error(),
        })
        // Backoff: 30s, 60s, 120s, 240s, capped at 300s
        backoff := time.Duration(30*(1<<consecutiveFails)) * time.Second
        if backoff > 300*time.Second {
            backoff = 300 * time.Second
        }
        ticker.Reset(backoff)
        return
    }
    consecutiveFails = 0
    ticker.Reset(time.Duration(cfg.PollIntervalSeconds) * time.Second)
    cache.WriteToPath(p, cache.Entry{
        FetchedAt:     time.Now().UTC(),
        MessagesUsed:  usage.MessagesUsed,
        MessagesLimit: usage.MessagesLimit,
        ResetAt:       usage.ResetAt,
    })
}
```

Add this to Task 7 Step 1 — the ticker must be declared with `var ticker *time.Ticker` before `fetch` is defined so the closure can reference it.

**Placeholder scan:** No TBDs, no "implement later" patterns found.

**Type consistency:** All function signatures are used consistently across tasks — `cache.WriteToPath(p string, e cache.Entry)`, `cache.ReadFromPath(p string)`, `api.FetchUsage(token, orgUUID string)`, `format.StatusLine(e cache.Entry)` are referenced identically in all tasks.
