#!/bin/sh
set -eu
cd "$(dirname "$0")"
profile=${1:-local}
[ -f .env ] || cp .env.example .env
set -a; . ./.env; set +a
if [ "$LAB_BIND_IP" != 127.0.0.1 ] && [ "${ALLOW_LAN:-0}" != 1 ]; then echo 'LAN binding requires ALLOW_LAN=1' >&2; exit 1; fi
export EDGE_MODE=$(cat .runtime/mode)
set -- -f compose.yaml
case "$profile" in
local) set -- "$@" --profile local; [ "$EDGE_MODE" != real-http ] || set -- "$@" --profile real-http ;;
lab-ready) case "$LAB_PC_ADDRESS" in localhost|192.0.2.*) echo 'Set real PC LAN IP' >&2; exit 1;; esac ;;
*) echo 'local or lab-ready required' >&2; exit 1 ;;
esac
docker compose "$@" config --quiet
docker compose "$@" up -d --wait --wait-timeout 120
docker compose "$@" ps
