#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")/.."

session="${BISTBOT_SCREEN_SESSION:-bistbot}"
repo_dir="$(pwd)"

if ! command -v screen >/dev/null 2>&1; then
	echo "screen is not installed on this machine"
	exit 1
fi

if screen -ls | grep -Eq "[[:space:]][0-9]+\\.${session}[[:space:]]"; then
	screen -S "$session" -X quit
	echo "stopped bistbot screen session: ${session}"
else
	echo "no bistbot screen session is running: ${session}"
fi

supervisor_pids="$(pgrep -f "${repo_dir}/scripts/supervise[.]sh" 2>/dev/null || true)"
if [ -n "$supervisor_pids" ]; then
	for pid in $supervisor_pids; do
		kill "$pid" 2>/dev/null || true
	done
	sleep 1
	for pid in $supervisor_pids; do
		kill -9 "$pid" 2>/dev/null || true
	done
fi

bistbot_pids="$(ps -axo pid=,command= | awk '$2 == "./bistbot" { print $1 }' || true)"
if [ -n "$bistbot_pids" ]; then
	for pid in $bistbot_pids; do
		kill "$pid" 2>/dev/null || true
	done
	sleep 1
	for pid in $bistbot_pids; do
		kill -9 "$pid" 2>/dev/null || true
	done
fi

rm -f logs/bistbot.pid
