#!/usr/bin/env bash
# Hunt for duplicate or conflicting pollers.
#
# `claude-monitor doctor` explains why the status bar has no reading. This covers
# the other failure mode: more than one daemon, or a locally built binary and the
# installed one fighting over the same PID file and cache. Both write
# ~/.cache/claude-monitor/daemon.pid, and the loser's exit deletes it, which
# leaves `refresh` reporting no daemon while one is running perfectly well.
set -uo pipefail

fails=0
notes=0

ok()   { printf '  [ok  ] %-16s %s\n' "$1" "$2"; }
bad()  { printf '  [FAIL] %-16s %s\n' "$1" "$2"; fails=$((fails + 1)); }
note() { printf '  [note] %-16s %s\n' "$1" "$2"; notes=$((notes + 1)); }

# pgrep -f matches this script's own subshells, so confirm each candidate really
# is a claude-monitor daemon by looking at its argv.
daemon_pids() {
  local p cmd exe sub
  for p in $(pgrep -f 'claude-monitor daemon' 2>/dev/null); do
    [ "$p" = "$$" ] && continue
    cmd=$(ps -o command= -p "$p" 2>/dev/null) || continue
    # shellcheck disable=SC2086
    set -- $cmd
    exe=${1:-}
    sub=${2:-}
    [ "$(basename "$exe" 2>/dev/null)" = "claude-monitor" ] && [ "$sub" = "daemon" ] && echo "$p"
  done
}

daemon_exe() { ps -o command= -p "$1" 2>/dev/null | awk '{print $1}'; }

printf 'claude-monitor poller check\n\n'

pids=$(daemon_pids)
count=$(printf '%s' "$pids" | grep -c . || true)

case "$count" in
  0) bad  "daemons"  "none running" ;;
  1) ok   "daemons"  "1 running (PID $pids, $(daemon_exe "$pids"))" ;;
  *) bad  "daemons"  "$count running, they will fight over the PID file and double the API polling"
     for p in $pids; do printf '         %-14s PID %-7s %s\n' "" "$p" "$(daemon_exe "$p")"; done ;;
esac

# A daemon started from a build tree is the usual source of a hijacked PID file.
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
for p in $pids; do
  exe=$(daemon_exe "$p")
  case "$exe" in
    "$repo_root"/*) bad "source build" "PID $p is running from this tree: $exe" ;;
  esac
done

pidfile="$HOME/.cache/claude-monitor/daemon.pid"
if [ ! -f "$pidfile" ]; then
  if [ "$count" -gt 0 ]; then
    bad "pid file" "missing at $pidfile while a daemon runs, so refresh will report no daemon"
  else
    ok "pid file" "absent, consistent with no daemon"
  fi
else
  recorded=$(tr -d '[:space:]' < "$pidfile")
  if ! printf '%s\n' $pids | grep -qx "$recorded" 2>/dev/null; then
    if kill -0 "$recorded" 2>/dev/null; then
      bad "pid file" "names PID $recorded, which is not a claude-monitor daemon"
    else
      bad "pid file" "names PID $recorded, which is not running (stale). refresh will fail"
    fi
  else
    ok "pid file" "names the running daemon ($recorded)"
  fi
fi

# The service manager's view should agree; a mismatch means something started a
# daemon outside it.
if [ "$(uname)" = Darwin ]; then
  label=com.github.tedwardd.claude-monitor
  managed=$(launchctl print "gui/$(id -u)/$label" 2>/dev/null | awk '/^\tpid = /{print $3}')
  if [ -z "$managed" ]; then
    note "launchd" "agent not loaded or not running"
  elif printf '%s\n' $pids | grep -qx "$managed" 2>/dev/null; then
    ok "launchd" "manages the running daemon ($managed)"
  else
    bad "launchd" "manages PID $managed but that is not among the running daemons"
  fi
else
  managed=$(systemctl --user show -p MainPID --value claude-monitor 2>/dev/null)
  if [ -z "$managed" ] || [ "$managed" = 0 ]; then
    note "systemd" "unit not running"
  elif printf '%s\n' $pids | grep -qx "$managed" 2>/dev/null; then
    ok "systemd" "manages the running daemon ($managed)"
  else
    bad "systemd" "manages PID $managed but that is not among the running daemons"
  fi
fi

# Anything else holding the same OAuth token shares its rate limit, which shows
# up as an unexplained 429.
others=$(ps -Ao comm= 2>/dev/null | grep -oE 'ClaudeBar|CodeBurn[A-Za-z]*' | sort -u | paste -sd, - || true)
if [ -n "$others" ]; then
  note "other pollers" "$others also running, sharing the token's rate limit"
else
  ok "other pollers" "none found"
fi

printf '\n'
if [ "$fails" -gt 0 ]; then
  printf '%d conflict(s) found.\n\n' "$fails"
  printf 'Usual fix: stop any daemon you started by hand, then let the service\n'
  printf 'manager own the only one.\n\n'
  if [ "$(uname)" = Darwin ]; then
    printf '  pkill -f "claude-monitor daemon"\n'
    printf '  launchctl kickstart -k gui/$(id -u)/com.github.tedwardd.claude-monitor\n'
  else
    printf '  pkill -f "claude-monitor daemon"\n'
    printf '  systemctl --user restart claude-monitor\n'
  fi
  printf '\nThen: claude-monitor doctor\n'
  exit 1
fi

if [ "$notes" -gt 0 ]; then
  printf 'No conflicts. %d note(s) above are worth knowing but are not faults.\n' "$notes"
else
  printf 'No conflicts.\n'
fi
