# Démarrage et diagnostic — Edge MQTT/reconstruction V2

Depuis la racine du dépôt, PowerShell 7, Docker Desktop en conteneurs Linux, Docker Compose récent (support de `!override`). Les commandes ne fusionnent aucune PR. Le nouveau projet est `caribou-edge-v2` ; il possède ses images, secrets et volumes. Les projets précédents restent disponibles.

```powershell
./deploy/edge-v2/start.ps1
# Si le réseau Docker exige son relais de téléchargement :
# ./deploy/edge-v2/start.ps1 -BuildProxy http://http.docker.internal:3128
Start-Process http://localhost:19290/integration/
./deploy/edge-v2/smoke.ps1
./deploy/edge-v2/network-smoke.ps1
./deploy/edge-v2/resilience.ps1
./deploy/edge-v2/diagnose.ps1
```

Le premier lancement construit quatre images et génère des identités distinctes dans `deploy/edge-v2/.runtime/` (ignoré par Git). Les exécutions suivantes conservent les secrets et les volumes. `-SkipBuild` réutilise les images. Le changement de topologie recrée les conteneurs concernés sans supprimer les volumes. Les tests ajoutent leurs propres comptes/vidéos de démonstration ; ne pas les exécuter en boucle sur un petit quota. Ils ne suppriment pas les vidéos déjà publiées.

| Service | Accès PC par défaut |
|---|---|
| Frontend + API vidéo | http://localhost:19290/integration/ |
| Central API opérateur | http://localhost:19288/api/v1/snapshot ; Bearer local |
| Edge A1 / B1 | http://localhost:19281 et :19282 ; Bearer requis |
| Broker hub | 127.0.0.1:1886 ; identités provisionnées |
| Brokers Edge et reconstruction Python | réseau Docker interne seulement |

Ports modifiables avec `V2_WEB_PORT`, `V2_CENTRAL_PORT`, `V2_EDGE_A_PORT`, `V2_EDGE_B_PORT`, `V2_MQTT_PORT`. La topologie publique, les quotas **projet** (64 MiB par défaut), les zones autorisées, TTL et leases se règlent dans `deploy/edge-v2/topology.json`. Ne pas réduire un quota en dessous des données déjà présentes. Les métriques de disque physique du client MQTT standalone ne deviennent jamais des offres de placement.

Le compte de démonstration se crée au premier login, conformément au service Java existant. Les sessions restent en mémoire et expirent au redémarrage du service auth ; les identités et mots de passe restent persistants. Le profil V2 conserve la source et ses sorties HLS, stockées en blocs variables. Pour un MP4 H264/AAC compatible navigateur, la lecture restitue la source exacte ; les autres codecs empruntent le parcours HLS/remux. `/api/videos/{id}/original` reconstruit toujours les octets originaux des nouvelles vidéos V2. Pour une ancienne vidéo V1 dont la source n’a jamais été conservée : HTTP 409, aucune promesse de restitution bit à bit.

## Reproduction des contrôles indépendants

```powershell
docker build -f deploy/lab/Dockerfile --target verify .
docker run --rm -v "${PWD}/videostreaming:/tests:ro" -w /tests caribou/edge-v2-reader:dev python -m unittest discover -p 'test_*.py' -v
docker run --rm -v "${PWD}:/repo:ro" -w /repo/frontend node:22-alpine node --test
Push-Location deploy/edge-v2
docker buildx bake -f compose.yaml --set '*.platform=linux/amd64,linux/arm64' --allow=fs.read=../.. --load
Pop-Location
```

Le workflow `Edge MQTT and reconstruction V2` répète compilation/tests des trois modules Go, tests Python, vraie publication/lecture, multi-zone, mobilité, corruption, reconnexion et persistance. Les workflows antérieurs restent actifs. Une compilation ARM64 et un lancement ARM64 sous QEMU ne prouvent pas le fonctionnement sur Jetson/Raspberry physiques.

## Préparer les machines du laboratoire

1. Copier `topology.json` vers un fichier privé, remplacer **tous** les endpoints par les vraies adresses LAN et ports des Edge (`http://IP_EDGE:18081`, par exemple), définir les quotas, garder les identités stables. L’adresse IP du PC et celles des cartes ne sont pas connues de cette livraison.
2. Construire les images multiarch ci-dessus. Sur le PC, démarrer les contrôleurs externes avec les valeurs réelles :

```powershell
# Remplacer les trois paramètres ci-dessous par vos données LAN réelles.
./deploy/edge-v2/start.ps1 -SkipBuild -ExternalEdges -Topology CHEMIN_TOPOLOGIE_LAN -Hub IP_PC:1886 -BindIP IP_PC
./deploy/edge-v2/export-edge.ps1 -Id A1 -Destination CHEMIN_PRIVE_NOUVEAU_A1
./deploy/edge-v2/export-edge.ps1 -Id B1 -Destination CHEMIN_PRIVE_NOUVEAU_B1
```

L’export refuse les DNS Docker locaux et une adresse de hub interne. Il embarque uniquement l’identité de l’Edge sélectionné, son broker et la configuration publique. Transférer chaque dossier par un canal privé, jamais dans Git. Les configurations exportées restent sensibles : le bridge contient le mot de passe de sa propre identité MQTT. Aucun secret opérateur ni ceux des autres Edge n’est exporté.

3. Transférer les images avec un registre privé ou des archives. Avec Docker récent :

```powershell
docker image save --platform linux/arm64 -o edge-v2-arm64.tar caribou/edge-v2-control:dev caribou/edge-v2-reader:dev
# Sur chaque carte : docker load -i edge-v2-arm64.tar
# Dans son dossier exporté :
# docker compose -f compose.edge.yaml up -d --wait
# docker compose -f compose.edge.yaml logs --tail 100
```

Le broker Mosquitto est téléchargé sur chaque carte si nécessaire. Les cartes doivent joindre le hub PC:1886 et tous les endpoints HTTP provisionnés doivent être joignables entre eux. Le worker reconstruit directement depuis les autres Edge. La variante PC arrête uniquement les Edge locaux **du nouveau projet** pour éviter des identités dupliquées ; les volumes sont conservés. Le changement local/LAN est à effectuer avant d’avoir des médias à conserver sous des endpoints différents ; aucune migration automatique de données ou des endpoints d’anciens objets n’est revendiquée.

Le bind LAN est explicite ; prévoir les règles réseau du laboratoire. Ce profil transporte HTTP/MQTT dans un LAN de confiance ; le déploiement TLS et l’autodécouverte réseau ne sont pas réalisés ici. Les scripts ne modifient pas le pare-feu du PC. Lancement matériel, Wi-Fi réel, performances/mémoire des cartes et versions Docker ARM64 restent à vérifier sur place.

## Arrêt sans suppression des données

```powershell
docker compose -f deploy/edge-v2/compose.yaml stop
# Relancer : ./deploy/edge-v2/start.ps1 -SkipBuild
```

Ne pas utiliser `down -v` sur vos volumes. `resilience.ps1` altère uniquement un bloc de son propre jeu d’essai et restaure les octets dans `finally` ; le catalogue n’est pas déclaré réparé ni répliqué automatiquement. Un bloc effectivement perdu produit un refus de lecture.
