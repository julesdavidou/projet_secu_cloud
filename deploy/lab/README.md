# ContrÃ´leurs CaribouTV â€” PC Docker â†’ laboratoire

Les vrais processus sont `central-controller`, `serveur-zone` de Dan (entrÃ©e `app/allocator.go`) et, par Edge, `edge-bridge` + le vÃ©ritable `cmd/edge-agent` de main. HRW, extents, coordinateur, rÃ©servations, catalogue SQLite et bus MQTT5 de la PR #1 sont rÃ©utilisÃ©s. Aucun placement n'est calculÃ© dans les scripts clients. Le banc historique `lab/placement` reste intact.

## Ce soir depuis Windows

PrÃ©requis : Docker Desktop dÃ©marrÃ© en mode conteneurs Linux, Docker Compose avec `up --wait`, PowerShell 5.1 ou 7. PremiÃ¨re construction : accÃ¨s Internet aux images et modules Go. Aucun Go Ã  installer sur le PC. Depuis la racine du dÃ©pÃ´t sur `codex/controllers-docker-lab` :

```powershell
.\deploy\lab\start.ps1
.\deploy\lab\smoke.ps1
.\deploy\lab\diagnostics.ps1
```

`start.ps1` construit l'image, provisionne des mots de passe distincts, valide Compose et attend la santÃ© des huit services. Le dÃ©faut est **local, real-http**, sur loopback. La topologie de dÃ©monstration autorise 1 MiB par Edge afin de forcer facilement le passage de A vers B. Les deux vrais agents possÃ¨dent des volumes de fichiers indÃ©pendants. Le smoke nÃ©cessite cette topologie et des volumes neufs ; il Ã©crit environ 1,2 MiB, redÃ©marre les contrÃ´leurs/un Edge et teste sa mobilitÃ©. Il s'arrÃªte au premier Ã©chec.

Pour rejouer le smoke, effacer explicitement **les seules donnÃ©es de cette dÃ©monstration** :

```powershell
.\deploy\lab\reset-demo.ps1 -DeleteTestVolumes
.\deploy\lab\start.ps1 -SkipInit
.\deploy\lab\smoke.ps1
```

Pour arrÃªter en conservant les donnÃ©es : `.\deploy\lab\reset-demo.ps1`. Ne jamais utiliser `docker system prune` pour ce banc. IdentitÃ©s et jetons restent dans `.runtime/`, ignorÃ© par Git et exclu du build Docker. Ne pas committer ou partager ce dossier ; rÃ©server l'accÃ¨s au compte utilisateur du PC.

Mode simulÃ©, volontairement sÃ©parÃ© (destruction des donnÃ©es de test explicite avant changement de mode) :

```powershell
.\deploy\lab\reset-demo.ps1 -DeleteTestVolumes
.\deploy\lab\init.ps1 -Mode mock
.\deploy\lab\start.ps1 -SkipInit
.\deploy\lab\smoke.ps1
# Retour aux fichiers rÃ©els : reset-demo -DeleteTestVolumes, init -Mode real-http, start -SkipInit.
```

En `mock`, aucun processus agent de fichiers n'est lancÃ©. `MOCK_STORE` est rÃ©servÃ© Ã  ce mode ; les preuves sont `MOCK`. En `real-http`, il est refusÃ©. Le mÃªme mot `READY` du catalogue historique existe dans les deux bancs : vÃ©rifier **le mode de configuration ET la preuve de chaque unitÃ©**. Seule `HTTP_SHA256_VERIFIED` qualifie le chemin avec vrais octets.

## Configuration et stockage

Copier `.env.example` vers `.env` (automatique au premier dÃ©marrage). `.env` contrÃ´le interfaces, ports, slots Compose et image. `config/topology.local.json` dÃ©finit zones, identitÃ©s, quota, endpoint annoncÃ©, zones autorisÃ©es Ã  la mobilitÃ© et temporisations. Le provisionnement produit `.runtime/config.json` et les ACL exactes ; aprÃ¨s modification, relancer `init.ps1`, puis `start.ps1 -SkipInit`. Ne pas changer une identitÃ©, un quota ou un endpoint sur des volumes actifs sans rÃ©conciliation ; pour la dÃ©mo, arrÃªter et rÃ©initialiser ses volumes explicitement.

| Valeur | Source effective | Sens |
|---|---|---|
| IdentitÃ© Edge | `EDGE_ID` = un `nodes[].id` provisionnÃ© | Stable Ã  travers les redÃ©marrages et dÃ©placements |
| Zone initiale | `nodes[].zone` ; `ZONE_ID` sur serveur-zone | L'Edge restaure ensuite sa zone/gÃ©nÃ©ration depuis son ledger |
| Quota projet | `nodes[].quota_bytes` | Ã‰quivalent demandÃ© de PROJECT_QUOTA_BYTES ; la source unique est le JSON, aucune dÃ©duction automatique depuis le disque |
| Broker bridge | `MQTT_BROKER=mqtt://IP_PC:1883` | MQTT5, credentials montÃ©s sous SECRETS_DIR |
| Ancien Edge Go | `EDGE_ZONE`, `STORAGE_ROOT`, `HTTP_AUTH_TOKEN_FILE` | Alias legacy conservÃ©s ; son MQTT legacy est dÃ©sactivÃ© dans ce dÃ©ploiement |
| Annonce HTTP | `nodes[].endpoint` | Adresse de l'**Edge**, joignable depuis le client qui enverra les fichiers |
| TTL / lease | `node_ttl_seconds` / `lease_seconds` | 5/30 s dans la dÃ©mo ; jusqu'Ã  60/600 s, exemple labo 300 s conseillÃ© pour transferts lents |
| Central | DB_PATH / CENTRAL_DB_PATH | `/data/central.db`, unique writer SQLite des placements |
| Zones | ZONE_DB_PATH / LEDGER_PATH | Registre de Dan + ledger de rÃ©servations, responsabilitÃ©s locales |

Le quota net = quota autorisÃ© âˆ’ fichiers STORED âˆ’ rÃ©servations actives non expirÃ©es. Les IDs de rÃ©servations dÃ©jÃ  dÃ©duites sont annoncÃ©s dans `included_reservation_ids`, ce qui empÃªche une seconde dÃ©duction par le scheduler. Le disque libre physique demeure une mesure indÃ©pendante. Un fichier STORED reste dÃ©bitÃ© aprÃ¨s expiration du bail. Pas de GC ni de suppression mÃ©tier automatique dans cette version ; ne pas retirer manuellement des fichiers puis annoncer une capacitÃ© rÃ©cupÃ©rÃ©e.

`PREPARE_STORE` rÃ©serve seulement. Chaque unitÃ© contient une taille et un SHA-256 attendus immuables. HTTP transmet les octets au vrai `internal/storage` : fichier temporaire imprÃ©visible, contrÃ´le longueur/hash, fsync, rename, fsync des rÃ©pertoires ; le bridge relit et revÃ©rifie ensuite le fichier. Il persiste STORED avant de notifier Central ; les notifications non confirmÃ©es sont rÃ©essayÃ©es aprÃ¨s redÃ©marrage. Aucune confirmation mÃ©tier aprÃ¨s erreur de fichier, hash ou ledger. Un fichier Ã©crit avant un Ã©chec tardif peut Ãªtre orphelin, sans faux READY.

Central conserve Ã©galement `control-metadata.json` dans **son mÃªme volume** : hashes attendus, endpoints, Ã©tat de transport et outbox durable. Les placements et Ã©tats de stockage restent dans le catalogue SQLite existant. Sauvegarder/restaurer ensemble tout le volume Central (DB, WAL Ã©ventuel et mÃ©tadonnÃ©es) et les volumes Edge. Un Ã©vÃ©nement outbox impossible Ã  rÃ©concilier fait Ã©chouer explicitement le dÃ©marrage ; ne pas effacer l'outbox pour forcer READY.

## Demain au labo

Il faut Docker Engine/Compose sur chaque hÃ´te, un OS Linux **64 bits ARM64** sur les RPi/Jetson et un rÃ©seau isolÃ© oÃ¹ PC et Edge se joignent. Pas d'exÃ©cution physique ARM validÃ©e ce soir. JetPack, CUDA et accÃ©lÃ©ration vidÃ©o sont hors scope. Synchroniser les horloges (NTP, `timedatectl status`) : les snapshots et baux utilisent des dates absolues.

1. Sur le PC, arrÃªter la dÃ©mo locale. Copier `config/topology.lab-ready.example.json` vers `config/topology.lab-ready.json`. Remplacer **chaque** adresse `192.0.2.*` par l'IP rÃ©elle de l'Edge concernÃ©. Ce prÃ©fixe est un exemple non routable. Ajuster IDs, zones, quota projet et `lease_seconds` selon les transferts. Les endpoints doivent Ãªtre des IP LAN des Edge, jamais `broker`, `agent-a` ni `localhost`.
2. Dans `.env`, mettre `LAB_BIND_IP=IP_LAN_DU_PC` et `LAB_PC_ADDRESS=IP_LAN_DU_PC`. Garder les ports ou les changer explicitement. Les noms des deux slots Zone (`ZONE_A_ID`, `ZONE_B_ID`) doivent correspondre aux zones JSON. La logique accepte d'autres IDs et jusqu'Ã  six zones ; ajouter des services Compose identiques si nÃ©cessaire. Les listes A1/B1 existent uniquement dans les exemples et le smoke, pas dans les handlers.
3. Depuis PowerShell PC (le reset ci-dessous efface les seules données de démonstration de ce projet ; sauvegarder auparavant si nécessaire) :

```powershell
.\deploy\lab\reset-demo.ps1
# Si passage depuis la dÃ©mo vers une nouvelle topologie :
# reset-demo.ps1 -DeleteTestVolumes efface seulement les volumes du projet de dÃ©mo.
.\deploy\lab\start.ps1 -Profile lab-ready -AllowLan
.\deploy\lab\export-edge.ps1 -Id A1 -Destination "$PWD\edge-A1-private"
.\deploy\lab\export-edge.ps1 -Id B1 -Destination "$PWD\edge-B1-private"
```

Le profil lab-ready lance uniquement broker, Central et deux serveurs-zone sur le PC. `-AllowLan` est un opt-in explicite. Les scripts ne modifient pas le pare-feu. Si des donnÃ©es de dÃ©mo persistent, les sauvegarder ou les supprimer explicitement avant le changement de topologie. Exporter chaque dossier privÃ© **uniquement sur l'Edge correspondant**, par clÃ© USB privÃ©e ou SSH. Le dossier contient son mot de passe MQTT, son jeton HTTP et le JSON public ; jamais les secrets central/admin/autres Edge.

4. Depuis le dossier privÃ© copiÃ© sur le RPi/Jetson, Bash (remplacer les marqueurs par les vraies valeurs) :

```bash
# PrÃ©alablement : docker load -i controllers-arm64.tar
export CONTROLLERS_IMAGE=caribou/controllers:2026-10-18
export EDGE_ID=A1
export LAB_PC_ADDRESS=IP_LAN_DU_PC
export MQTT_PORT=1883
export EDGE_BIND_IP=IP_LAN_DE_CET_EDGE
export EDGE_HTTP_PORT=18081
# Les fichiers copiÃ©s depuis Windows doivent rester privÃ©s et lisibles du groupe du conteneur.
chmod 700 edge-config
sudo chown -R "$(id -u):10001" edge-config/identity
chmod 750 edge-config/identity
chmod 640 edge-config/identity/*
docker compose -f compose.edge.yaml config --quiet
docker compose -f compose.edge.yaml up -d --wait --wait-timeout 120
docker compose -f compose.edge.yaml ps
curl --fail "http://$EDGE_BIND_IP:$EDGE_HTTP_PORT/ready"
```

`EDGE_ID` doit correspondre au dossier et au JSON. Sur le deuxiÃ¨me Edge, exporter B1 et sa propre IP. Le broker est l'IP LAN du PC, **jamais localhost depuis une machine distante**. Les deux rÃ©seaux Docker de deux hÃ´tes ne sont pas interconnectÃ©s : seuls les ports publiÃ©s sur leurs IP LAN transportent MQTT/HTTP.

5. Pare-feu/routage : autoriser TCP MQTT_PORT sur le PC depuis les seules IP des Edge ; le port Central depuis le seul poste opÃ©rateur si nÃ©cessaire ; sur chaque Edge, EDGE_HTTP_PORT depuis le PC/client. VÃ©rifier les VLAN/Wi-Fi isolÃ©s, les adresses DHCP et le profil rÃ©seau Windows. Exemple **Ã  adapter puis exÃ©cuter dans PowerShell administrateur**, sans ouvrir toutes les machines du campus :

```powershell
# $pcIp et $edgeIps doivent contenir les IP rÃ©elles, pas 192.0.2.*.
New-NetFirewallRule -DisplayName 'Caribou MQTT labo' -Direction Inbound -Action Allow -Protocol TCP -LocalAddress $pcIp -LocalPort 1883 -RemoteAddress $edgeIps -Profile Private
# Retrait aprÃ¨s la sÃ©ance : Remove-NetFirewallRule -DisplayName 'Caribou MQTT labo'
Test-NetConnection -ComputerName $pcIp -Port 1883
Test-NetConnection -ComputerName $edgeIps[0] -Port 18081
```

Sur Linux, vÃ©rifier `ip -br addr`, `ip route`, `nc -vz IP_PC 1883`. Ne pas recopier des rÃ¨gles ufw/firewalld d'une machine inconnue. MQTT/HTTP sont authentifiÃ©s mais **non chiffrÃ©s dans ce banc** ; utiliser uniquement un rÃ©seau local isolÃ© et de confiance. TLS/VPN est un prÃ©requis avant un rÃ©seau partagÃ© non fiable. Les mutations et consultations du catalogue exigent un jeton, les Ã©critures MQTT des ACL exactes ; les endpoints santÃ© sont publics.

6. Depuis le PC, envoyer de petits segments synthÃ©tiques sans changer le placement Go :

```powershell
[IO.File]::WriteAllBytes("$PWD\segment0.bin", [byte[]](1..200))
[IO.File]::WriteAllBytes("$PWD\segment1.bin", [byte[]](201..250))
.\deploy\lab\upload.ps1 -SourceNodeId A1 -Files @("$PWD\segment0.bin", "$PWD\segment1.bin")
.\deploy\lab\diagnostics.ps1
```

Ce client demande un extent par segment positif (`target_size_bytes=1`), reÃ§oit le plan Go, puis utilise exclusivement ses endpoints. Il n'exÃ©cute ni HRW ni stratÃ©gie de secours. Le rÃ©sultat doit Ãªtre READY avec toutes les preuves HTTP_SHA256_VERIFIED. Un petit upload peut rester dans A ; pour solliciter B, choisir plusieurs unitÃ©s compatibles avec son quota dont le total dÃ©passe la capacitÃ© restante de A. Ne pas dÃ©passer 1 GiB par unitÃ© ; privilÃ©gier des unitÃ©s petites qui terminent avant le bail et les timeouts HTTP de 30 s.

## Images et fonctionnement hors ligne

```powershell
# Depuis la racine du dÃ©pÃ´t, avec Internet ce soir :
docker buildx build --platform linux/amd64,linux/arm64 -f deploy/lab/Dockerfile --target runtime -t caribou/controllers:2026-10-18 --load .
docker image save --platform linux/arm64 -o controllers-arm64.tar caribou/controllers:2026-10-18
docker image save --platform linux/amd64 -o controllers-amd64.tar caribou/controllers:2026-10-18
# Sur la machine cible : docker load -i controllers-arm64.tar
```

Docker Desktop avec magasin containerd accepte le chargement multiarchitecture. Sur un Engine plus ancien, construire/sauvegarder une architecture Ã  la fois avec `--platform linux/arm64 --load`. L'image contient les cinq exÃ©cutables distincts, sans secrets, CGO_ENABLED=0 ; aucun compilateur n'est requis sur les Edge. Le PC doit aussi avoir l'image `eclipse-mosquitto:2.0.22` dÃ©jÃ  chargÃ©e (`docker save`/`load` si PC dÃ©placÃ©). Les contrÃ´leurs dÃ©jÃ  construits se dÃ©marrent avec `start.ps1 -SkipInit` ; provisionnement sans reconstruction : `init.ps1 -SkipBuild`.

Bash alternatif pour un PC Linux :

```bash
sh deploy/lab/init.sh real-http
sh deploy/lab/start.sh local
sh deploy/lab/smoke.sh # mÃªme suite, nÃ©cessite PowerShell 7 (pwsh) sur le poste de test
# Labo : adapter .env et config/topology.lab-ready.json, puis
sh deploy/lab/init.sh real-http config/topology.lab-ready.json
ALLOW_LAN=1 sh deploy/lab/start.sh lab-ready
```

`smoke.sh` annonce NOT_TESTED et sort non-zÃ©ro si pwsh manque ; il n'invente pas une seconde suite validÃ©e. Aucun PowerShell n'est nÃ©cessaire pour faire fonctionner les conteneurs Linux distants.

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

| SymptÃ´me | VÃ©rification / correction |
|---|---|
| Docker indisponible | DÃ©marrer Docker Desktop Linux ; `docker info` |
| Broker crash `illegal option` | Fins de lignes LF du script sh ; `.gitattributes` les impose |
| Broker healthy, bridge unhealthy | Logs du bridge : credentials, ACL, identitÃ©, zone/endpoint/quota non provisionnÃ©s, horloge en avance |
| Liveness healthy mais ready 503 | MQTT ou Central non joignable ; health ne prouve pas la readiness |
| Aucun nÅ“ud Ã©ligible | Attendre heartbeat frais, vÃ©rifier TTL/NTP ; retained AVAILABLE seul ne suffit pas |
| API 401 | Jeton manquant/erronÃ© ; ne pas dÃ©sactiver l'authentification pour dÃ©panner |
| Upload 409 | Source non enregistrÃ©e, capacitÃ© insuffisante, extent trop grand ou rÃ©servation rejetÃ©e ; pas d'allocateur de secours |
| HTTP 422 | Longueur/hash, unitÃ© non prÃ©parÃ©e ou bail expirÃ© ; nouvel upload avec un autre request_id aprÃ¨s correction |
| HTTP 5xx / stockage indisponible | Logs agent/bridge, montage /data, permissions, quota/disque ; aucun READY de secours |
| Labo MQTT OK mais HTTP KO | Endpoint annoncÃ© IP/port de l'Edge, bind et pare-feu ; ne pas utiliser le DNS Docker du PC |
| Quota plus bas aprÃ¨s test | Fichiers STORED rÃ©ellement comptÃ©s ; inspecter snapshot/ledger, reset explicite de dÃ©mo seulement |
| Central ne restaure pas | Sauvegarder tout le volume et lire l'erreur outbox/JSON/SQLite ; ne jamais supprimer arbitrairement le catalogue |

Inspection fichier rÃ©elle : `docker compose -f deploy/lab/compose.yaml exec agent-a find /data/tenants/project/objects -type f`, puis `sha256sum` et `stat -c %s` dans le conteneur. Le smoke exÃ©cute ces contrÃ´les. Pour SQLite et les Ã©vÃ©nements, copie cohÃ©rente : arrÃªter Central, `docker cp caribou-controllers-central-1:/data ./central-backup`, puis redÃ©marrer ; ouvrir la copie avec sqlite3/DB Browser. Tables : nodes, videos, extents, upload_requests, processed_events. Consulter `SELECT event_id,processed_at FROM processed_events;`. Ne pas ouvrir le volume en Ã©criture depuis un second service.

Rapport des exÃ©cutions : [CONTROLLERS_DEPLOY_EVIDENCE](../../docs/CONTROLLERS_DEPLOY_EVIDENCE.md). Contrats et limites futures : [CONTROLLERS_CONTRACTS](../../docs/CONTROLLERS_CONTRACTS.md). Ceci dÃ©montre contrÃ´le + stockage d'unitÃ©s, sans lecture HLS/SIS ni streaming live/RTP.
