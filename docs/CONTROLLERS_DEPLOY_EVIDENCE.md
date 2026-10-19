# Livraison contrôleurs — preuves du 18 octobre 2026

Branche distincte : **codex/controllers-docker-lab**, issue de **codex/placement-edge-v1** à `d8eea1b8ea5f5f05595da55ccaadd1b4b8ba42e4` (PR #1 draft, non mergée). La PR de livraison est stackée sur cette base ; son HEAD exact figure dans la description GitHub et dans le rapport local `outputs/controllers-2026-10-18/DELIVERY.md`. Aucun merge, aucune fermeture de PR #1, aucune modification de `main`, `containers` ou `video_streaming`. Audit : [CONTROLLERS_AUDIT.md](CONTROLLERS_AUDIT.md).

## Exécution réelle disponible

Windows PowerShell **7.6.5**, Docker client/Engine **29.8.2**, Compose **v5.6.0**, moteur Linux Docker Desktop. Go de build/tests : **go1.27.2 linux/amd64** (`golang:1.27.2`). Lancement initial de Docker Desktop nécessaire car il était arrêté. Les exécutables de test Windows pouvant être bloqués par Application Control, les neuf contrôles Go ont été exécutés dans Linux Docker, indépendamment dans les trois modules.

```powershell
docker build -f deploy/lab/Dockerfile --target verify -t caribou/controllers-verify:dev .
# Ce target exécute séparément, dans racine / serveur-zone / lab/placement :
# go test ./... ; go test -race ./... ; go vet ./...
.\deploy\lab\init.ps1
.\deploy\lab\start.ps1 -SkipInit
.\deploy\lab\smoke.ps1
.\deploy\lab\diagnostics.ps1
```

| Contrôle | Résultat observé |
|---|---|
| Racine : test / race / vet | **PASS / PASS / PASS** |
| serveur-zone : test / race / vet | **PASS / PASS / PASS**, TestGetNodeScansAllSixColumns inclus |
| lab/placement : test / race / vet | **PASS / PASS / PASS**, aucun fichier modifié dans ce banc |
| Isolation des vues Central/STATUS Edge et unicité video_id | **PASS**, réponses sans référence mutable au ledger et requête conflictuelle sans altération de la vidéo existante |
| gofmt sur fichiers concernés + git diff --check | **PASS** ; les anciens internal/storage/storage*.go ne sont pas reformatés |
| Dockerfile Edge historique AMD64/ARM64 + Compose racine sans MQTT | **PASS après correctif** : copie des go.mod/go.sum du module local avant go mod download ; aucun changement de la CI legacy |
| Compose config --quiet / build / up --wait | **PASS**, exit codes vérifiés |
| Broker, Central, zone-a, zone-b, edge-a/b, agent-a/b | **PASS**, huit services healthy en real-http |
| Registre, deux zones, quota initial 1 048 576 bytes/nœud | **PASS**, snapshots authentifiés + heartbeat frais |
| Allocation locale | **PASS**, zéro appel OFFER distant mesuré |
| Capacité locale insuffisante | **PASS**, OFFER puis RESERVE zone B, destinations choisies par Go |
| Réservation expirée | **PASS**, débit 64 bytes puis capacité restaurée après expiration |
| Extent plus gros que chaque quota | **PASS**, refus HTTP 409 et rollback, aucun dépassement |
| PREPARE_STORE | **PASS**, vidéo non READY après préparation |
| Mauvais SHA / longueur tronquée | **PASS**, HTTP 422, jamais READY après ces écritures |
| Une seule unité sur deux écrite | **PASS**, vidéo reste non READY |
| Deux unités complètes sur deux vrais agents | **PASS**, READY après réception des deux preuves réelles |
| Double publication MQTT STORE_COMPLETE | **PASS**, REPLAY_COMPLETE relit les fichiers puis republie les confirmations ; compteur processed_events inchangé |
| Retained AVAILABLE sans heartbeat courant | **PASS**, pause >TTL, nouvelle publication retained, capacité éligible zone B = 0 |
| Redémarrages Central + zones A/B + bridge A + vrai agent A | **PASS**, READY, compteur d'événements et contenu disque préservés |
| Mobilité A1 A→B puis retour A | **PASS**, node_id inchangé et lookup joint sur la nouvelle zone, relecture des mêmes octets |
| Profil mock séparé | **PASS** pour contrôle ; preuves **MOCK**, vérification disque **SKIPPED** |
| Ancien verify.ps1 -Fresh | **PASS** 01–12, dont redémarrage SQLite et réconciliation après redémarrage source ; banc ensuite arrêté, volumes conservés |
| Erreur backend disque / relecture corrompue / ledger non persistant | **PASS tests unitaires** avec backend HTTP injecté (**MOCK d'erreurs**, aucune prétention disque plein matériel) ; aucun appel de confirmation |
| Anonymous MQTT / écriture état autre Edge / réponse RPC usurpée | **PASS**, refus broker Not authorized ; contrôle du motif MQTT, pas uniquement de l'exit code permissif de mosquitto_pub |
| export-edge.ps1 + upload.ps1 | **PASS**, seule identité B1 exportée ; fichier réel 200 bytes et preuve HTTP_SHA256_VERIFIED |
| Compose Edge autonome, réseau Docker séparé | **PASS sur le même PC**, broker atteint via host.docker.internal et port publié ; PUT/GET/hash réels ; pas de test réseau physique externe |
| diagnostics.ps1 | **PASS**, état/ports/logs/snapshot, sans valeurs secrètes |
| PowerShell 5.1 / shell Linux physique | **NOT_TESTED** ; scripts écrits compatibles PS5.1, exécution effective PS7.6.5 ; smoke.sh délègue à la même suite pwsh |
| Raspberry Pi / Jetson / pare-feu et réseau du laboratoire | **NOT_TESTED**, matériel/IP réels indisponibles ; aucune règle pare-feu installée automatiquement |
| VOD complète, HLS, live, SIS, RTP/RTX/WebRTC | **SKIPPED**, hors scope demandé |

Dernier smoke real-http enregistré : `s1792348029116`. Deux unités de **614 400 bytes**, vérifiées par `sha256sum` ET `stat -c %s` directement dans des volumes différents puis par GET après redémarrages :

| Destination | SHA-256 | Preuve |
|---|---|---|
| A1 / extent 0 | f0344bb266faa11ff40714ee285ec5d8d7e4ad43df6224e913b9b87e2818d8d6 | HTTP_SHA256_VERIFIED |
| B1 / extent 1 | f38cb3b9d33bf3e95fd8069ac4ff9bd883628a9336a4f94954be8b303360ab31 | HTTP_SHA256_VERIFIED |

La sonde du Compose autonome a ensuite écrit sur B1 200 bytes SHA-256 `a48742653620d8aeee06f9aef53889980ead41a44aeb11c5b0996dfc4fbc127b`. Ses volumes B1 existants ont été réutilisés explicitement, après arrêt du bridge/agent locaux, pour ne jamais attribuer une identité déjà stockée à un volume vide. Le Compose autonome a été arrêté et les huit services locaux remis healthy.

## Images hors ligne et provenance

Les cinq exécutables `central-controller`, `serveur-zone`, `edge-bridge`, `edge-agent`, `lab-admin` sont compilés avec `CGO_ENABLED=0`, GOOS=linux et GOARCH explicite. `verify-image.py` a inspecté **les vrais ELF des archives exportées**, machine 62 AMD64 / 183 ARM64, et SHA-256 de chaque binaire. **PASS compilation/provenance ARM64 ; NOT_TESTED exécution physique ARM64**. Les petits RUN Alpine de construction peuvent être émulés par Docker ; cela ne qualifie pas les processus Go sur un RPi.

```powershell
docker buildx build --platform linux/amd64,linux/arm64 -f deploy/lab/Dockerfile --target runtime -t caribou/controllers:dev -t caribou/controllers:2026-10-18 --load .
docker image save --platform linux/amd64 -o controllers-amd64.tar caribou/controllers:2026-10-18
docker image save --platform linux/arm64 -o controllers-arm64.tar caribou/controllers:2026-10-18
python deploy/lab/verify-image.py controllers-amd64.tar --expect amd64
python deploy/lab/verify-image.py controllers-arm64.tar --expect arm64
```

Index multiarchitecture : `sha256:10023d86ab128d8c70dcb2c93ffb56dd4052b4a1a882c53d03a52fb0b8e29f4a`.

| Plateforme | Manifest image | SHA-256 archive docker save |
|---|---|---|
| linux/amd64 | 60fa14ad8b1cf070e98ff20dfbd1ceffd02416f84a60c5b5c188ae8d36df91d1 | d19f418674b992e66025adc855a153ecb93b4cc3aedb40327d9019f533b1b7b5 |
| linux/arm64 | 12556382bdcbfe9f7fed0bf73ab9de7af7d679b743f48a5b3322e06e287f9741 | a3ddfad9c19b179e970dd033d2784172ca2538185593d0a689d83f5288c2da1f |

Les archives et journaux locaux sont dans `outputs/controllers-2026-10-18/`, hors dépôt : tests-three-modules.log, build-multiarch.log, smoke-real-http.log, smoke-mock.log, provenance-*.json, image-archives-sha256.json, snapshot-real-http.json, diagnostics.log, external-compose-probe.log, acl-probe.log. Aucune identité réelle n'est dans ces livrables ni les images.

## Réseau et responsabilités

Défauts : 127.0.0.1:1883 broker, :18088 Central, :18081/:18082 bridge Edge. Aucun port publié pour zones ni backend de stockage. LAN explicite `LAB_BIND_IP` + `-AllowLan`, credentials distincts, ACL générées sur IDs configurés. Réseau local isolé requis car MQTT/HTTP ne sont pas chiffrés. Les locators de laboratoire utilisent les IP des vrais Edge, pas le DNS interne Docker.

SQLite Central reste l'unique autorité des vidéos/extents/processed_events ; registre SQLite de Dan local aux zones ; metadata/outbox et ledger Edge durables sans algorithme de placement concurrent. Toute erreur de persistance ferme l'admission ; restaurer les volumes complets et ne pas forcer READY. Taille max d'unité vérifiée 1 GiB, HTTP 30 s, bail configurable jusqu'à 600 s ; ne pas en déduire une qualification de gros flux média.

La première CI GitHub a révélé un échec du Dockerfile Edge historique : `/src/lab/placement/go.mod` absent lors de go mod download. Le correctif copie les métadonnées du module avant le téléchargement ; build legacy AMD64/ARM64 et Compose racine ont ensuite été rejoués avec succès. Les binaires des contrôleurs restent identiques (aucun code Go changé par ce correctif). Dernier état GitHub dans ci-status.json.

Défauts rencontrés et corrigés : getNode sélectionnait six colonnes pour quatre Scan ; fins de lignes Windows faisaient échouer le script broker (LF imposé) ; enregistrement périodique ne devait pas effacer la liveness fraîche ; preuves MOCK refusées pour un nœud réel ; objet dérivé par hash pour respecter les IDs de stockage ; save/fsync de la chaîne de répertoires ; erreurs de stockage injectées sans confirmation. Un essai de sonde a également demandé de fournir à nouveau les variables Compose lors du nettoyage ; la reprise/arrêt a ensuite réussi. Aucun échec n'a été transformé en PASS sans correction et réexécution correspondante.

La procédure complète « Ce soir depuis Windows » / « Demain au labo », les commandes de préparation ARM et le diagnostic sont dans [deploy/lab/README.md](../deploy/lab/README.md). Contrats MQTT/HTTP effectifs et tableau P0/P1/P2 VOD/live/SIS/RTP : [CONTROLLERS_CONTRACTS.md](CONTROLLERS_CONTRACTS.md). L'absence des IP du laboratoire bloque uniquement la validation **physique distante**, pas le démarrage fonctionnel démontré sur PC.

Les vues Central et STATUS Edge possèdent leurs propres unités/maps afin que leur encodage hors verrou ne partage pas de données mutables. Une seconde request_id ne peut pas réutiliser une video_id et masquer la vidéo déjà cataloguée. Régressions ajoutées, trois modules revérifiés et les deux profils rejoués ; images ci-dessus réexportées après ces correctifs.
