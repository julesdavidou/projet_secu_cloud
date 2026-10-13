#!/bin/sh
set -eu
cd "$(dirname "$0")"
[ -f .env ] || cp .env.example .env
dc() { docker compose --profile placement "$@"; }
# --fresh resets only the dedicated simulation project's data.
if [ "${1:-}" = --fresh ]; then dc down -v --remove-orphans; fi
dc config --quiet
dc up -d --build --wait broker central zone-a zone-b a1 a2 b1 b2
dc run --rm --no-deps --build scenario scenario
before="$(dc run --rm --no-deps scenario snapshot)"
dc restart central
dc up -d --wait central
after="$(dc run --rm --no-deps scenario snapshot)"
[ "$before" = "$after" ] || { echo 'FAIL central restart changed catalog'; exit 1; }
echo 'PASS 07 central restart preserves exact authoritative catalog'
capacity_before="$(dc run --rm --no-deps scenario capacities)"
dc restart zone-a zone-b
dc up -d --wait zone-a zone-b
recovered=false
for i in $(seq 1 20); do
  capacity_after="$(dc run --rm --no-deps scenario capacities)"
  if [ "$capacity_before" = "$capacity_after" ]; then recovered=true; break; fi
  sleep 1
done
[ "$recovered" = true ] || { echo 'FAIL zone restart changed project capacity'; exit 1; }
echo 'PASS 11 zone restart restores durable debits and waits for fresh node liveness'
dc run --rm --no-deps scenario reconcile
dc logs --no-color
