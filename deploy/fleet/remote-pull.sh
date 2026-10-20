#!/bin/sh
# Run as root. Credentials arrive only on stdin, never in argv or SFTP files.
set -eu
reference=$1
platform=$2
config_digest=$3
manifest_digest=$4
revision=$5
case "$reference" in ghcr.io/*@sha256:*) ;; *) exit 1;; esac
IFS= read -r registry_user
IFS= read -r registry_token
umask 077
credential_dir=$(mktemp -d /tmp/caribou-ghcr.XXXXXXXX)
trap 'rm -f "$credential_dir/config.json"; rmdir "$credential_dir"' EXIT
export DOCKER_CONFIG="$credential_dir"
getent hosts ghcr.io >/dev/null || { echo 'GHCR DNS inaccessible ; essayer -ImageSource local.' >&2; exit 1; }
printf '%s\n' "$registry_token" | docker login ghcr.io --username "$registry_user" --password-stdin >/dev/null 2>&1 || {
  echo 'GHCR HTTPS/authentification refusée ; vérifier réseau, PAT et read:packages.' >&2; exit 1;
}
unset registry_token
if ! docker pull --platform "$platform" "$reference" >/dev/null 2>&1; then
  echo 'Pull GHCR impossible : réseau, droits du package ou plateforme. Mode local disponible ; conteneurs conservés.' >&2
  exit 1
fi
actual=$(docker image inspect --format '{{.Id}}' "$reference")
[ "$actual" = "$config_digest" ] || [ "$actual" = "$manifest_digest" ] || { echo 'Digest image inattendu' >&2; exit 1; }
[ "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$reference")" = "$revision" ] || exit 1
[ "$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$reference")" = "${platform%/v7}" ] || exit 1
echo 'PASS GHCR HTTPS/authentification/pull et révision vérifiés'
