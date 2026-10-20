#!/bin/sh
set -eu
edge-agent &
agent_pid=$!
trap 'kill "$agent_pid" 2>/dev/null || true; wait "$agent_pid" 2>/dev/null || true' EXIT
attempt=0
while [ "$attempt" -lt 30 ]; do
  if wget -q -O - http://127.0.0.1:8080/health; then
    printf '\nPASS Edge HTTP on target platform (physical hardware not tested)\n'
    exit 0
  fi
  attempt=$((attempt+1))
  sleep 1
done
exit 1
