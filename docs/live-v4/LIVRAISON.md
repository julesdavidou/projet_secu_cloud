# Live signé et mesures — livraison du 20 octobre 2026

Base vérifiée : `main` à `cfda3e3`, merge humain de la PR #6 contenant `880a869` et l'intégration V3 précédente. La branche `codex/live-secure-metrics` ajoute le live ; elle nécessite sa propre revue et n'est pas fusionnée automatiquement. Les rapports V3 décrivent leur état historique ; ce document décrit le nouveau lot.

## Démarrage Windows reproductible

Prérequis : Git, PowerShell 7, Docker Desktop démarré en conteneurs Linux. Depuis la racine du dépôt :

```powershell
git fetch origin
git switch codex/live-secure-metrics
./deploy/live-v4/start.ps1
# Uniquement si ce PC exige le proxy Docker pour construire les images :
# ./deploy/live-v4/start.ps1 -BuildProxy http://http.docker.internal:3128
./deploy/live-v4/smoke.ps1
```

Ouvrir `http://localhost:20690/live/` ; le frontend VOD du même banc est à `http://localhost:20690/`. Ce Compose indépendant utilise `caribou-live-v4`, ses propres volumes, PKI, comptes et tags d'images. Il ne redémarre pas les bancs `caribou-secure-v3` (19690) ou `caribou-integration` (18090). Données et clés existantes conservées. Le premier build télécharge les dépendances ; `-SkipBuild` réutilise ensuite les images déjà construites de cette branche.

`smoke.ps1` crée les comptes/espaces de démonstration et teste la VOD réelle. Comptes **locaux de test seulement** : `secure-owner-a@example.invalid` et `secure-owner-b@example.invalid`, mot de passe `OnlyForLocalTest!2026`. Ce sont des fixtures identifiables, pas des secrets de production. Les identifiants stables viennent de Java et varient selon l'installation. Chaque agent propriétaire reste lié à son premier compte : ne pas substituer un autre utilisateur dans un volume déjà enrôlé ; un nouvel utilisateur doit avoir son propre agent et volume.

Pour OBS, dans un second terminal :

```powershell
./deploy/live-v4/publish.ps1 -Email secure-owner-a@example.invalid -Title 'Mon direct'
```

Saisir le mot de passe dans la boîte de connexion. Attendre le message OBS, puis configurer OBS : service personnalisé, serveur `rtmp://127.0.0.1:21935/live`, clé `stream`, vidéo H.264, audio AAC, intervalle d'image clé **2 secondes**. Lancer la diffusion. Choisir le direct dans `/live/` ou utiliser son identifiant `l-…` affiché par le producteur, puis cliquer sur Regarder. La vidéo démarre muette pour permettre l'autoplay ; on peut activer le son dans le lecteur. Arrêter OBS termine le direct après publication des fragments finalisés. Pour l'autre agent, utiliser `-Slot b` et le port RTMP 21936.

Pour une source fichier rejouée à vitesse réelle, sans OBS :

```powershell
./deploy/live-v4/publish.ps1 -Email secure-owner-a@example.invalid -FilePath 'C:/Videos/test.mp4' -Title 'Essai temps réel' -MaxSeconds 120
```

Le mode normal remuxe H.264/AAC sans réencodage. `-Transcode` demande explicitement H.264/libx264 CRF18 + AAC : il consomme du CPU et modifie la qualité ; il n'exploite pas le GPU Jetson. Aucune baisse automatique de résolution ou débit. Une source incompatible doit être convertie ou refusée ; aucun succès simulé.

Pour partager avec les autres comptes connectés, ajouter `-Public` au lancement. Même un direct partagé reste soumis à une session Java. Pour arrêter depuis un autre terminal :

```powershell
$liveID = Read-Host 'Identifiant exact l- suivi de 32 caractères hexadécimaux'
./deploy/live-v4/stop.ps1 -Email secure-owner-a@example.invalid -LiveID $liveID
```

Le producteur contrôle l'état toutes les cinq secondes entre publications ; le délai dépend des requêtes en cours et du réseau. Un arrêt forcé du conteneur peut nécessiter cette clôture authentifiée après redémarrage.

## Décisions et parcours réel

1. **Un seul placement** : FFmpeg → producteur natif → agent propriétaire existant → upload V3 → scheduler Go / allocator des zones → MQTT / ACK métier → stockage HTTP réel → catalogue SQLite Central. Aucun choix de nœud dans le Python. Les politiques prix/RTT existantes s'appliquent. `live_watcher.py` devient une entrée vers ce parcours ; l'ancien prototype reste récupérable dans Git.
2. **Session signée** : création, ordre, fermeture et partage sont des opérations IBS versionnées. Le journal et le contrôle CAS/nonces sont dans le même SQLite Central. Un fragment entre dans le manifest seulement après `READY`, avec propriétaire/session/numéro signés. Réessayer conserve identifiant et preuve ; changer le contenu est refusé. Le live n'accepte pas de délégation ; les opérations déléguées VOD restent préservées sans extension implicite de leur portée.
3. **Fragments immuables** : init + fragment fMP4 passent par les signatures SIS/Hess-BLS12-381 V3 et la reconstruction sur les vrais Edge. L'init est séparé pour HLS seulement après reconstruction et contrôle de son SHA-256 signé. SHA-256 reste distinct de SIS/IBS. Aucun manifest n'accepte d'URL Edge ou d'URI arbitraire du client.
4. **Lecture continue** : HLS glissant sur six fragments, sans `ENDLIST` avant clôture, HLS.js 1.7.3 embarqué avec licence/provenance, sans CDN. Session et partage sont vérifiés à chaque requête, même en cache. Cache reconstruit vérifié : une minute, au plus 64 entrées live. Une corruption durable est détectée lors d'une reconstruction froide, pas rétroactivement dans des octets déjà vérifiés en cache.
5. **Clés/privilèges** : Java inchangé, clés privées dans l'agent propriétaire persistant, pas dans OBS/le navigateur/le producteur natif. Ce dernier renouvelle sa connexion Java toutes les quinze minutes ; le mot de passe reste en RAM et est transmis au conteneur par stdin. Provisionnement Docker par commande ponctuelle autorisée ; aucun conteneur ne reçoit le socket Docker.
6. **RTMP local** : FFmpeg écoute seulement pendant la publication ; ports Docker publiés sur `127.0.0.1`. `stream` n'est pas une authentification : l'entrée est une source locale de confiance ; l'agent authentifié signe ce qu'il reçoit. OBS distant nécessite par exemple un tunnel SSH authentifié vers un serveur SSH déjà installé/autorisé. Aucun RTMP public sécurisé, RTMPS, serveur SSH, RTP ou WebRTC n'est installé par ce lot.

Un écran live est lié depuis le frontend existant ; le lecteur VOD, ses playlists, utilisateurs et droits sont conservés. Les paquets à un seul `original.mp4` sont réservés aux racines live `l-…` et opérations `live_segment-N` ; aucune relaxation de la validation VOD.

## Mesures et interprétation

Exporter les mesures dans `/live/` produit un JSON local : premier affichage par `requestVideoFrameCallback` quand disponible (repli explicitement identifié), FPS observés, images perdues, interruptions/durée, buffer, distance au bord live, temps/octets/TTFB de téléchargement et temps serveur de reconstruction. Le producteur conserve aussi publication/backlog/SHA dans `/data/captures/…/measurements.json`.

L'âge programme utilise `EXT-X-PROGRAM-DATE-TIME` de FFmpeg et l'horloge serveur estimée par aller-retour HTTP ; il suppose des machines synchronisées NTP. C'est une estimation programme → lecture, **pas caméra → écran**. `hls.latency` est une distance au bord du flux ; elle ne remplace pas cette latence physique. Pour caméra → écran, filmer une horloge commune source/écran ou injecter des timestamps de capture corrélés, puis mesurer sur matériel réel.

Pendant la diffusion, dans un autre terminal :

```powershell
./deploy/live-v4/measure.ps1 -Seconds 60 -IntervalSeconds 2
```

Échantillonnage Docker de tous les rôles V4, y compris les conteneurs par utilisateur : CPU/RAM et compteurs réseau/disque. Calculer les débits sur leurs différences entre échantillons. Ce n'est pas une instrumentation Wi-Fi/GPU/caméra. Le test navigateur collecte ces ressources pendant l'émission, pas seulement après.

Comparaison de qualité, seulement avec référence réelle et enregistrement reconstruit complet :

```powershell
python ./videostreaming/live_quality.py --reference source.mp4 --reconstructed reception.mp4
```

L'outil FFmpeg refuse des nombres d'images différents, puis compare SSIM/PSNR par rang. Il neutralise les timestamps pour cette comparaison d'image uniquement : RTMP quantifie les temps à la milliseconde et un simple alignement PTS faisait comparer des images voisines. Il ne mesure ni synchronisation A/V ni fidélité temporelle. PSNR infini est la chaîne JSON `"inf"`. Sans référence, aucune valeur SSIM/PSNR n'est inventée.

## Résultats locaux du 20/10

Les [preuves](preuves/) proviennent de Docker Desktop Linux AMD64 sous Windows, pas du matériel physique.

| Contrôle | Résultat réellement observé |
|---|---|
| Source | Motif/sinus synthétiques 320×180, 24 FPS, 32 s ; transport RTMP TCP FFmpeg réel à vitesse réelle |
| Stockage/reconstruction | 8 fragments de tailles variables, fichiers réellement stockés dans deux zones ; init+fragment relus identiques octet par octet et décodables |
| Navigateur | Chromium headless, rendu avant fin d'émission ; 768 images affichées, 0 perdue, 0 interruption mesurée, 8 téléchargements |
| Premier affichage | 9,436 s depuis la demande, attente initiale/validation comprises ; aucun SLA |
| Lecture stabilisée | Médiane observée ≈23,83 FPS ; âge programme médian ≈8,22 s sur échantillons actifs après 15 s |
| Qualité image | 768 images référence = 768 reconstruites ; SSIM 1, PSNR infini par rang ; aucune affirmation sur caméra/débit élevé |
| Publication | Médiane ≈3,24 s par fragment de 4 s sur ce PC avec les contrôles du test ; aucune extrapolation Pi/Jetson |
| Ressources | 16 échantillons pendant l'essai ; CPU/RAM/net/disque par rôle conservés ; charge du banc/test comprise |
| Droits/ordre | Compte étranger privé refusé, partage/retrait même après cache, répétition idempotente, trou et append après fin refusés |
| Pannes réelles | Bloc corrompu puis absent : 502 froid, restauration dans finally ; état/ordre/droits/octets identiques après redémarrage complet |
| Régression VOD | Deux propriétaires/deux zones, stockage/relecture signés réels, passés sur le même code V4 |
| Vérifications | Trois modules Go : tests/race/vet/compilation ; 13 tests Python crypto/live ; 9 tests frontend |
| Commandes utilisateur | Wrapper PowerShell testé avec MP4 synthétique de 8 s et mot de passe stdin ; mesure PowerShell testée |
| ARM64 | Quatre images construites ; Go Edge/health exécuté sous QEMU ; live physique Raspberry Pi/Jetson **non testé** |

Les unités SQLite emploient explicitement un double du verdict crypto et des lignes `READY` de fixture. L'intégration utilise les vraies autorités, `py_ecc`, Java, MQTT, fichiers et reconstructions. La réservation de capacité du test VOD et les filtres prix du live dirigent le placement via le scheduler existant ; aucun `STORED` fabriqué.

Pour reproduire :

```powershell
docker build --target verify -f deploy/lab/Dockerfile .
./deploy/live-v4/smoke.ps1
docker compose --project-directory deploy/live-v4 -f deploy/live-v4/.runtime/compose.json exec -T owner-a python -m unittest test_live test_secure_sis test_secure_operations test_publication_retry -v
# Node 24 et Playwright 1.62.1 + Chromium installés dans l'environnement de test :
node ./deploy/live-v4/browser-check.cjs
docker compose --project-directory deploy/live-v4 -f deploy/live-v4/.runtime/compose.json cp owner-a:/data/test-results/live.json deploy/live-v4/results/live.json
./deploy/live-v4/resilience.ps1
```

Le test de pannes cible uniquement les nouveaux fragments de `results/live.json`, vérifie labels/propriétaire, sauvegarde/restaure le bloc et redémarre seulement V4. Ne pas lancer deux producteurs sur la même entrée RTMP. `live-v4.yml` reproduit le parcours navigateur et conserve les artefacts ; les six workflows précédents restent actifs. Le rapport final externe donne leur statut sur le SHA publié.

## Bornes et préparation du laboratoire

- Fragments par défaut 4 s, GOP 2 s recommandé. Le premier fragment fixe la durée cible HLS ; un GOP ultérieur qui la dépasse est refusé pour garder un manifest conforme. Signature/publication séquentielles : réduire à 2 s sans mesurer la capacité peut accumuler du retard. Aucun LL-HLS, multi-débits adaptatif, réplication nouvelle ou garantie de latence.
- Init ≤1 MiB, fragment ≤16 MiB, 1024 fragments/session ; producteur borné à 1 h et backlog 8 par défaut. Dépassement : arrêt explicite, aucune perte silencieuse. Changement de codec/init : nouvelle session. Une reconnexion OBS peut demander une nouvelle commande/session.
- Crédits démonstratifs : budget par publication, pas paiement ni plafond global du direct. La rétention demandée n'est pas une purge. Les fichiers historiques restent conservés ; vérifier l'espace libre avant un long essai. Le manifest glissant n'est pas un DVR intégral.
- Reprise possible d'un dossier HLS existant en surveillance, sans `--file`/`--rtmp-listen`, avec son journal stable ; aucune capture existante écrasée en mode entrée. Arrêter une entrée peut perdre son dernier fragment non finalisé, jamais annoncé `READY`.
- Les paramètres SIS V3 nécessitent encore une évaluation cryptographique indépendante. Les tests ne prouvent pas une conformité intégrale à l'article. Le 502 historique reste de cause indéterminée ; les 502 injectés ici sont intentionnels.

Sur Raspberry Pi/Jetson, suivre les procédures existantes [réseau, export, installation](../conformity-v3/RESEAU-QOS-OPERATIONS.md) **avec le code de cette branche**. Le live réutilise Edge/worker existants : aucun second agent MQTT ni placement. Construire les tags ARM64 distincts, exporter un bundle propre à chaque appareil et à la PKI du banc choisi, charger les images, provisionner chaque vrai propriétaire, puis contrôler healthchecks et annonces de zone. Docker/Python et PowerShell 7 pour l'administration sont des prérequis ; un ancien JetPack peut nécessiter un OS compatible. Images CPU, aucun besoin de CUDA.

Le banc V4 ici reste local. Ne pas mélanger des bundles PKI V3 avec V4 (identités/ports distincts). Pour un premier essai physique, conserver un profil V3 et ses bundles cohérents, reconstruire ses images depuis cette branche, puis publier nativement depuis le PC contre l'agent propriétaire correspondant (19591 pour A, 19592 pour B), après provisionnement utilisateur selon le guide :

```powershell
python ./videostreaming/live_publish.py --owner-url http://127.0.0.1:19591 --email VOTRE_COMPTE --hls-dir ./capture-unique --rtmp-listen rtmp://127.0.0.1:1935/live/stream
```

Ce mode demande Python/httpx et FFmpeg sur le PC ; OBS pousse sur le port local 1935, et le lecteur V3 est à `http://localhost:19690/live/` après reconstruction du web de cette branche. Ne pas reconstruire un banc en cours d'essai sans coordination avec Jules. Les portails et frontend HTTP restent en loopback ; un navigateur distant nécessite un tunnel SSH autorisé ou un frontal HTTPS avec session, pas une ouverture HTTP improvisée. Le Wi-Fi seul ne remplace ni SSH, ni Docker, ni certificats. Aucun déploiement physique validé en une commande sur tous les appareils.

Références : [muxer HLS FFmpeg](https://ffmpeg.org/ffmpeg-formats.html), [HLS.js playingDate](https://hlsjs-dev.video-dev.org/api-docs/hls.js.hls.playingdate), [HLS.js latency](https://hlsjs.video-dev.org/api-docs/hls.js.hls.latency), [statistiques fragments](https://hlsjs.video-dev.org/api-docs/hls.js.fragloadeddata).
