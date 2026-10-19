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
 case "$BRIDGE_NODE_ID" in A1|B1) ;; *) exit 1 ;; esac
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
exec mosquitto -c /tmp/secure/broker.conf
