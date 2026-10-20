# Déploiement multiarchitecture avec GHCR

## Audit et architecture

Le banc physique utilise `deploy/integration/*` et `deploy/lab/compose.edge.yaml`. Les bancs `secure-v3`, `live-v4` et leurs workflows restent indépendants.

| Package GHCR sous `ghcr.io/julesdavidou/` | Dockerfile / cible | Services | Plateformes |
| --- | --- | --- | --- |
| `projet_secu_cloud-control` | `deploy/lab/Dockerfile`, `runtime` | PC : Central, zones, provisionnement ; Edge : agent + bridge | AMD64, ARM64, ARM/v7 |
| `projet_secu_cloud-auth` | `deploy/integration/Dockerfile.auth` | Authentification Java sur PC | AMD64 |
| `projet_secu_cloud-web` | `deploy/integration/Dockerfile.web` | API Go, frontend, ffmpeg/Python sur PC | AMD64 |

Le broker reste l’image tierce `eclipse-mosquitto:2.0.22`, sans republication. Java et Web ne sont pas construits inutilement en ARM/v7.

Dans `fleet.py`, `build_image` construit les trois images PC puis Control pour chaque architecture Edge. Le mode local conserve `docker image save` dans `main`, l’envoi SFTP dans `deploy_node` et `docker load` dans `remote-deploy.sh`. Les secrets, configurations et une archive Git sont transférés dans les deux modes. Les modes sont explicites : une erreur GHCR ne provoque aucun fallback automatique ni changement silencieux de commit.

Le rejet « Image incorrecte » du 20 octobre vient d’une confusion vérifiée entre l’index OCI `dd407e…` renvoyé par Docker Desktop et la configuration `b8e8a4…` utilisée par Docker classique. Le mode local calcule désormais le digest de la configuration désignée par `manifest.json` dans l’archive Docker. La vérification distante accepte cette configuration ou le descripteur de transport attendu, puis vérifie commit et architecture ; le contrôle n’est pas supprimé.

## Workflow, versions et cache

`.github/workflows/fleet-images.yml` :

1. Sur PR : tests de l’automatisation, frontend, contrôles Dockerfile, Go test/race/vet dans les trois modules et smoke d’intégration Java/Go/MQTT/vidéo. Les shells sont également exécutés dans un conteneur sans réseau avec Docker simulé pour tester rollback et nettoyage des credentials. Aucune connexion au laboratoire.
2. Cinq variantes sont construites : Control AMD64/ARM64/ARMv7, Auth AMD64, Web AMD64. Go est compilé sur le runner natif avec GOOS/GOARCH et GOARM=7 pour ARMv7. QEMU sert aux étapes runtime ARM et au véritable smoke HTTP des binaires.
3. Une PR ne publie rien. Un push dans `main` publie les variantes `sha-<SHA complet>-<architecture>`, puis assemble les tags `sha-<SHA complet>` et `main` seulement après réussite de tous les builds/smokes. Un lancement manuel ne publie que depuis `main`.

Les variantes intermédiaires peuvent exister si un smoke échoue ; le tag de déploiement final n’est assemblé qu’après succès. Les autres workflows existants restent indépendants : cette chaîne ne prétend pas remplacer tous les bancs du projet.

Les actions sont épinglées par SHA complet, avec leurs versions vérifiées dans `deploy/fleet/actions-lock.json` : checkout 7.0.1, build-push 7.4.0, Buildx 4.4.1, QEMU 4.4.0, login 4.6.0 et metadata 6.2.0. Le workflow passe actionlint.

Les labels OCI contiennent source et révision. **L’immuabilité vient de `image@sha256:…`, pas du tag**, qui peut être remplacé lors d’une reconstruction du même commit. Le déployeur résout le tag du SHA complet, vérifie les hashes du manifeste, du manifeste enfant et de la configuration, puis l’architecture et le label de commit. Le rapport et le manifeste distant conservent les références exactes.

Les caches GHA ont un scope par composant/architecture : `cache-from: type=gha,scope=…` et `cache-to: type=gha,scope=…,mode=max`. Les dépendances Go sont téléchargées dans une couche avant le code. Les mounts BuildKit de modules/compilation sont conservés, avec initialisation depuis cette couche : un mount seul ne persiste pas entre runners éphémères. Les labels de commit arrivent en fin de Dockerfile. Modifier uniquement le frontend n’invalide plus la compilation Control.

Le cache registry n’est pas activé en parallèle : GHA suffit ici et évite un package/droit supplémentaire. Le premier build reste coûteux ; un changement de code invalide les couches concernées. Les CRLF historiques d’archives Git sont normalisés dans la cible Docker `verify` avant gofmt, et `.gitattributes` fixe les Go à LF. Aucun comportement Go n’a été changé.

## Activation GitHub et packages privés

Après validation des changements, **l’utilisateur décide du commit/push sur main** ; aucune publication n’a lieu pour le seul code local.

1. **Repository → Settings → Actions → General** : autoriser les actions du workflow, garder les protections de branche/revue. Le workflow demande `contents: read` et `packages: write` dans les jobs qui publient.
2. La publication utilise `secrets.GITHUB_TOKEN`. Aucun PAT de publication à ajouter aux secrets du dépôt.
3. Après première publication, vérifier dans chacun des trois packages : visibilité **Private**, lien au dépôt, et **Package settings → Manage Actions access** : accès en écriture pour ce dépôt. Une politique d’organisation peut exiger une intervention administrateur ; ne pas la contourner ni rendre les packages publics.
4. Attendre le succès de **Fleet images (GHCR)** avant de déployer le SHA correspondant.

Pour télécharger, créer un **PAT classic** avec expiration et **`read:packages` uniquement**, sur un compte ayant accès aux trois packages. Pas de `write:packages`, `delete:packages` ou `repo` requis pour ces pulls GHCR avec droits granulaires. Autoriser le SSO si nécessaire. Selon la documentation GitHub actuelle, les fine-grained PAT ne remplacent pas ce PAT pour GitHub Packages.

Le déployeur demande le PAT en saisie masquée (`getpass`). Le compte vient de `-RegistryUser` ou `CARIBOU_GHCR_USERNAME`. `CARIBOU_GHCR_TOKEN` est accepté si un gestionnaire de secrets le fournit dans l’environnement ; ne pas saisir sa valeur littérale dans l’historique PowerShell ou le chat.

Le PC utilise `docker login --password-stdin` avec un `--config` temporaire et une ACL NTFS limitée au compte courant. Les Edge reçoivent le token uniquement dans stdin du canal SSH chiffré, puis utilisent `docker login --password-stdin` avec `DOCKER_CONFIG` temporaire et privé sous root. Aucun token dans argv, SFTP, Git, rapports ou logs. Les traps/finally nettoient après succès/échec. Un arrêt brutal peut empêcher un trap : le dossier résiduel reste protégé par les droits root/compte courant et n’est pas réutilisé.

Rotation : fournir un nouveau PAT, vérifier un pull, puis révoquer l’ancien dans GitHub ; aucune mise à jour de fichier sur les Edge. `Remove-Item Env:CARIBOU_GHCR_TOKEN` retire une variable de la session courante après utilisation.

Références : [GHCR et PAT classic](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry), [publication avec GITHUB_TOKEN](https://docs.github.com/en/actions/tutorials/publish-packages/publish-docker-images), [cache GHA](https://docs.docker.com/build/cache/backends/gha/), [actions Docker](https://github.com/docker/build-push-action).

## Première installation et déploiement Windows

Organisation : dépôt `Secu Cloud/projet_secu_cloud`, outils/accès/venv/rapports dans le voisin `Secu Cloud/cariboutv-outils`. Après un pull, synchroniser uniquement les sources publiques :

```powershell
cd 'C:\Users\jules\Documents\ICY\5A\Secu Cloud\projet_secu_cloud'
.\deploy\fleet\install-tools.ps1
cd '..\cariboutv-outils'
```

`inventory.local.json` et `.runtime` sont conservés. Pour une installation neuve seulement, copier `inventory.example.json` en `inventory.local.json` puis renseigner les accès. Python 3.10+, Git et Docker Desktop Linux sont nécessaires sur le PC ; le lanceur installe Paramiko dans son venv. Compose PC 2.24.4+ est requis pour l’overlay `!override`.

Après publication réussie, PowerShell administrateur pour la règle de pare-feu :

```powershell
docker info --format '{{.OSType}}'   # attendu : linux
.\deploy.ps1 -ImageSource registry -RegistryUser julesdavidou -CoordinatorAddress 192.168.2.64 -ConfigureFirewall -Jobs 5
.\test.ps1 -HealthOnly
.\test.ps1
```

Le PAT est demandé une fois. Sans `-Nodes`, les cinq appareils sont sélectionnés. Le mode registry résout et télécharge les images centrales puis prépare les pulls des Edge **avant mise à jour des services centraux**. Il n’exécute ni build, ni export Docker, ni transfert SFTP d’image. La copie Git/config/secrets reste nécessaire. Les images locales présentes et vérifiées sont réutilisées ; un Edge avec cache valide n’exige pas de nouvelle connexion GHCR.

Le PC héberge toujours les services centraux et le broker du banc sur 1884 ; ce n’est pas un simple lanceur. Le site est `http://IP_DU_PC:18090/`, la console `/integration/`. Le broker tiers `192.168.1.1:1883` n’est pas utilisé ici.

Pour un groupe partiel :

```powershell
.\deploy.ps1 -RegistryUser julesdavidou -Nodes jetson02,raspberry04,raspberry05 -Jobs 3
```

La sélection définit le groupe actif complet, la topologie, le pare-feu et les tests. Les machines exclues ne sont pas contactées/arrêtées, mais disparaissent de la nouvelle topologie centrale. Repasser la liste complète pour ajouter un nœud, ou omettre `-Nodes` pour les cinq. `-Jobs` limite les contrôles, pulls et déploiements SSH parallèles ; les trois pulls centraux restent séquentiels. Les tests suivants reprennent le dernier groupe demandé ; `-Nodes` sur les tests seuls ne reconfigure pas le placement vidéo.

## Version précise, redéploiement et rollback

```powershell
$revision = 'REMPLACER_PAR_UN_SHA_COMPLET_DE_40_CARACTERES_HEXADECIMAUX'
.\deploy.ps1 -RegistryUser julesdavidou -Revision $revision
.\deploy.ps1 -RegistryUser julesdavidou -Revision $revision -ForceRecreate
```

Le défaut est `git fetch origin main` puis `origin/main`. `-SkipFetch` garde la référence connue. `-Revision` accepte uniquement un SHA complet. Le code non committé n’est pas déployé. Les scripts/configs Compose du PC doivent correspondre au commit choisi ; un écart est refusé. Pour un ancien commit qui change ces fichiers, remettre d’abord la copie du dépôt à cette version après sauvegarde de ses modifications locales et passer **aussi `-Revision`**, car le défaut reste origin/main.

Retrouver l’ancien commit dans `.runtime/reports`, puis relancer avec ce SHA et le même groupe actif. Les rapports gardent les digests : si le même SHA a été reconstruit et son tag remplacé, comparer au rapport précédent pour exiger les mêmes octets.

Chaque Edge garde une sauvegarde privée `previous.*` de sa configuration. En cas d’échec de `compose up --wait`, le script restaure celle-ci et tente le redémarrage de l’ancienne image en cache, sans pull ni build. Il indique si le rollback échoue. Pas de rollback atomique du parc, des bases/données ou des services centraux : ces derniers nécessitent un redéploiement explicite de la version précédente. Une migration de schéma applicatif demanderait une procédure spécifique.

Volumes nommés, ledgers, secrets et anciennes releases restent conservés ; les changements de quota/identité d’un ledger existant sont refusés. Aucun prune ni suppression de volume dans les outils du parc.

## Mode local de secours et options

```powershell
.\deploy.ps1 -ImageSource local -CoordinatorAddress 192.168.2.64 -ConfigureFirewall -Jobs 5
.\deploy.ps1 -ImageSource local -SkipBuild -SkipFetch -Jobs 5
.\deploy.ps1 -CheckOnly -Jobs 5
```

`local` conserve build + save/SFTP/load et ne demande aucun accès GHCR. Il n’est totalement hors ligne que si images de base/dépendances et références Git sont en cache. `-SkipBuild` interdit les builds locaux et exige les bonnes images ; il est sans effet en registry, où les pulls restent actifs. `-CheckOnly` contrôle SSH/sudo/Docker/architecture/horloge/espace/Compose, sans déploiement, installation Compose ou accès GHCR ; un PASS ne prouve pas qu’un conteneur tourne.

Compose dédié v2.26.1 (minimum Edge 2.20) et son contrôle SHA256 restent automatiquement installés si besoin dans `~/cariboutv-lab/tools/docker-compose`. Docker Engine n’est pas remplacé. Les contrôles de clés SSH, d’horloge à trois secondes, d’architecture 32/64 bits et d’espace restent actifs.

## Réseau et diagnostic

PC → Edge TCP 18081, y compris depuis le conteneur web ; Edge → PC TCP 1884. `-ConfigureFirewall` règle uniquement MQTT dans Windows, pour les IP sélectionnées. L’accès web depuis téléphone peut nécessiter une règle TCP 18090. Aucun changement des routeurs ou pare-feu Linux.

Pour les images manquantes, DNS/HTTPS vers GHCR et ses destinations de téléchargement sont nécessaires sur chaque cible. Le script distant vérifie DNS avec getent, login HTTPS/authentification puis pull exact avec plateforme. Un refus de pull peut relever du réseau, des droits ou de la plateforme ; les détails Docker de l’authentification sont volontairement remplacés par un message sans secret. L’alternative `-ImageSource local` est explicite ; aucun changement automatique d’image en cas d’échec réseau.

```bash
uname -m
getconf LONG_BIT
getent hosts ghcr.io
curl -I https://ghcr.io/v2/  # 401 sans authentification est normal et confirme HTTPS.
sudo docker info --format '{{.OSType}} {{.Architecture}}'
cd ~/cariboutv-lab/deployment
sudo docker compose -p caribou-fleet -f compose.edge.yaml ps -a
sudo docker compose -p caribou-fleet -f compose.edge.yaml logs --tail 100
sudo cat manifest.json
```

Remplacer `docker compose` par `~/cariboutv-lab/tools/docker-compose` s’il est installé. Le dossier dépend du compte SSH : `/home/camera/cariboutv-lab` même si l’on vérifie depuis root. Les rapports PC sont dans `.runtime/reports`.

Pour une image déjà téléchargée, reprendre la référence exacte du manifeste :

```bash
sudo docker image inspect 'ghcr.io/julesdavidou/projet_secu_cloud-control@sha256:DIGEST' \
  --format '{{.Os}}/{{.Architecture}} {{.Id}} {{index .Config.Labels "org.opencontainers.image.revision"}}'
```

Sur le PC authentifié manuellement, `docker buildx imagetools inspect IMAGE:sha-COMMIT` affiche le manifeste et ses plateformes. Ne pas déduire l’architecture du nom de machine : raspberry04 est ARM64, camera et raspberry05 sont ARM/v7 dans le parc constaté.

## Tests disponibles et limites

```powershell
py -3 -m unittest discover -s deploy/fleet -p 'test_*.py' -v
node --test frontend/tests/contracts.test.mjs frontend/tests/static-server.test.mjs
docker build -f deploy/lab/Dockerfile --target verify .
docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7 -f deploy/lab/Dockerfile --target runtime --output type=cacheonly .
docker run --rm --network none --env CARIBOU_SHELL_TESTS=1 --mount "type=bind,source=${PWD},target=/repo,readonly" --workdir /repo python:3.13-slim python -m unittest discover -s deploy/fleet -p test_shell.py -v
```

Le smoke réel peut tourner dans un projet local avec ports, images et volumes distincts. La CI n’accède jamais aux adresses privées des appareils. `test.ps1 -HealthOnly` contrôle images/versions, santé et heartbeats ; `test.ps1` ajoute transfert, SHA256, reconstruction et décodage d’une courte vidéo privée conservée dans le stockage. Le scheduler choisit les nœuds transportant les unités : le test ne prétend pas transférer un média sur chaque machine.

Publication GHCR, cache GHA distant, permissions privées et pulls depuis les cinq appareils restent à vérifier après le push approuvé et le déploiement explicitement lancé. Un build ARM et un démarrage sous QEMU ne constituent pas une validation physique Jetson/Raspberry.

### Vérification locale du 20 octobre 2026

- 34 tests unitaires Python/PowerShell réussis sur Windows ; les cinq tests shell sont ignorés dans ce premier lancement puis exécutés séparément dans Linux : cinq réussites, dont rafraîchissement de topologie à image inchangée.
- Neuf tests frontend réussis ; Go test, race et vet réussis dans les trois modules.
- Trois Dockerfiles vérifiés sans avertissement ; workflow validé par actionlint 1.7.12.
- Images Control/Edge construites sur AMD64, ARM64, ARM/v7 et véritable démarrage HTTP réussi sur chacune (QEMU pour ARM). Web et Auth construits sur AMD64.
- Smoke d’intégration réel réussi dans `caribou-ghcr-check` : connexion Java, upload, placement, stockage et SHA256, reconstruction MP4, décodage audio/vidéo, Range206, droits privés, favoris/progression et frontend. Ce banc distinct a ensuite été arrêté, avec ses volumes conservés. Les projets existants n’ont pas été redéployés.
- SSH en lecture seule : PASS jetson02, jetson03, raspberry04, camera ; raspberry05 (`192.168.2.11`) : délai dépassé à deux reprises depuis ce PC. Aucun déploiement physique ni installation Compose exécuté. Les tests physiques et les autres bancs V3/V4 n’ont pas été relancés.
- Aucun commit, push, lancement GitHub Actions ou publication GHCR effectué pendant la préparation.
