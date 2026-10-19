#!/bin/sh
set -eu
mkdir -p /tmp/secure
cp /run/tls/* /tmp/secure/
if [ -d /run/bridge-tls ]; then
 cp /run/bridge-tls/tls.crt /tmp/secure/client.crt
 cp /run/bridge-tls/tls.key /tmp/secure/client.key
fi
cp /provision/acl /tmp/secure/acl
if [ -n "${BRIDGE_NODE_ID:-}" ]; then
 # A local bridge is an internal Mosquitto client, separate from its remote
 # mTLS identity. Grant only this node's outgoing origin and inbound commands.
 case "$BRIDGE_NODE_ID" in ''|*[!A-Za-z0-9_.-]*) exit 1 ;; esac
 [ "${#BRIDGE_NODE_ID}" -le 100 ] || exit 1
 actor="edge-$BRIDGE_NODE_ID"
 {
  printf '\nuser bridge-%s\n' "$BRIDGE_NODE_ID"
  printf 'topic read lab/placement/v1/+/request/%s\n' "$actor"
  printf 'topic read lab/placement/v1/+/reply/%s\n' "$actor"
  printf 'topic read lab/placement/v1/presence/%s\n' "$actor"
  printf 'topic read lab/placement/v1/zone/+/node/%s/+\n' "$BRIDGE_NODE_ID"
  printf 'topic write lab/placement/v1/%s/request/+\n' "$actor"
  printf 'topic write lab/placement/v1/%s/reply/+\n' "$actor"
 } >> /tmp/secure/acl
fi
cp /config/broker.conf /tmp/secure/broker.conf
chown -R mosquitto:mosquitto /tmp/secure /mosquitto/data
chmod 640 /tmp/secure/*
# The supervisor changes only its own Mosquitto child and /etc/hosts. It has
# neither a Docker socket nor access to other containers. Stable TLS names are
# retained when DHCP addresses change. An unchanged lease does not restart MQTT.
cp /etc/hosts /tmp/secure/base-hosts
mosquitto -c /tmp/secure/broker.conf &
child=$!
trap 'kill "$child" 2>/dev/null || true; wait "$child" || true; exit 0' TERM INT
while kill -0 "$child" 2>/dev/null; do
 sleep 2 & wait $! || true
 cp /config/broker.conf /tmp/secure/next.conf
 cp /tmp/secure/base-hosts /tmp/secure/next.hosts
 lease=0
 if [ -f /discovery/expires ]; then lease=$(cat /discovery/expires); fi
 case "$lease" in ''|*[!0-9]*) lease=0 ;; esac
 now=$(date +%s)
 if [ "$lease" -gt "$now" ] && [ "$lease" -le "$((now + 31))" ] && [ -f /discovery/peers.conf ] && [ -f /discovery/hosts ]; then
  # Keep hub connection and exact own-origin topic rules; replace fixed peers.
  awk '/^connection / {count++} count < 2 {print}' /config/broker.conf > /tmp/secure/next.conf
  cat /discovery/peers.conf >> /tmp/secure/next.conf
  cat /discovery/hosts >> /tmp/secure/next.hosts
 fi
 if ! cmp -s /tmp/secure/next.conf /tmp/secure/broker.conf || ! cmp -s /tmp/secure/next.hosts /etc/hosts; then
  kill "$child" 2>/dev/null || true
  wait "$child" || true
  cp /tmp/secure/next.conf /tmp/secure/broker.conf
  cat /tmp/secure/next.hosts > /etc/hosts
  chown mosquitto:mosquitto /tmp/secure/broker.conf
  mosquitto -c /tmp/secure/broker.conf &
  child=$!
 fi
done
wait "$child"
