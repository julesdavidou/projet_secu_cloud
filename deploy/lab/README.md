# Contrôleurs CaribouTV — PC Docker → laboratoire

Les vrais processus sont `central-controller`, `serveur-zone` de Dan (entrée `app/allocator.go`) et, par Edge, `edge-bridge` + le véritable `cmd/edge-agent` de main. HRW, extents, coordinateur, réservations, catalogue SQLite et bus MQTT5 de la PR #1 sont réutilisés. Aucun placement n'est calculé dans les scripts clients. Le banc historique `lab/placement` reste intact.

## Ce soir depuis Windows

Prérequis : Docker Desktop démarré en mode conteneurs Linux, Docker Compose avec `up --wait`, PowerShell 5.1 ou 7. Première construction : accès Internet aux images et modules Go. Aucun Go à installer sur le PC. Depuis la racine du dépôt sur `codex/controllers-docker-lab` :

```powershell
.\deploy\lab\start.ps1
.\deploy\lab\smoke.ps1
.\deploy\lab\diagnostics.ps1
```

`start.ps1` construit l'image, provisionne des mots de passe distincts, valide Compose et attend la santé des huit services. Le défaut est **local, real-http**, sur loopback. La topologie de démonstration autorise 1 MiB par Edge afin de forcer facilement le passage de A vers B. Les deux vrais agents possèdent des volumes de fichiers indépendants. Le smoke nécessite cette topologie et des volumes neufs ; il écrit environ 1,2 MiB, redémarre les contrôleurs/un Edge et teste sa mobilité. Il s'arrête au premier échec.

Pour rejouer le smoke, effacer explicitement **les seules données de cette démonstration** :

```powershell
.\deploy\lab\reset-demo.ps1 -DeleteTestVolumes
.\deploy\lab\start.ps1 -SkipInit
.\deploy\lab\smoke.ps1
```

Pour arrêter en conservant les données : `.\deploy\lab\reset-demo.ps1`. Ne jamais utiliser `docker system prune` pour ce banc. Identités et jetons restent dans `.runtime/`, ignoré par Git et exclu du build Docker. Ne pas committer ou partager ce dossier ; réserver l'accès au compte utilisateur du PC.

Mode simulé, volontairement séparé (destruction des données de test explicite avant changement de mode) :

```powershell
.\deploy\lab\reset-demo.ps1 -DeleteTestVolumes
.\deploy\lab\init.ps1 -Mode mock
.\deploy\lab\start.ps1 -SkipInit
.\deploy\lab\smoke.ps1
# Retour aux fichiers réels : reset-demo -DeleteTestVolumes, init -Mode real-http, start -SkipInit.
```

En `mock`, aucun processus agent de fichiers n'est lancé. `MOCK_STORE` est réservé à ce mode ; les preuves sont `MOCK`. En `real-http`, il est refusé. Le même mot `READY` du catalogue historique existe dans les deux bancs : vérifier **le mode de configuration ET la preuve de chaque unité**. Seule `HTTP_SHA256_VERIFIED` qualifie le chemin avec vrais octets.

## Configuration et stockage

Copier `.env.example` vers `.env` (automatique au premier démarrage). `.env` contrôle interfaces, ports, slots Compose et image. `config/topology.local.json` définit zones, identités, quota, endpoint annoncé, zones autorisées à la mobilité et temporisations. Le provisionnement produit `.runtime/config.json` et les ACL exactes ; après modification, relancer `init.ps1`, puis `start.ps1 -SkipInit`. Ne pas changer une identité, un quota ou un endpoint sur des volumes actifs sans réconciliation ; pour la démo, arrêter et réinitialiser ses volumes explicitement.

| Valeur | Source effective | Sens |
|---|---|---|
| Identité Edge | `EDGE_ID` = un `nodes[].id` provisionné | Stable à travers les redémarrages et déplacements |
| Zone initiale | `nodes[].zone` ; `ZONE_ID` sur serveur-zone | L'Edge restaure ensuite sa zone/génération depuis son ledger |
| Quota projet | `nodes[].quota_bytes` | Équivalent demandé de PROJECT_QUOTA_BYTES ; la source unique est le JSON, aucune déduction automatique depuis le disque |
| Broker bridge | `MQTT_BROKER=mqtt://IP_PC:1883` | MQTT5, credentials montés sous SECRETS_DIR |
| Ancien Edge Go | `EDGE_ZONE`, `STORAGE_ROOT`, `HTTP_AUTH_TOKEN_FILE` | Alias legacy conservés ; son MQTT legacy est désactivé dans ce déploiement |
| Annonce HTTP | `nodes[].endpoint` | Adresse de l'**Edge**, joignable depuis le client qui enverra les fichiers |
| TTL / lease | `node_ttl_seconds` / `lease_seconds` | 5/30 s dans la démo ; jusqu'à 60/600 s, exemple labo 300 s conseillé pour transferts lents |
| Central | DB_PATH / CENTRAL_DB_PATH | `/data/central.db`, unique writer SQLite des placements |
| Zones | ZONE_DB_PATH / LEDGER_PATH | Registre de Dan + ledger de réservations, responsabilités locales |

Le quota net = quota autorisé − fichiers STORED − réservations actives non expirées. Les IDs de réservations déjà déduites sont annoncés dans `included_reservation_ids`, ce qui empêche une seconde déduction par le scheduler. Le disque libre physique demeure une mesure indépendante. Un fichier STORED reste débité après expiration du bail. Pas de GC ni de suppression métier automatique dans cette version ; ne pas retirer manuellement des fichiers puis annoncer une capacité récupérée.

`PREPARE_STORE` réserve seulement. Chaque unité contient une taille et un SHA-256 attendus immuables. HTTP transmet les octets au vrai `internal/storage` : fichier temporaire imprévisible, contrôle longueur/hash, fsync, rename, fsync des répertoires ; le bridge relit et revérifie ensuite le fichier. Il persiste STORED avant de notifier Central ; les notifications non confirmées sont réessayées après redémarrage. Aucune confirmation métier après erreur de fichier, hash ou ledger. Un fichier écrit avant un échec tardif peut être orphelin, sans faux READY.

Central conserve également `control-metadata.json` dans **son même volume** : hashes attendus, endpoints, état de transport et outbox durable. Les placements et états de stockage restent dans le catalogue SQLite existant. Sauvegarder/restaurer ensemble tout le volume Central (DB, WAL éventuel et métadonnées) et les volumes Edge. Un événement outbox impossible à réconcilier fait échouer explicitement le démarrage ; ne pas effacer l'outbox pour forcer READY.

## Demain au labo

Il faut Docker Engine/Compose sur chaque hôte, un OS Linux **64 bits ARM64** sur les RPi/Jetson et un réseau isolé où PC et Edge se joignent. Pas d'exécution physique ARM validée ce soir. JetPack, CUDA et accélération vidéo sont hors scope. Synchroniser les horloges (NTP, `timedatectl status`) : les snapshots et baux utilisent des dates absolues.

1. Sur le PC, arrêter la démo locale. Copier `config/topology.lab-ready.example.json` vers `config/topology.lab-ready.json`. Remplacer **chaque** adresse `192.0.2.*` par l'IP réelle de l'Edge concerné. Ce préfixe est un exemple non routable. Ajuster IDs, zones, quota projet et `lease_seconds` selon les transferts. Les endpoints doivent être des IP LAN des Edge, jamais `broker`, `agent-a` ni `localhost`.
2. Dans `.env`, mettre `LAB_BIND_IP=IP_LAN_DU_PC` et `LAB_PC_ADDRESS=IP_LAN_DU_PC`. Garder les ports ou les changer explicitement. Les noms des deux slots Zone (`ZONE_A_ID`, `ZONE_B_ID`) doivent correspondre aux zones JSON. La logique accepte d'autres IDs et jusqu'à six zones ; ajouter des services Compose identiques si nécessaire. Les listes A1/B1 existent uniquement dans les exemples et le smoke, pas dans les handlers.
3. Depuis PowerShell PC (le reset ci-dessous efface les seules données de démonstration de ce projet ; sauvegarder auparavant si nécessaire) :

```powershell
.\deploy\lab\reset-demo.ps1 -DeleteTestVolumes
.\deploy\lab\start.ps1 -Profile lab-ready -AllowLan
.\deploy\lab\export-edge.ps1 -Id A1 -Destination "$PWD\edge-A1-private"
.\deploy\lab\export-edge.ps1 -Id B1 -Destination "$PWD\edge-B1-private"
```

Le profil lab-ready lance uniquement broker, Central et deux serveurs-zone sur le PC. `-AllowLan` est un opt-in explicite. Les scripts ne modifient pas le pare-feu. Si des données de démo persistent, les sauvegarder ou les supprimer explicitement avant le changement de topologie. Exporter chaque dossier privé **uniquement sur l'Edge correspondant**, par clé USB privée ou SSH. Le dossier contient son mot de passe MQTT, son jeton HTTP et le JSON public ; jamais les secrets central/admin/autres Edge.

4. Depuis le dossier privé copié sur le RPi/Jetson, Bash (remplacer les marqueurs par les vraies valeurs) :

```bash
# Préalablement : docker load -i controllers-arm64.tar
export CONTROLLERS_IMAGE=caribou/controllers:2026-10-18
export EDGE_ID=A1
export LAB_PC_ADDRESS=IP_LAN_DU_PC
export MQTT_PORT=1883
export EDGE_BIND_IP=IP_LAN_DE_CET_EDGE
export EDGE_HTTP_PORT=18081
# Les fichiers copiés depuis Windows doivent rester privés et lisibles du groupe du conteneur.
chmod 700 edge-config
sudo chown -R "$(id -u):10001" edge-config/identity
chmod 750 edge-config/identity
chmod 640 edge-config/identity/*
docker compose -f compose.edge.yaml config --quiet
docker compose -f compose.edge.yaml up -d --wait --wait-timeout 120
docker compose -f compose.edge.yaml ps
curl --fail "http://$EDGE_BIND_IP:$EDGE_HTTP_PORT/ready"
```

`EDGE_ID` doit correspondre au dossier et au JSON. Sur le deuxième Edge, exporter B1 et sa propre IP. Le broker est l'IP LAN du PC, **jamais localhost depuis une machine distante**. Les deux réseaux Docker de deux hôtes ne sont pas interconnectés : seuls les ports publiés sur leurs IP LAN transportent MQTT/HTTP.

5. Pare-feu/routage : autoriser TCP MQTT_PORT sur le PC depuis les seules IP des Edge ; le port Central depuis le seul poste opérateur si nécessaire ; sur chaque Edge, EDGE_HTTP_PORT depuis le PC/client. Vérifier les VLAN/Wi-Fi isolés, les adresses DHCP et le profil réseau Windows. Exemple **à adapter puis exécuter dans PowerShell administrateur**, sans ouvrir toutes les machines du campus :

```powershell
# $pcIp et $edgeIps doivent contenir les IP réelles, pas 192.0.2.*.
New-NetFirewallRule -DisplayName 'Caribou MQTT labo' -Direction Inbound -Action Allow -Protocol TCP -LocalAddress $pcIp -LocalPort 1883 -RemoteAddress $edgeIps -Profile Private
# Retrait après la séance : Remove-NetFirewallRule -DisplayName 'Caribou MQTT labo'
Test-NetConnection -ComputerName $pcIp -Port 1883
Test-NetConnection -ComputerName $edgeIps[0] -Port 18081
```

Sur Linux, vérifier `ip -br addr`, `ip route`, `nc -vz IP_PC 1883`. Ne pas recopier des règles ufw/firewalld d'une machine inconnue. MQTT/HTTP sont authentifiés mais **non chiffrés dans ce banc** ; utiliser uniquement un réseau local isolé et de confiance. TLS/VPN est un prérequis avant un réseau partagé non fiable. Les mutations et consultations du catalogue exigent un jeton, les écritures MQTT des ACL exactes ; les endpoints santé sont publics.

6. Depuis le PC, envoyer de petits segments synthétiques sans changer le placement Go :

```powershell
[IO.File]::WriteAllBytes("$PWD\segment0.bin", [byte[]](1..200))
[IO.File]::WriteAllBytes("$PWD\segment1.bin", [byte[]](201..250))
.\deploy\lab\upload.ps1 -SourceNodeId A1 -Files @("$PWD\segment0.bin", "$PWD\segment1.bin")
.\deploy\lab\diagnostics.ps1
```

Ce client demande un extent par segment positif (`target_size_bytes=1`), reçoit le plan Go, puis utilise exclusivement ses endpoints. Il n'exécute ni HRW ni stratégie de secours. Le résultat doit être READY avec toutes les preuves HTTP_SHA256_VERIFIED. Un petit upload peut rester dans A ; pour solliciter B, choisir plusieurs unités compatibles avec son quota dont le total dépasse la capacité restante de A. Ne pas dépasser 1 GiB par unité ; privilégier des unités petites qui terminent avant le bail et les timeouts HTTP de 30 s.

## Images et fonctionnement hors ligne

```powershell
# Depuis la racine du dépôt, avec Internet ce soir :
docker buildx build --platform linux/amd64,linux/arm64 -f deploy/lab/Dockerfile --target runtime -t caribou/controllers:2026-10-18 --load .
docker image save --platform linux/arm64 -o controllers-arm64.tar caribou/controllers:2026-10-18
docker image save --platform linux/amd64 -o controllers-amd64.tar caribou/controllers:2026-10-18
# Sur la machine cible : docker load -i controllers-arm64.tar
```

Docker Desktop avec magasin containerd accepte le chargement multiarchitecture. Sur un Engine plus ancien, construire/sauvegarder une architecture à la fois avec `--platform linux/arm64 --load`. L'image contient les cinq exécutables distincts, sans secrets, CGO_ENABLED=0 ; aucun compilateur n'est requis sur les Edge. Le PC doit aussi avoir l'image `eclipse-mosquitto:2.0.22` déjà chargée (`docker save`/`load` si PC déplacé). Les contrôleurs déjà construits se démarrent avec `start.ps1 -SkipInit` ; provisionnement sans reconstruction : `init.ps1 -SkipBuild`. Sur un nouveau PC hors ligne ayant chargé l'archive versionnée, sélectionner explicitement son tag :

```powershell
$env:CONTROLLERS_IMAGE='caribou/controllers:2026-10-18'
.\deploy\lab\init.ps1 -SkipBuild
.\deploy\lab\start.ps1 -SkipInit
```

Bash alternatif pour un PC Linux :

```bash
sh deploy/lab/init.sh real-http
sh deploy/lab/start.sh local
sh deploy/lab/smoke.sh # même suite, nécessite PowerShell 7 (pwsh) sur le poste de test
# Labo : adapter .env et config/topology.lab-ready.json, puis
sh deploy/lab/init.sh real-http config/topology.lab-ready.json
ALLOW_LAN=1 sh deploy/lab/start.sh lab-ready
```

`smoke.sh` annonce NOT_TESTED et sort non-zéro si pwsh manque ; il n'invente pas une seconde suite validée. Aucun PowerShell n'est nécessaire pour faire fonctionner les conteneurs Linux distants.

## Diagnostic en moins de cinq minutes

```powershell
.\deploy\lab\diagnostics.ps1
docker compose -f deploy/lab/compose.yaml --profile local --profile real-http ps -a
docker compose -f deploy/lab/compose.yaml --profile local --profile real-http logs --tail 100 central zone-a zone-b edge-a edge-b broker
# Si bind loopback :
Invoke-RestMethod http://127.0.0.1:18088/health
Invoke-RestMethod http://127.0.0.1:18088/ready
$token=(Get-Content -Raw deploy/lab/.runtime/operator/operator.token).Trim()
Invoke-RestMethod http://127.0.0.1:18088/api/v1/snapshot -Headers @{Authorization="Bearer $token"}
```

| Symptôme | Vérification / correction |
|---|---|
| Docker indisponible | Démarrer Docker Desktop Linux ; `docker info` |
| Broker crash `illegal option` | Fins de lignes LF du script sh ; `.gitattributes` les impose |
| Broker healthy, bridge unhealthy | Logs du bridge : credentials, ACL, identité, zone/endpoint/quota non provisionnés, horloge en avance |
| Liveness healthy mais ready 503 | MQTT ou Central non joignable ; health ne prouve pas la readiness |
| Aucun nœud éligible | Attendre heartbeat frais, vérifier TTL/NTP ; retained AVAILABLE seul ne suffit pas |
| API 401 | Jeton manquant/erroné ; ne pas désactiver l'authentification pour dépanner |
| Upload 409 | Source non enregistrée, capacité insuffisante, extent trop grand ou réservation rejetée ; pas d'allocateur de secours |
| HTTP 422 | Longueur/hash, unité non préparée ou bail expiré ; nouvel upload avec un autre request_id après correction |
| HTTP 5xx / stockage indisponible | Logs agent/bridge, montage /data, permissions, quota/disque ; aucun READY de secours |
| Labo MQTT OK mais HTTP KO | Endpoint annoncé IP/port de l'Edge, bind et pare-feu ; ne pas utiliser le DNS Docker du PC |
| Quota plus bas après test | Fichiers STORED réellement comptés ; inspecter snapshot/ledger, reset explicite de démo seulement |
| Central ne restaure pas | Sauvegarder tout le volume et lire l'erreur outbox/JSON/SQLite ; ne jamais supprimer arbitrairement le catalogue |

Inspection fichier réelle : `docker compose -f deploy/lab/compose.yaml exec agent-a find /data/tenants/project/objects -type f`, puis `sha256sum` et `stat -c %s` dans le conteneur. Le smoke exécute ces contrôles. Pour SQLite et les événements, copie cohérente : arrêter Central, `docker cp caribou-controllers-central-1:/data ./central-backup`, puis redémarrer ; ouvrir la copie avec sqlite3/DB Browser. Tables : nodes, videos, extents, upload_requests, processed_events. Consulter `SELECT event_id,processed_at FROM processed_events;`. Ne pas ouvrir le volume en écriture depuis un second service.

Rapport des exécutions : [CONTROLLERS_DEPLOY_EVIDENCE](../../docs/CONTROLLERS_DEPLOY_EVIDENCE.md). Contrats et limites futures : [CONTROLLERS_CONTRACTS](../../docs/CONTROLLERS_CONTRACTS.md). Ceci démontre contrôle + stockage d'unités, sans lecture HLS/SIS ni streaming live/RTP.
