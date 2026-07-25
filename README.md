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

This will verify your credentials, write a config file, patch your tmux config, and install a systemd user service. See [Manual setup](#manual-setup) below if you prefer to do any of these steps yourself.

## Arch Linux

Install via your AUR helper:

```sh
# Pre-built binary (faster install, no Go toolchain needed)
paru -S tmux-claude-monitor-bin

# Build from source
paru -S tmux-claude-monitor
```

After install, enable the daemon:

```sh
systemctl --user enable --now claude-monitor
```

Then add to `~/.config/tmux/tmux.conf`:

```
set -g status-right-length 200
set -g status-right '#(claude-monitor status) | %H:%M'
```

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

### Display options

Add a `display` array to control which components appear in the status bar. Omitting the key (or leaving it empty) shows everything.

Valid values: `"bar"`, `"session"`, `"reset"`, `"extra"`

```json
{
  "poll_interval_seconds": 300,
  "cache_path": "~/.cache/claude-monitor/status.json",
  "credentials_path": "~/.claude/.credentials.json",
  "display": ["bar", "session"]
}
```

The example above shows only the block bar and session percentage, hiding the reset time and extra usage.

## Viewing logs

The daemon runs as a systemd user service. Logs are available via journald:

```sh
journalctl --user -u claude-monitor -f
```

## Manual setup

`claude-monitor init` performs five steps in order. You can skip any step and implement it yourself — they are fully independent.

### Step 1 — Credentials

The daemon reads a Claude Code OAuth token from `~/.claude/.credentials.json` (the default path, configurable). This file is created by Claude Code when you log in. It looks like:

```json
{
  "claudeAiOauth": {
    "accessToken": "...",
    "refreshToken": "...",
    "expiresAt": 1234567890000
  }
}
```

`init` only reads this file — it never modifies it. If the file is missing or the token is expired, run `claude logout && claude login` to refresh it.

### Step 2 — Config file

`init` writes the config to `~/.config/claude-monitor/config.json`. You can create it manually:

```sh
mkdir -p ~/.config/claude-monitor
cat > ~/.config/claude-monitor/config.json <<'EOF'
{
  "poll_interval_seconds": 300,
  "cache_path": "~/.cache/claude-monitor/status.json",
  "credentials_path": "~/.claude/.credentials.json"
}
EOF
```

All paths support `~/` expansion. The config is loaded by both the daemon and the `status` subcommand.

### Step 3 — tmux config

`init` appends the following block to `~/.config/tmux/tmux.conf`, preserving any existing `status-right` content by prepending it:

```
# claude-monitor begin
set -g status-right-length 200
set -g status-right '<your existing status-right> #(  claude-monitor status)'
bind-key F5 run-shell 'claude-monitor refresh'
# claude-monitor end
```

To add this manually, append it to your tmux config (adjusting `status-right` to fit your existing setup), then reload:

```sh
tmux source ~/.config/tmux/tmux.conf
```

If you use a different tmux config path, just add the `status-right` and `bind-key` lines wherever appropriate. The `#(  claude-monitor status)` call runs every time tmux refreshes the status bar and reads from the local cache — it does not hit the network.

### Step 4 — systemd service

`init` installs a user service at `~/.config/systemd/user/claude-monitor.service`:

```ini
[Unit]
Description=Claude usage monitor
After=network.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
ExecStart=/path/to/claude-monitor daemon
Restart=on-failure
RestartSec=30

[Install]
WantedBy=default.target
```

To install it manually (replace the `ExecStart` path with the output of `which claude-monitor`):

```sh
mkdir -p ~/.config/systemd/user
# write the unit file as above, then:
systemctl --user daemon-reload
systemctl --user enable --now claude-monitor
```

The service restarts automatically on failure (up to 5 times per 5 minutes) and starts on login via `default.target`.

### Running without systemd

If you don't use systemd, you can run the daemon directly and manage it yourself:

```sh
claude-monitor daemon &
```

Or integrate it with whatever process supervisor you prefer. The daemon writes its PID to `~/.cache/claude-monitor/daemon.pid` and handles `SIGUSR1` for immediate refresh and `SIGTERM`/`SIGINT` for graceful shutdown.

## Platform support

Currently Linux only. macOS support (tmux without systemd/D-Bus) is planned.
