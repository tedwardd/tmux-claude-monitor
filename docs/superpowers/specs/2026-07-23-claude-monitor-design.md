# claude-monitor Design Spec

**Date:** 2026-07-23  
**Status:** Approved

## Overview

A Go binary that polls the claude.ai internal API for Claude Pro/Max subscription quota usage and displays it in the tmux status bar. A background daemon decouples API calls from tmux rendering; a cache file ensures the status subcommand is always sub-millisecond.

---

## Architecture

Single binary: `claude-monitor`

### Subcommands

| Subcommand | Role |
|---|---|
| `init` | One-time setup: verifies auth, writes config, patches tmux, starts daemon |
| `daemon` | Long-running poller; writes cache; handles SIGUSR1 for immediate refresh |
| `status` | Reads cache, prints tmux-formatted string; called by tmux on every render |
| `refresh` | Sends SIGUSR1 to daemon PID for immediate fetch |

### Data Flow

```
claude.ai API
      │  (every 5 min, or on SIGUSR1)
      ▼
  [daemon]  ──writes──▶  ~/.cache/claude-monitor/status.json
                                    │
                         (sub-ms read on every tmux render)
                                    ▼
                              [status]  ──▶  tmux status bar
```

The daemon and `status` are fully decoupled. tmux calls `status` on every `status-interval` tick — no network call ever blocks a tmux render.

---

## Auth & API

**Token source:** `~/.claude/.credentials.json` (written and maintained by Claude Code's OAuth flow). The daemon reads the `accessToken` field at startup and re-reads the file on token expiry — no manual token management required.

**Bootstrap call:** On first run (or if org UUID is not cached), the daemon calls:
```
GET https://claude.ai/api/bootstrap
Authorization: Bearer <accessToken>
```
to retrieve the organization UUID, which is stored in the config file.

**Usage endpoint:**
```
GET https://claude.ai/api/organizations/{org_uuid}/usage
Authorization: Bearer <accessToken>
```

Response fields consumed:
- Messages used in the current rolling window
- Messages limit for the window
- Window reset timestamp (UTC)

**Error handling:**
- Network failure, HTTP error, or token expiry: daemon writes an error marker to cache; `status` outputs `Claude: ??` in grey
- Exponential backoff on failure, capped at 5 minutes
- Token expiry: re-read credentials file (Claude Code refreshes it in the background); retry after brief delay

---

## Cache Format

**Path:** `~/.cache/claude-monitor/status.json`

```json
{
  "fetched_at": "2026-07-23T14:30:00Z",
  "messages_used": 42,
  "messages_limit": 50,
  "reset_at": "2026-07-23T16:30:00Z",
  "error": ""
}
```

Cache is considered stale if `fetched_at` is older than 15 minutes (daemon likely died). Stale cache triggers the grey fallback output.

---

## Status Output

The `status` subcommand prints a tmux-formatted string. Color is based on consumption level:

| Consumption | Color | Example |
|---|---|---|
| < 70% used | green | `#[fg=green]Claude: ███░░░ 58% ↺14:30#[default]` |
| 70–89% used | yellow | `#[fg=yellow]Claude: ████░░ 84% ↺14:30#[default]` |
| ≥ 90% used | red | `#[fg=red]Claude: █████░ 92% ↺14:30#[default]` |
| Error / stale | grey | `#[fg=colour244]Claude: ??#[default]` |

- Block bar is 6 characters wide (filled/empty blocks proportional to usage)
- Reset time is shown in local time (HH:MM)
- Percentage is `messages_used / messages_limit * 100`, rounded to integer

---

## Configuration

**Path:** `~/.config/claude-monitor/config.json`

```json
{
  "poll_interval_seconds": 300,
  "cache_path": "~/.cache/claude-monitor/status.json",
  "credentials_path": "~/.claude/.credentials.json",
  "org_uuid": ""
}
```

`org_uuid` is populated by `init` after the bootstrap call and persisted so subsequent daemon starts skip the bootstrap call.

---

## Daemon Lifecycle

- PID file: `~/.cache/claude-monitor/daemon.pid`
- On start: immediate fetch, then poll every `poll_interval_seconds`
- On `SIGUSR1`: immediate fetch (triggered by `refresh` subcommand)
- On `SIGTERM`/`SIGINT`: clean shutdown, remove PID file

---

## `init` Subcommand Flow

Runs interactively; each step is logged to stdout:

1. **Verify auth** — read `~/.claude/.credentials.json`, call bootstrap endpoint, print current quota as confirmation
2. **Write config** — create `~/.config/claude-monitor/config.json` with org UUID and defaults; skip if already present (idempotent)
3. **Patch tmux config** — append to `~/.config/tmux/tmux.conf`:
   - Prepend `#(claude-monitor status)` to existing `status-right` value
   - Add `bind-key F5 run-shell 'claude-monitor refresh'`
   - Skip if block already present (idempotent re-runs)
4. **Start daemon** — fork `claude-monitor daemon &` into background; confirm PID written
5. **Reload tmux** — run `tmux source ~/.config/tmux/tmux.conf` so changes take effect immediately
6. **Print shell snippet** — output line to add to `~/.zshrc` for daemon auto-start on login:
   ```sh
   pgrep -x claude-monitor >/dev/null || claude-monitor daemon &
   ```

---

## tmux Integration

Lines appended to `~/.config/tmux/tmux.conf` by `init`:

```tmux
# claude-monitor begin
set -g status-right '<existing content> #(claude-monitor status)'
bind-key F5 run-shell 'claude-monitor refresh'
# claude-monitor end
```

Manual refresh keybinding: `<prefix> F5`

---

## File Layout

```
~/Projects/tmux-usage-monitor/
├── main.go
├── cmd/
│   ├── daemon.go
│   ├── init.go
│   ├── refresh.go
│   └── status.go
├── internal/
│   ├── api/         # claude.ai HTTP client
│   ├── cache/       # read/write status.json
│   └── config/      # read/write config.json
└── go.mod
```

---

## Non-Goals

- No browser-based OAuth flow (relies on Claude Code's existing token)
- No support for API key auth (Pro/Max subscription only)
- No historical usage tracking or graphing
- No TUI or interactive display beyond the status bar string
