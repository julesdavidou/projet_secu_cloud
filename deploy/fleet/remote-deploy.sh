#!/bin/bash
# Exécuté via sudo dans le seul dossier dédié à CaribouTV.
set -Eeuo pipefail
base=$1
commit=$2
archive_hash=$3
image=$4
image_id=$5
edge_id=$6
quota=$7
transport_id=${8:-$image_id}
architecture=${9:-}
image_source=${10:-local}
force_recreate=${11:-0}
case "$base" in /home/*/cariboutv-lab|/root/cariboutv-lab) ;; *) echo 'Dossier distant refusé' >&2; exit 1;; esac
[[ "$commit" =~ ^[0-9a-f]{40}$ ]] || exit 1
[[ "$edge_id" =~ ^[a-zA-Z0-9_-]+$ ]] || exit 1
incoming="$base/incoming"
release="$base/releases/$commit"
deployment="$base/deployment"
if [ -e "$base/repo" ] && [ ! -L "$base/repo" ]; then
  echo 'Le chemin repo existe déjà et ne provient pas de ce déploiement ; données conservées.' >&2; exit 1
fi
compose() {
  if [ -x "$base/tools/docker-compose" ]; then "$base/tools/docker-compose" "$@"; else docker compose "$@"; fi
}
exec 9>"$base/deploy.lock"
flock -n 9 || { echo 'Un déploiement est déjà en cours sur cet Edge' >&2; exit 1; }

# Le manifeste existant protège l’identité et le quota du ledger persistant.
if [ -f "$deployment/identity" ]; then
  [ "$(cat "$deployment/identity")" = "$edge_id:$quota" ] || {
    echo 'Identité/quota modifié : réconciliation du stockage requise, données conservées.' >&2; exit 1;
  }
fi
printf '%s  %s\n' "$archive_hash" "$incoming/source.tar.gz" | sha256sum -c -
mkdir -p "$release" "$deployment/edge-config/identity"
if [ ! -f "$deployment/identity" ]; then printf '%s\n' "$edge_id:$quota" > "$deployment/identity"; fi
if [ ! -f "$release/.fleet-complete" ]; then
  tar -xzf "$incoming/source.tar.gz" -C "$release" --no-same-owner
  printf '%s\n' "$commit" > "$release/.fleet-complete"
fi
actual=$(docker image inspect --format '{{.Id}}' "$image" 2>/dev/null || true)
if [ "$actual" != "$image_id" ] && [ "$actual" != "$transport_id" ]; then
  [ "$image_source" = local ] || { echo 'Image GHCR non préparée : conteneurs conservés.' >&2; exit 1; }
  docker load -i "$incoming/image.tar"
fi
actual=$(docker image inspect --format '{{.Id}}' "$image")
[ "$actual" = "$image_id" ] || [ "$actual" = "$transport_id" ] || { echo 'Image incorrecte : configuration ou manifeste inattendu' >&2; exit 1; }
actual_revision=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")
case "$actual_revision" in ''|'<no value>'|'<nil>')
  actual_revision=$(docker image inspect --format '{{index .Config.Labels "org.caribou.source-revision"}}' "$image");;
esac
[ "$actual_revision" = "$commit" ] || { echo 'Révision image incorrecte' >&2; exit 1; }
if [ -n "$architecture" ]; then
  [ "$(docker image inspect --format '{{.Os}}/{{.Architecture}}' "$image")" = "linux/${architecture%/v7}" ] || { echo 'Architecture image incorrecte' >&2; exit 1; }
fi

# Keep the previous configuration and image reference for a local rollback.
# No volume is deleted. Backups stay in the dedicated project directory.
backup=''
if [ -f "$deployment/.env" ]; then
  backup=$(mktemp -d "$base/previous.XXXXXXXX")
  chmod 700 "$backup"
  cp -a "$deployment/." "$backup/"
fi
rollback() {
  status=$?
  trap - ERR
  if [ -n "$backup" ]; then
    echo 'Échec : restauration de la configuration et de l’image précédentes.' >&2
    cp -a "$backup/." "$deployment/"
    (cd "$deployment" && compose -p caribou-fleet -f compose.edge.yaml up -d --no-build --pull never --wait --wait-timeout 180) || echo 'Rollback incomplet : vérifier les services manuellement.' >&2
  else
    echo 'Premier déploiement en échec ; aucun volume supprimé.' >&2
  fi
  exit "$status"
}
trap rollback ERR

config_changed=0
cmp -s "$incoming/config.json" "$deployment/edge-config/config.json" || config_changed=1
install -m 600 "$incoming/edge.env" "$deployment/.env"
install -m 644 "$incoming/config.json" "$deployment/edge-config/config.json"
install -m 644 "$release/deploy/lab/compose.edge.yaml" "$deployment/compose.edge.yaml"
# Seulement les secrets de cet Edge ; aucun secret opérateur ou d’un autre nœud.
for secret in "$incoming/identity/"*; do
  [ -f "$secret" ] || continue
  cmp -s "$secret" "$deployment/edge-config/identity/$(basename "$secret")" || config_changed=1
  install -m 640 -o 10001 -g 10001 "$secret" "$deployment/edge-config/identity/$(basename "$secret")"
done
chown 10001:10001 "$deployment/edge-config/identity"
chmod 750 "$deployment/edge-config/identity"
chmod 700 "$deployment/edge-config"
cd "$deployment"
refresh=()
if [ "$force_recreate" = 1 ] || [ "$config_changed" = 1 ]; then refresh=(--force-recreate); fi
compose -p caribou-fleet -f compose.edge.yaml config --quiet
compose -p caribou-fleet -f compose.edge.yaml up -d --no-build --pull never "${refresh[@]}" --wait --wait-timeout 180
compose -p caribou-fleet -f compose.edge.yaml ps
ln -sfn "$release" "$base/repo"
install -m 644 "$incoming/manifest.json" "$deployment/manifest.json"
printf '%s\n' "$edge_id:$quota" > "$deployment/identity"
echo "PASS $edge_id commit=$commit"
trap - ERR
# Les volumes Docker, les autres projets et les anciennes releases sont conservés.
