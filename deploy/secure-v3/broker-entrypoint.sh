#!/bin/sh
set -eu
mkdir -p /tmp/secure
cp /run/tls/* /tmp/secure/
if [ -d /run/bridge-tls ]; then
 cp /run/bridge-tls/tls.crt /tmp/secure/client.crt
 cp /run/bridge-tls/tls.key /tmp/secure/client.key
fi
cp /provision/acl /tmp/secure/acl
cp /config/broker.conf /tmp/secure/broker.conf
chown -R mosquitto:mosquitto /tmp/secure /mosquitto/data
chmod 640 /tmp/secure/*
exec mosquitto -c /tmp/secure/broker.conf
