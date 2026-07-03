#!/usr/bin/env sh
set -eu

cd "$(dirname "$0")/.."
mkdir -p logs

session="${BISTBOT_SCREEN_SESSION:-bistbot}"

if ! command -v screen >/dev/null 2>&1; then
	echo "screen is not installed on this machine"
	exit 1
fi

if screen -ls | grep -Eq "[[:space:]][0-9]+\\.${session}[[:space:]]"; then
	echo "bistbot screen session already running: ${session}"
	exit 0
fi

go build -o bistbot ./cmd/bistbot

repo_dir="$(pwd)"
screen -dmS "$session" /bin/sh -c 'cd "$1" && exec ./scripts/supervise.sh' sh "$repo_dir"

echo "bistbot started in detached screen session: ${session}"
echo "logs: logs/bistbot.shell.err.log and logs/bistbot.supervisor.log"
