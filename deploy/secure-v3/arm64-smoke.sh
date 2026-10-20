#!/bin/sh
set -eu
uname -m
edge-agent &
agent_pid=$!
trap 'kill "$agent_pid"; wait "$agent_pid"' EXIT
for attempt in 1 2 3 4 5; do
 if wget -q -O - http://127.0.0.1:8080/health; then
  printf '\nPASS ARM64 Go Edge HTTP health under emulation; physical hardware NOT_TESTED\n'
  exit 0
 fi
 sleep 1
done
exit 1
