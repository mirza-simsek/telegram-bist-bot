#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")/.."

session="${BISTBOT_SCREEN_SESSION:-bistbot}"

if ! command -v screen >/dev/null 2>&1; then
	echo "screen is not installed on this machine"
	exit 1
fi

if screen -ls | grep -Eq "[[:space:]][0-9]+\\.${session}[[:space:]]"; then
	echo "screen session running: ${session}"
else
	echo "screen session not running: ${session}"
fi

ps -axo pid,ppid,stat,etime,command | awk '$0 !~ /awk/ && /[.]\/bistbot|[s]cripts\/supervise[.]sh|[b]ist_scan[.]py|SCREEN -dmS bistbot/ { print }' || true

echo
echo "last supervisor lines:"
tail -n 20 logs/bistbot.supervisor.log 2>/dev/null || true

echo
echo "last bot log lines:"
tail -n 40 logs/bistbot.shell.err.log 2>/dev/null || true
