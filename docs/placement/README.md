# Placement / adressage Edge V1

Le point d'entrée demandé est **`serveur-zone/app/allocator.go`**. Le serveur de
Dan provenant de `containers` (`3a56360cd3eb1722dd800dccddf09247c4f3378f`) reste
le processus Zone Controller du banc Docker. L'allocateur utilise le paquet pur
[`pkg/placement`](../../pkg/placement) et l'adaptateur MQTT5 de référence
[`lab/placement`](../../lab/placement). La branche de travail part du vrai `main`
`80a8d45f73569f86b14bf730be5eecd43880d157` ; les deux histoires Git indépendantes
n'ont pas été fusionnées. La livraison est rebasée sur le main suivant, `7860a4aac90c085e2c5bb281cf2aca6105d95532`, qui rend MQTT optionnel dans l'Edge Agent et retire son broker Compose. Cette évolution est conservée intégralement ; notre broker est uniquement celui du laboratoire. Les fichiers de l'Edge Agent restent inchangés par notre implémentation.

Ce qui fonctionne réellement : validation des manifests, extents variables,
HRW local, réservations atomiques et durables, échanges MQTT5 authentifiés,
placement inter-zone, transactions et persistance SQLite, idempotence et
résolution de mobilité par jointure node→zone. **Le stockage des octets médias
est simulé** : les mocks envoient des STORE_COMPLETE sans recevoir de vidéo.
READY dans ce laboratoire atteste uniquement les résultats de ces mocks.

## Reproduire le banc (Docker Desktop ou Docker Engine + Compose)

Depuis la racine du dépôt, PowerShell :

```powershell
./lab/placement/verify.ps1 -Fresh
```

Linux/macOS :

```sh
sh lab/placement/verify.sh --fresh
```

`-Fresh` / `--fresh` supprime uniquement les données du projet de simulation
`cariboutv-placement`. À utiliser pour rejouer les dix scénarios avec les mêmes
capacités initiales. Sans cette option les volumes sont conservés ; le scénario
exige néanmoins un banc frais. Aucun port MQTT/HTTP du laboratoire n'est publié
sur l'hôte. Aucun Edge réel n'est contacté. Le réseau Docker est interne.

Commandes détaillées, depuis `lab/placement` :

```sh
cp .env.example .env
docker compose --profile placement config --quiet
docker compose --profile placement up -d --build --wait broker central zone-a zone-b a1 a2 b1 b2
docker compose --profile placement run --rm --no-deps --build scenario scenario
docker compose --profile placement run --rm --no-deps scenario snapshot
docker compose --profile placement restart central
docker compose --profile placement up -d --wait central
docker compose --profile placement run --rm --no-deps scenario snapshot
docker compose --profile placement logs --no-color
docker compose --profile placement down
```

Les deux snapshots doivent être identiques après restart. Le script les compare
automatiquement. `down` conserve les volumes ; `down -v` efface seulement les
données de cette simulation. Les mots de passe d'exemple sont exclusivement des
identifiants de démonstration, copiés dans un `.env` ignoré par Git.

Un manifest personnalisé peut être soumis sans simuler de transfert :

```sh
docker compose --profile placement run --rm --no-deps scenario upload /src-example.json
```

Il faut monter son fichier dans le conteneur (`run -v /chemin/manifest.json:/src-example.json:ro`).
Le format complet est dans [`example-upload.json`](../../lab/placement/example-upload.json).
Cette commande renvoie un plan RESERVED, pas une preuve de stockage.

## Tests et builds

```sh
docker build -f lab/placement/Dockerfile --target verify .
```

Cette cible vérifie gofmt, puis exécute `go test ./...`, `go test -race ./...` et
`go vet ./...` dans **chacun des trois modules** : racine, lab/placement et
serveur-zone. Un `go test ./...` depuis la racine ne traverse pas les modules
imbriqués. Les dépendances racine des collègues n'ont pas été modifiées.

Avec Go installé :

```sh
gofmt -l pkg/placement lab/placement serveur-zone/app
go test ./... && go test -race ./... && go vet ./...
(cd lab/placement && go test ./... && go test -race ./... && go vet ./...)
(cd serveur-zone && go test ./... && go test -race ./... && go vet ./...)
```

Build multiarchitecture, sans publication :

```sh
docker buildx build -f lab/placement/Dockerfile --target runtime --platform linux/amd64,linux/arm64 --output type=oci,dest=placement.oci .
docker buildx build -f lab/placement/Dockerfile --target zone --platform linux/amd64,linux/arm64 --output type=oci,dest=zone.oci .
```

Le runtime contient les deux exécutables. Vérifier l'OS et l'architecture avant
un futur essai ARM : arm64 suppose un OS 64 bits. Aucune validation sur Raspberry
Pi ou Jetson physique, aucun encodeur Nvidia et aucun déploiement matériel ne
sont revendiqués.

## Utiliser allocator.go et conserver les travaux de Dan

`Allocator.UpdateProjectCapacity`, `Offer`, `Reserve`, `Release` sont les appels
locaux testés. Ils exigent un snapshot **de capacité projet**, fourni par une
interface explicite ; `nodes.go:StorageMax/StorageUsed` n'est pas converti en
capacité projet. L'algorithme est partagé, pas recopié dans deux contrôleurs.

`PLACEMENT_ENABLED=1` active `initAllocator()` depuis `initMQTT()` dans le
serveur existant. Le banc utilise ce chemin. Les handlers et RPC sont isolés
sous `lab/placement/v1`; ils ne commandent pas `edge/{id}/commands/#`.
`database.go` et le modèle/CRUD de `nodes.go` sont préservés (formatage seul dans
nodes.go). Leur base `/data/zone.db` est un registre local, pas le catalogue
global. Le Central est le seul writer du catalogue `/data/central.db`.

Le brouillon `mqtt.go` de la seconde archive contenait une syntaxe `@TODO`
invalide, des imports absents et des hooks non définis. Ces erreurs de compilation
ont été corrigées. `cariboutv/commands` est conservé dans le mode legacy optionnel
`LEGACY_COMMANDS_ENABLED=1`, désactivé par défaut. Ses callbacks refusent les
mutations : le format manifest/identité/réponse de ce topic reste à convenir avec
Dan. Cela ne constitue pas une intégration opérationnelle de ce topic existant.
Les sources originales des points de raccordement sont sous `upstream/`.

Le service de zone seul, avec les commandes désactivées, est reproductible par
`docker compose -f compose.zone.yaml up -d --build --wait`. Son Dockerfile se
construit désormais avec **le contexte racine** pour accéder au paquet partagé.
Le `compose.yaml` racine de l'Edge Agent reste inchangé.

Voir [architecture et contrats](ARCHITECTURE.md), [questions ouvertes](OPEN_QUESTIONS.md)
et [audit préalable](AUDIT.md). Le rapport de validation donne les résultats
réellement observés, y compris les limites et les essais initiaux échoués.

