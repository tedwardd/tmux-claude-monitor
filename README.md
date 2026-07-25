# tmux-claude-monitor

A tmux status bar daemon that displays your [Claude Pro](https://claude.ai) quota usage in real time.

## What it shows

```
S:42% W:23% +$8.50
```

- **S:** — current session utilization
- **W:** — weekly utilization
- **+$8.50** — extra (overage) usage, shown only when active

## Prerequisites

- Linux (uses systemd for the background service and D-Bus for sleep/wake detection)
- tmux
- An active Claude Pro subscription with [Claude Code](https://claude.ai/code) installed and authenticated

## Installation

Download the latest binary for your architecture from the [Releases](https://github.com/tedwardd/tmux-claude-monitor/releases) page and place it on your `$PATH`:

```sh
# Linux amd64
curl -L https://github.com/tedwardd/tmux-claude-monitor/releases/latest/download/claude-monitor_linux_amd64.tar.gz \
  | tar xz -C ~/.local/bin/
```

Then run one-time setup:

```sh
claude-monitor init
```

This will:

1. Verify your Claude credentials
2. Patch your tmux config to display the status bar widget
3. Install and start a systemd user service that keeps the cache fresh

## Commands

| Command | Description |
|---|---|
| `claude-monitor init` | One-time setup |
| `claude-monitor status` | Print the current status string (called by tmux) |
| `claude-monitor refresh` | Signal the daemon for an immediate fetch |
| `claude-monitor daemon` | Run the background poller (managed by systemd) |

**Manual refresh keybinding:** `<prefix> F5` — added to your tmux config by `init`.

## How it works

A systemd user service polls the Claude API every 5 minutes and writes a cache file. The tmux status bar reads from that cache via `claude-monitor status`. On wake from sleep, the daemon listens for the D-Bus `PrepareForSleep` signal from `org.freedesktop.login1.Manager` and refreshes within ~5 seconds. On systems without D-Bus, it falls back to monotonic clock drift detection.

## Configuration

Config lives at `~/.config/claude-monitor/config.json` and is created by `init`:

```json
{
  "poll_interval_seconds": 300,
  "cache_path": "~/.cache/claude-monitor/status.json",
  "credentials_path": "~/.claude/.credentials.json"
}
```

## Viewing logs

The daemon runs as a systemd user service. Logs are available via journald:

```sh
journalctl --user -u claude-monitor -f
```

## Platform support

Currently Linux only. macOS support (tmux without systemd/D-Bus) is planned.
