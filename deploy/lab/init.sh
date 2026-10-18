#!/bin/sh
set -eu
cd "$(dirname "$0")"
mode=${1:-real-http}
topology=${2:-config/topology.local.json}
[ -f .env ] || cp .env.example .env
set -a; . ./.env; set +a
docker build -f Dockerfile --target runtime -t "$CONTROLLERS_IMAGE" ../..
mkdir -p .runtime
docker run --rm --user 0:0 -e HOST_UID="$(id -u)" -v "$(realpath "$topology"):/input/topology.json:ro" -v "$PWD/.runtime:/output" "$CONTROLLERS_IMAGE" lab-admin -mode "$mode"
printf '%s\n' "$mode" > .runtime/mode
echo "PASS provisioned $mode; secrets in .runtime/"
