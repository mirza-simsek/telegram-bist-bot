#!/usr/bin/env sh
set -u

cd "$(dirname "$0")/.."
mkdir -p logs

PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"
export PATH

child_pid=""

stop_child() {
	if [ -n "$child_pid" ] && kill -0 "$child_pid" 2>/dev/null; then
		kill "$child_pid" 2>/dev/null || true
		wait "$child_pid" 2>/dev/null || true
	fi
}

trap '' HUP
trap 'echo "$(date "+%Y-%m-%d %H:%M:%S") supervisor received TERM" >> logs/bistbot.supervisor.log; stop_child; exit 0' TERM
trap 'echo "$(date "+%Y-%m-%d %H:%M:%S") supervisor received INT" >> logs/bistbot.supervisor.log; stop_child; exit 0' INT

while :; do
	started_at="$(date '+%Y-%m-%d %H:%M:%S')"
	echo "$started_at supervisor starting bistbot" >> logs/bistbot.supervisor.log
	./bistbot >> logs/bistbot.shell.log 2>> logs/bistbot.shell.err.log &
	child_pid="$!"
	wait "$child_pid"
	status="$?"
	child_pid=""
	finished_at="$(date '+%Y-%m-%d %H:%M:%S')"
	echo "$finished_at bistbot exited with status $status" >> logs/bistbot.supervisor.log
	sleep 5
done
