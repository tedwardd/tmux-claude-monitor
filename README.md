# tmux-claude-monitor

A tmux status bar daemon that displays your [Claude Pro](https://claude.ai) quota usage in real time.

## What it shows

```
Claude: ███░░░ 52% ↺15:20 +$16.45/$80.00
```

| Component | Meaning |
|---|---|
| `███░░░` | Session quota used, drawn as six blocks |
| `52%` | Session utilization |
| `↺15:20` | Local time the session quota resets |
| `+$16.45/$80.00` | Extra usage against your monthly limit, shown only while extra usage is enabled on the account |

The text is green below 70%, yellow from 70%, and red from 90%. Any component can be switched off, see [Display options](#display-options).

Two placeholders stand in when there is no reading to show:

| Placeholder | Cause |
|---|---|
| `Claude: --` | No cache file, so the daemon has not run yet |
| `Claude: ??` | The last fetch failed, or the cache is older than 15 minutes |

Weekly usage is fetched and kept in the cache, but nothing renders it in the status line yet.

## Prerequisites

- Linux with systemd, or macOS 11 or later
- tmux
- An active Claude Pro subscription with [Claude Code](https://claude.ai/code) installed and authenticated

## Installation

### macOS (Homebrew)

```sh
brew install --cask tedwardd/tap/claude-monitor
```

Apple Silicon and Intel are both covered. To upgrade later:

```sh
brew upgrade --cask claude-monitor
```

An upgrade replaces the binary but the already-running daemon keeps the old one loaded, so the cask restarts the launchd agent for you once `init` has installed it. You do not need to re-run `init` after upgrading.

### Arch Linux (AUR)

```sh
# Pre-built binary, no Go toolchain needed
paru -S tmux-claude-monitor-bin

# Build from source
paru -S tmux-claude-monitor
```

Both packages install a systemd user unit at `/usr/lib/systemd/user/claude-monitor.service`. `claude-monitor init` finds that unit and skips installing its own, but it does not enable it, so start the packaged unit yourself:

```sh
systemctl --user enable --now claude-monitor
```

Run `init` as well for the credential check and tmux patching.

### Other Linux

Download the archive for your architecture from the [Releases](https://github.com/tedwardd/tmux-claude-monitor/releases) page and put the binary on your `$PATH`:

```sh
VERSION=0.3.1
mkdir -p ~/.local/bin
curl -fsSL "https://github.com/tedwardd/tmux-claude-monitor/releases/download/v${VERSION}/claude-monitor_${VERSION}_linux_amd64.tar.gz" \
  | tar xz -C ~/.local/bin/ claude-monitor
```

Set `VERSION` to the current release, and swap `amd64` for `arm64` on 64-bit ARM.

### Setup

Once installed, run the one-time setup on any platform:

```sh
claude-monitor init
```

`init` verifies your credentials, writes a config file, patches your tmux config, and installs a background service: a systemd user service on Linux, a launchd user agent on macOS. See [Manual setup](#manual-setup) below if you prefer to do any of these steps yourself.

On macOS the daemon reads your token from the login keychain. Depending on how the keychain item was created, macOS may ask whether `security` can access it. Choose "Always Allow" if prompted. The daemon reads the token once at startup, so a denied or dismissed prompt shows up again each time the service restarts.

## Commands

| Command | Description |
|---|---|
| `claude-monitor init` | One-time setup |
| `claude-monitor status` | Print the current status string (called by tmux) |
| `claude-monitor refresh` | Signal the daemon for an immediate fetch |
| `claude-monitor daemon` | Run the background poller (managed by systemd or launchd) |

`init` takes two flags:

| Flag | Effect |
|---|---|
| `--force` | Redo every step even if the config, tmux block, or service is already in place |
| `--discover` | Check auth, print the raw usage API response, and stop without changing anything |

**Manual refresh keybinding:** `<prefix> F5` — added to your tmux config by `init`.

## How it works

A background service polls the Claude API every 5 minutes and writes a cache file. The tmux status bar reads from that cache via `claude-monitor status`. The service is a systemd user service on Linux and a launchd user agent on macOS.

When a fetch fails, the error goes into the cache so the status bar shows `??`, and the next attempt backs off starting at 30 seconds and doubling up to 5 minutes. The interval returns to normal on the first success.

To catch a laptop coming out of sleep, the Linux daemon listens for the D-Bus `PrepareForSleep` signal from `org.freedesktop.login1.Manager` and refreshes within about 5 seconds. macOS has no equivalent signal reachable without cgo, so the daemon there compares wall-clock against monotonic elapsed time and treats a gap as a wake. Linux uses the same method when D-Bus is unavailable.

## Network access

The daemon opens exactly one connection: `GET https://api.anthropic.com/api/oauth/usage`, over TCP 443. There is no telemetry, no update check, and no second endpoint. `claude-monitor status` never touches the network at all, it only reads the cache file.

Every release archive contains `claude-monitor.lsrules`, a [Little Snitch](https://obdev.at/products/littlesnitch/) rule group that states this in a form the firewall enforces:

```sh
open claude-monitor.lsrules   # from the extracted archive, or the Caskroom directory
```

The point is not convenience. Because the group is the complete list of what the program needs, any connection attempt it does not cover is a signal that something changed, and Little Snitch will say so rather than allowing it quietly. The rule carries the reason for the connection in its `notes` field, so the policy documents itself.

Two things worth knowing:

The rule matches on destination rather than on the executable, using `process: "any"`. Homebrew installs to a versioned directory that changes on every upgrade, and Little Snitch matches the full executable path with no wildcard support, so a path-based rule would need re-approving after each upgrade. Scoping to `api.anthropic.com:443` avoids that. If you would rather pin the process as well, add a second rule with the output of `which claude-monitor` resolved through `readlink`, and expect to update it when the version changes.

DNS is not in the group. In testing the daemon's hostname lookups went through the system resolver rather than leaving the process directly, so no rule was needed. If you do see a DNS prompt attributed to `claude-monitor`, add a rule allowing `remote: "dns-servers"` for it.

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

On macOS, `credentials_path` normally points at a file that does not exist. That is expected: the daemon tries the path first and then reads the login keychain. Leave the key at its default unless you keep a credentials file of your own somewhere.

## Viewing logs

On Linux the daemon logs to journald:

```sh
journalctl --user -u claude-monitor -f
```

On macOS launchd writes stdout and stderr to a file:

```sh
tail -f ~/Library/Logs/claude-monitor.log
```

## Manual setup

`claude-monitor init` performs five steps in order. You can skip any step and implement it yourself — they are fully independent.

### Step 1 — Credentials

The daemon reads a Claude Code OAuth token. Where it comes from depends on how Claude Code stored it.

On Linux the token is in `~/.claude/.credentials.json` (the default path, configurable), written by Claude Code when you log in. It looks like:

```json
{
  "claudeAiOauth": {
    "accessToken": "...",
    "refreshToken": "...",
    "expiresAt": 1234567890000
  }
}
```

On macOS Claude Code keeps the same JSON in the login keychain instead of on disk, under the service name `Claude Code-credentials`. The daemon checks `credentials_path` first and falls back to the keychain when no file is there, which is the normal case on macOS. You can see the item with:

```sh
security find-generic-password -s "Claude Code-credentials"
```

`init` only reads the token, it never modifies it. If the token is missing or expired, run `claude logout && claude login` to refresh it.

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

`init` patches an existing `~/.tmux.conf` or `~/.config/tmux/tmux.conf`, falling back to the latter. It copies the file to `<path>.claude-monitor.bak` first, then appends this block:

```
# claude-monitor begin
set -g status-right-length 200
set -ga status-right " #(  claude-monitor status)"
bind-key F5 run-shell "claude-monitor refresh"
# claude-monitor end
```

Your own `set -g status-right` line is left exactly as it is. The block uses `-ga` to append to whatever you already set, so nothing of yours is rewritten or escaped. If your config sets no `status-right` at all, the block adds one of its own above the append.

Re-running `init` replaces the block rather than adding a second one, so `--force` is safe. Your non-append assignment resets the option ahead of the append each time the file is sourced, which is what stops the segment accumulating when you reload.

To add this manually, append it to your tmux config (adjusting `status-right` to fit your existing setup), then reload:

```sh
tmux source ~/.config/tmux/tmux.conf
```

If you use a different tmux config path, just add the `status-right` and `bind-key` lines wherever appropriate. The `#(  claude-monitor status)` call runs every time tmux refreshes the status bar and reads from the local cache — it does not hit the network.

### Step 4 — Background service

On macOS, `init` installs a launchd agent at `~/Library/LaunchAgents/com.github.tedwardd.claude-monitor.plist` pointing at the installed binary, then loads it:

```sh
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.github.tedwardd.claude-monitor.plist
launchctl kickstart gui/$(id -u)/com.github.tedwardd.claude-monitor
```

The agent records the binary's stable `$PATH` location rather than the path it was launched from, so a Homebrew upgrade does not leave it pointing into an old versioned directory.

The agent sets `RunAtLoad` so it starts on login, and `KeepAlive` with `SuccessfulExit` false so launchd restarts it if it crashes but leaves it alone after a clean exit. `ThrottleInterval` holds restarts to one every 30 seconds. To remove it:

```sh
launchctl bootout gui/$(id -u)/com.github.tedwardd.claude-monitor
rm ~/Library/LaunchAgents/com.github.tedwardd.claude-monitor.plist
```

On Linux, `init` installs a user service at `~/.config/systemd/user/claude-monitor.service`:

```ini
[Unit]
Description=Claude usage monitor
After=network.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
ExecStart=claude-monitor daemon
Restart=on-failure
RestartSec=30

[Install]
WantedBy=default.target
```

systemd resolves a bare command name against a fixed list of system directories such as `/usr/bin`, not against your `$PATH`. That works for the AUR packages, which install to `/usr/bin`. If you installed the binary somewhere else, `~/.local/bin` for instance, change `ExecStart` to the absolute path from `which claude-monitor`.

To install it manually:

```sh
mkdir -p ~/.config/systemd/user
# write the unit file as above, then:
systemctl --user daemon-reload
systemctl --user enable --now claude-monitor
```

The service restarts automatically on failure (up to 5 times per 5 minutes) and starts on login via `default.target`.

### Running without a service manager

If you don't want systemd or launchd involved, run the daemon directly and manage it yourself:

```sh
claude-monitor daemon &
```

Or integrate it with whatever process supervisor you prefer. The daemon writes its PID to `~/.cache/claude-monitor/daemon.pid` and handles `SIGUSR1` for immediate refresh and `SIGTERM`/`SIGINT` for graceful shutdown.

## Platform support

Linux and macOS, on both amd64 and arm64. Windows is not supported: the daemon uses `SIGUSR1` for the refresh signal.

Config and cache stay at `~/.config/claude-monitor` and `~/.cache/claude-monitor` on both platforms rather than moving under `~/Library` on macOS, so the same config file works on either.
