# Livraison contrÃ´leurs â€” preuves du 18 octobre 2026

Branche distincte : **codex/controllers-docker-lab**, issue de **codex/placement-edge-v1** Ã  `d8eea1b8ea5f5f05595da55ccaadd1b4b8ba42e4` (PR #1 draft, non mergÃ©e). La PR de livraison est stackÃ©e sur cette base ; son HEAD exact figure dans la description GitHub et dans le rapport local `outputs/controllers-2026-10-18/DELIVERY.md`. Aucun merge, aucune fermeture de PR #1, aucune modification de `main`, `containers` ou `video_streaming`. Audit : [CONTROLLERS_AUDIT.md](CONTROLLERS_AUDIT.md).

## ExÃ©cution rÃ©elle disponible

Windows PowerShell **7.6.5**, Docker client/Engine **29.8.2**, Compose **v5.6.0**, moteur Linux Docker Desktop. Go de build/tests : **go1.27.2 linux/amd64** (`golang:1.27.2`). Lancement initial de Docker Desktop nÃ©cessaire car il Ã©tait arrÃªtÃ©. Les exÃ©cutables de test Windows pouvant Ãªtre bloquÃ©s par Application Control, les neuf contrÃ´les Go ont Ã©tÃ© exÃ©cutÃ©s dans Linux Docker, indÃ©pendamment dans les trois modules.

```powershell
docker build -f deploy/lab/Dockerfile --target verify -t caribou/controllers-verify:dev .
# Ce target exÃ©cute sÃ©parÃ©ment, dans racine / serveur-zone / lab/placement :
# go test ./... ; go test -race ./... ; go vet ./...
.\deploy\lab\init.ps1
.\deploy\lab\start.ps1 -SkipInit
.\deploy\lab\smoke.ps1
.\deploy\lab\diagnostics.ps1
```

| ContrÃ´le | RÃ©sultat observÃ© |
|---|---|
| Racine : test / race / vet | **PASS / PASS / PASS** |
| serveur-zone : test / race / vet | **PASS / PASS / PASS**, TestGetNodeScansAllSixColumns inclus |
| lab/placement : test / race / vet | **PASS / PASS / PASS**, aucun fichier modifiÃ© dans ce banc |
| gofmt sur fichiers concernÃ©s + git diff --check | **PASS** ; les anciens internal/storage/storage*.go ne sont pas reformatÃ©s |
| Dockerfile Edge historique AMD64/ARM64 + Compose racine sans MQTT | **PASS après correctif** : copie des go.mod/go.sum du module local avant go mod download ; aucun changement de la CI legacy |
| Compose config --quiet / build / up --wait | **PASS**, exit codes vÃ©rifiÃ©s |
| Broker, Central, zone-a, zone-b, edge-a/b, agent-a/b | **PASS**, huit services healthy en real-http |
| Registre, deux zones, quota initial 1 048 576 bytes/nÅ“ud | **PASS**, snapshots authentifiÃ©s + heartbeat frais |
| Allocation locale | **PASS**, zÃ©ro appel OFFER distant mesurÃ© |
| CapacitÃ© locale insuffisante | **PASS**, OFFER puis RESERVE zone B, destinations choisies par Go |
| RÃ©servation expirÃ©e | **PASS**, dÃ©bit 64 bytes puis capacitÃ© restaurÃ©e aprÃ¨s expiration |
| Extent plus gros que chaque quota | **PASS**, refus HTTP 409 et rollback, aucun dÃ©passement |
| PREPARE_STORE | **PASS**, vidÃ©o non READY aprÃ¨s prÃ©paration |
| Mauvais SHA / longueur tronquÃ©e | **PASS**, HTTP 422, jamais READY aprÃ¨s ces Ã©critures |
| Une seule unitÃ© sur deux Ã©crite | **PASS**, vidÃ©o reste non READY |
| Deux unitÃ©s complÃ¨tes sur deux vrais agents | **PASS**, READY aprÃ¨s rÃ©ception des deux preuves rÃ©elles |
| Double publication MQTT STORE_COMPLETE | **PASS**, REPLAY_COMPLETE relit les fichiers puis republie les confirmations ; compteur processed_events inchangÃ© |
| Retained AVAILABLE sans heartbeat courant | **PASS**, pause >TTL, nouvelle publication retained, capacitÃ© Ã©ligible zone B = 0 |
| RedÃ©marrages Central + zones A/B + bridge A + vrai agent A | **PASS**, READY, compteur d'Ã©vÃ©nements et contenu disque prÃ©servÃ©s |
| MobilitÃ© A1 Aâ†’B puis retour A | **PASS**, node_id inchangÃ© et lookup joint sur la nouvelle zone, relecture des mÃªmes octets |
| Profil mock sÃ©parÃ© | **PASS** pour contrÃ´le ; preuves **MOCK**, vÃ©rification disque **SKIPPED** |
| Ancien verify.ps1 -Fresh | **PASS** 01â€“12, dont redÃ©marrage SQLite et rÃ©conciliation aprÃ¨s redÃ©marrage source ; banc ensuite arrÃªtÃ©, volumes conservÃ©s |
| Erreur backend disque / relecture corrompue / ledger non persistant | **PASS tests unitaires** avec backend HTTP injectÃ© (**MOCK d'erreurs**, aucune prÃ©tention disque plein matÃ©riel) ; aucun appel de confirmation |
| Anonymous MQTT / Ã©criture Ã©tat autre Edge / rÃ©ponse RPC usurpÃ©e | **PASS**, refus broker Not authorized ; contrÃ´le du motif MQTT, pas uniquement de l'exit code permissif de mosquitto_pub |
| export-edge.ps1 + upload.ps1 | **PASS**, seule identitÃ© B1 exportÃ©e ; fichier rÃ©el 200 bytes et preuve HTTP_SHA256_VERIFIED |
| Compose Edge autonome, rÃ©seau Docker sÃ©parÃ© | **PASS sur le mÃªme PC**, broker atteint via host.docker.internal et port publiÃ© ; PUT/GET/hash rÃ©els ; pas de test rÃ©seau physique externe |
| diagnostics.ps1 | **PASS**, Ã©tat/ports/logs/snapshot, sans valeurs secrÃ¨tes |
| PowerShell 5.1 / shell Linux physique | **NOT_TESTED** ; scripts Ã©crits compatibles PS5.1, exÃ©cution effective PS7.6.5 ; smoke.sh dÃ©lÃ¨gue Ã  la mÃªme suite pwsh |
| Raspberry Pi / Jetson / pare-feu et rÃ©seau du laboratoire | **NOT_TESTED**, matÃ©riel/IP rÃ©els indisponibles ; aucune rÃ¨gle pare-feu installÃ©e automatiquement |
| VOD complÃ¨te, HLS, live, SIS, RTP/RTX/WebRTC | **SKIPPED**, hors scope demandÃ© |

Dernier smoke real-http enregistrÃ© : `s1792346809052`. Deux unitÃ©s de **614 400 bytes**, vÃ©rifiÃ©es par `sha256sum` ET `stat -c %s` directement dans des volumes diffÃ©rents puis par GET aprÃ¨s redÃ©marrages :

| Destination | SHA-256 | Preuve |
|---|---|---|
| A1 / extent 0 | f0344bb266faa11ff40714ee285ec5d8d7e4ad43df6224e913b9b87e2818d8d6 | HTTP_SHA256_VERIFIED |
| B1 / extent 1 | f38cb3b9d33bf3e95fd8069ac4ff9bd883628a9336a4f94954be8b303360ab31 | HTTP_SHA256_VERIFIED |

La sonde du Compose autonome a ensuite Ã©crit sur B1 200 bytes SHA-256 `a48742653620d8aeee06f9aef53889980ead41a44aeb11c5b0996dfc4fbc127b`. Ses volumes B1 existants ont Ã©tÃ© rÃ©utilisÃ©s explicitement, aprÃ¨s arrÃªt du bridge/agent locaux, pour ne jamais attribuer une identitÃ© dÃ©jÃ  stockÃ©e Ã  un volume vide. Le Compose autonome a Ã©tÃ© arrÃªtÃ© et les huit services locaux remis healthy.

## Images hors ligne et provenance

Les cinq exÃ©cutables `central-controller`, `serveur-zone`, `edge-bridge`, `edge-agent`, `lab-admin` sont compilÃ©s avec `CGO_ENABLED=0`, GOOS=linux et GOARCH explicite. `verify-image.py` a inspectÃ© **les vrais ELF des archives exportÃ©es**, machine 62 AMD64 / 183 ARM64, et SHA-256 de chaque binaire. **PASS compilation/provenance ARM64 ; NOT_TESTED exÃ©cution physique ARM64**. Les petits RUN Alpine de construction peuvent Ãªtre Ã©mulÃ©s par Docker ; cela ne qualifie pas les processus Go sur un RPi.

```powershell
docker buildx build --platform linux/amd64,linux/arm64 -f deploy/lab/Dockerfile --target runtime -t caribou/controllers:dev -t caribou/controllers:2026-10-18 --load .
docker image save --platform linux/amd64 -o controllers-amd64.tar caribou/controllers:2026-10-18
docker image save --platform linux/arm64 -o controllers-arm64.tar caribou/controllers:2026-10-18
python deploy/lab/verify-image.py controllers-amd64.tar --expect amd64
python deploy/lab/verify-image.py controllers-arm64.tar --expect arm64
```

Index multiarchitecture : `sha256:18d37f1fc680fcd5701ef2d7549f31dc4c0e55478d38e5a1252f6027e6f851a8`.

| Plateforme | Manifest image | SHA-256 archive docker save |
|---|---|---|
| linux/amd64 | ea160044a03dee146c62fa9a321ccc3b437517c0d5075db3715bab2d7b2da2b9 | c2b7e77c3c24666770e3fc0d6a96564721131452a742f1f84cdf807c547b0a06 |
| linux/arm64 | 4b9d7923272207ea9fa83b580e2842ed9a52afc5da0ffb6209ee3d12f6b238b2 | 83c5956510289f8a495450854a023ca12b1981d29f8a3a38c6a23de47ba67fc1 |

Les archives et journaux locaux sont dans `outputs/controllers-2026-10-18/`, hors dÃ©pÃ´t : tests-three-modules.log, build-multiarch.log, smoke-real-http.log, smoke-mock.log, provenance-*.json, image-archives-sha256.json, snapshot-real-http.json, diagnostics.log, external-compose-probe.log, acl-probe.log. Aucune identitÃ© rÃ©elle n'est dans ces livrables ni les images.

## RÃ©seau et responsabilitÃ©s

DÃ©fauts : 127.0.0.1:1883 broker, :18088 Central, :18081/:18082 bridge Edge. Aucun port publiÃ© pour zones ni backend de stockage. LAN explicite `LAB_BIND_IP` + `-AllowLan`, credentials distincts, ACL gÃ©nÃ©rÃ©es sur IDs configurÃ©s. RÃ©seau local isolÃ© requis car MQTT/HTTP ne sont pas chiffrÃ©s. Les locators de laboratoire utilisent les IP des vrais Edge, pas le DNS interne Docker.

SQLite Central reste l'unique autoritÃ© des vidÃ©os/extents/processed_events ; registre SQLite de Dan local aux zones ; metadata/outbox et ledger Edge durables sans algorithme de placement concurrent. Toute erreur de persistance ferme l'admission ; restaurer les volumes complets et ne pas forcer READY. Taille max d'unitÃ© vÃ©rifiÃ©e 1 GiB, HTTP 30 s, bail configurable jusqu'Ã  600 s ; ne pas en dÃ©duire une qualification de gros flux mÃ©dia.

DÃ©fauts rencontrÃ©s et corrigÃ©s : getNode sÃ©lectionnait six colonnes pour quatre Scan ; fins de lignes Windows faisaient Ã©chouer le script broker (LF imposÃ©) ; enregistrement pÃ©riodique ne devait pas effacer la liveness fraÃ®che ; preuves MOCK refusÃ©es pour un nÅ“ud rÃ©el ; objet dÃ©rivÃ© par hash pour respecter les IDs de stockage ; save/fsync de la chaÃ®ne de rÃ©pertoires ; erreurs de stockage injectÃ©es sans confirmation. Un essai de sonde a Ã©galement demandÃ© de fournir Ã  nouveau les variables Compose lors du nettoyage ; la reprise/arrÃªt a ensuite rÃ©ussi. Aucun Ã©chec n'a Ã©tÃ© transformÃ© en PASS sans correction et rÃ©exÃ©cution correspondante.

La procÃ©dure complÃ¨te Â« Ce soir depuis Windows Â» / Â« Demain au labo Â», les commandes de prÃ©paration ARM et le diagnostic sont dans [deploy/lab/README.md](../deploy/lab/README.md). Contrats MQTT/HTTP effectifs et tableau P0/P1/P2 VOD/live/SIS/RTP : [CONTROLLERS_CONTRACTS.md](CONTROLLERS_CONTRACTS.md). L'absence des IP du laboratoire bloque uniquement la validation **physique distante**, pas le dÃ©marrage fonctionnel dÃ©montrÃ© sur PC.
