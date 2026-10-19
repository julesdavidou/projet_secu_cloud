#!/bin/sh
set -eu
mkdir -p /tmp/auth
: > /tmp/auth/passwd
for file in /provision/*.password; do
  actor=$(basename "$file" .password)
  password=$(cat "$file")
  mosquitto_passwd -b /tmp/auth/passwd "$actor" "$password"
done
cp /provision/acl /tmp/auth/acl
chown -R mosquitto:mosquitto /tmp/auth /mosquitto/data
chmod 640 /tmp/auth/passwd
exec mosquitto -c /mosquitto/config/mosquitto.conf
