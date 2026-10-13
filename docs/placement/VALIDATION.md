# Validation réellement exécutée — 13 octobre 2026

Environnement : Windows, Docker Desktop 4.95.0, daemon Linux amd64 Docker 29.8.2,
Compose 5.6.0, Go 1.27.2. Aucun test sur machine ARM physique.

| Commande / vérification | Résultat observé |
|---|---|
| `gofmt -l pkg/placement lab/placement serveur-zone/app` | PASS, sortie vide |
| Formatage des blobs Git Go historiques | PASS ; le checkout Windows ajoute CRLF, sans défaut dans les blobs Git |
| `go test ./...` racine, laboratoire, serveur-zone | PASS, exécuté dans Linux |
| `go test -race ./...` dans ces trois modules | PASS |
| `go vet ./...` dans ces trois modules | PASS |
| 16 tests Go ajoutés + tests storage existants | PASS |
| `docker compose --profile placement config --quiet` | PASS |
| `./lab/placement/verify.ps1 -Fresh` | PASS, douze contrôles E2E ci-dessous |
| Build des deux exécutables `linux/amd64,linux/arm64` | PASS |
| Inspection des fichiers ELF dans l'archive OCI | PASS : e_machine 62 en amd64, 183 en arm64, pour chaque exécutable |
| Exécution de `verify.sh` sur un hôte Linux | SKIP localement ; script fourni et branché dans la CI dédiée |
| Raspberry Pi / Jetson physique, média, SIS, RTP/RTX/WebRTC | SKIP : matériel absent / modules hors périmètre |

Commande des trois suites Go :

```sh
docker build -f lab/placement/Dockerfile --target verify -t cariboutv-placement-verify .
```

Les tests couvrent manifests invalides et overflow, tailles variables et segment
dépassant la cible, HRW/déterminisme, distribution sur 10 000 tirages, manifest
de 100 000 segments, capacité, snapshot/heartbeat périmé, concurrence, OFFER
initial simultané et extension à B/C/D, réponse RESERVE perdue suivie de RELEASE,
expiration/tombstones, générations de mobilité, course snapshot/horloge appelant,
reprise du checkpoint, disque défaillant avec admission fermée, strict JSON et
coalescing, transactions/rollback SQLite injecté au deuxième extent, redelivery,
annulation, persistance et réconciliation de requête.

Sortie des scénarios observés :

```text
PASS 01 local-only placement, zero remote OFFER, READY only after simulated completion
PASS 02 local insufficiency uses remote OFFER/RESERVE and remote HRW
PASS 03 combined capacity insufficient: explicit error and reservation rollback
PASS 04 DEGRADED fallback; UNAVAILABLE and stale heartbeat rejected
PASS 05 concurrent reservations fit capacity; repeated RELEASE is idempotent
PASS 06 duplicate upload and STORE commands do not double-commit
PASS 07 central restart preserves exact authoritative catalog
PASS 08 A1 moves A to B; extents still reference A1 and resolve through current node zone
PASS 09 expired reservation restores project capacity
PASS 10 100 extents in one command; 2 aggregate summary publications
PASS 11 zone restart restores durable debits and waits for fresh node liveness
PASS 12 source restart reconciles immutable request without reallocation or new event
```

Les essais initiaux n'ont pas tous réussi : Go/Docker absents du PATH, downloads
bloqués dans le sandbox, exécution de certains tests Windows refusée par la
politique Application Control. Les suites ont donc été exécutées dans Linux,
sans présenter les tentatives Windows comme des PASS.

Les reprises E2E ont permis de corriger une course réelle (snapshot reçu après
l'horodatage capturé au début de l'upload, couverte par un test de régression),
et deux hypothèses trop rapides du scénario (résumé voisin pas encore reçu,
nœud mobile pas encore admis au moment de comparer les capacités).
Le build multiarch a été corrigé pour utiliser les ARG automatiques de Buildx ;
les architectures ont ensuite été vérifiées dans les vrais exécutables ELF.

La CI GitHub est configurée par `.github/workflows/placement.yml` sans modifier
le workflow existant des collègues. Son résultat distant doit être distingué
de ces vérifications locales ; aucune réussite distante n'est supposée.
