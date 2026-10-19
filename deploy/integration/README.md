# CaribouTV : branche intégrée, banc PC et préparation laboratoire

Ce Compose est **un nouveau projet `caribou-integration`**, sans migration, suppression ni réutilisation des volumes de `caribou-controllers`. Les scripts existants dans `deploy/lab` restent disponibles. Depuis la racine du dépôt, Docker Desktop en mode conteneurs Linux :

```powershell
# Premier lancement (construction puis secrets distincts, idempotents)
.\deploy\integration\start.ps1
# Sur le PC de Cyril, si le téléchargement des paquets échoue sans proxy :
.\deploy\integration\start.ps1 -BuildProxy 'http://http.docker.internal:3128'
# Les images sont déjà construites sur ce PC :
.\deploy\integration\start.ps1 -SkipBuild
.\deploy\integration\smoke.ps1
.\deploy\integration\resilience.ps1
.\deploy\integration\diagnostics.ps1
```

1. Ouvrir **http://localhost:18090/integration/**, saisir un email et un mot de passe de test d'au moins 8 caractères. Le service Java existant crée automatiquement un compte au premier login ; ce comportement est conservé. Aucun email n'est envoyé.
2. Publier un petit MP4 (64 Mio maximum, suffisamment court pour encoder et transférer avant le bail). Publication privée par défaut ; la case de partage rend la vidéo visible aux autres comptes connectés du banc.
3. Cliquer « Ouvrir le frontend spectateur existant » : catalogue, catégories/recherche, favoris, profil, historique et lecteur utilisent l'API avec la session Java. Une erreur de session exige de revenir à la console pour se reconnecter. La maquette du collègue reste inchangée.
4. Les vidéos « Démo intégrée – fichiers Edge réels » sont des mires synthétiques, réellement segmentées, stockées, relues et décodées. Elles permettent un premier test après connexion avec votre propre compte.

Ports PC par défaut : web **18090**, central opérateur **19088**, MQTT **1884**, bridges Edge **19081/19082**. Tous sont sur loopback. L'authentification Java n'est pas publiée directement. Variables facultatives : `WEB_PORT`, `CENTRAL_PORT`, `MQTT_PORT`, `EDGE_A_PORT`, `EDGE_B_PORT`. `WEB_BIND_IP` et `LAB_BIND_IP` contrôlent les interfaces d'écoute. Les anciens ports 18088/1883/18081/18082 restent disponibles pour l'autre banc.

## Ce qui circule réellement

Navigateur → API web Go → service Java pour la session. La passerelle web réutilise `videostreaming/segmentation_video.py` pour produire `init.mp4` et les segments HLS fMP4. Chaque fichier positif est un extent (`target_size_bytes=1`) ; seul le scheduler Go existant choisit les destinations. L'API Central, le catalogue SQLite, les contrôleurs de zone et MQTT5 existants réservent et valident le plan. La passerelle suit ses endpoints et envoie les vrais octets aux vrais Edge Agents Go via les bridges. PREPARE n'est jamais READY.

`/api/videos` filtre le catalogue Central sur READY, preuves `HTTP_SHA256_VERIFIED` et droits utilisateur. `/api/videos/:id/playback` télécharge les unités depuis leurs Edge, vérifie tailles et SHA-256, puis remuxe le HLS en MP4 pour le lecteur natif existant. `/media/:id/video.mp4` exige la session et gère Range/206. Le Central ne transporte aucun média ; **une passerelle web distincte transporte et reconstruit le média**. Le fichier source envoyé n'est pas conservé ; les segments encodés sont les objets stockés. Un cache MP4 dérivé et les métadonnées UI sont sur `web-data`.

Le JSON web contient propriétaire, titre, visibilité, mapping HLS et profils. Il n'est pas autorité de placement : aucune capacité, réservation ou transition READY n'y est calculée. Les vidéos créées hors de cette API n'ont pas automatiquement de propriétaire/mapping vidéo : elles restent dans le catalogue opérateur, sans exposition au frontend. Une migration explicite reste à concevoir.

## Tests et diagnostic

`smoke.ps1` crée des comptes distincts et deux vidéos de six secondes par exécution. Il conserve les fichiers de test et consomme environ 540 Kio de quota. Il teste sessions, mauvais mot de passe, catalogue READY, propriétaire privé, partage explicite, profil/favoris/reprise, refus cross-origin, rejet d'un faux MP4, relecture SHA-256, remux, décodage FFmpeg et Range/206. Ce sont de vrais services ; seule la mire vidéo est synthétique. Résultats JSON : volume web, `/data/test-results`.

`resilience.ps1` est réservé au **banc local A1/B1**. Il sélectionne le dernier résultat smoke, vérifie les SHA-256 directement dans les volumes des agents, modifie un octet de cette seule vidéo synthétique puis restaure exactement les octets dans un `finally`. Il exige le refus de lecture, puis redémarre uniquement les services de ce nouveau projet et vérifie identités, profils, catalogue et lecture persistants. Les sessions Java sont en mémoire : reconnexion attendue après redémarrage de l'auth. `-SkipRestart` évite cette interruption du banc.

Débordement interzone supplémentaire (12 Mio de fichiers synthétiques **conservés**, pas des vidéos ; test sauté si moins de 6 Mio libres par nœud) :

```powershell
docker compose -f deploy/integration/compose.yaml cp deploy/integration/controller-smoke.py web:/tmp/controller-smoke.py
docker compose -f deploy/integration/compose.yaml exec -T web python3 /tmp/controller-smoke.py
```

```powershell
# Les scripts ne réinitialisent pas les volumes. Une topologie modifiée provoque la recréation des services pour recharger configuration et ACL ; les sessions Java doivent alors être renouvelées.
docker compose -f deploy/integration/compose.yaml ps
docker compose -f deploy/integration/compose.yaml logs --tail 100 web central zone-a zone-b edge-a edge-b
# Arrêter en conservant toutes les données :
docker compose -f deploy/integration/compose.yaml stop
```

Ne pas utiliser `down -v`, `volume prune` ni recopier tout `.runtime` sur un Edge. Si le quota démo de 8 Mio/nœud est plein, les nouveaux placements sont refusés. Choisir avant provisionnement un quota adapté dans `topology.json`. Ne pas modifier à l'aveugle le quota d'un ledger existant ni effacer des données pour refaire passer un test.

## Images AMD64 / ARM64, hors ligne

Les trois Dockerfiles produisent les deux architectures. Les images ont été construites et chargées sur le PC ; le parcours complet a aussi été exécuté en ARM64 sous émulation Docker Desktop. Leur exécution sur de vrais ARM reste à vérifier au labo. Depuis la racine du dépôt :

```powershell
Push-Location deploy/integration
# Avec Buildx et QEMU installés par Docker Desktop :
docker buildx bake -f compose.yaml --set '*.platform=linux/amd64,linux/arm64' --allow=fs.read=../.. --load
# Proxy facultatif : ajouter --set '*.args.http_proxy=http://http.docker.internal:3128' --set '*.args.https_proxy=http://http.docker.internal:3128'
Pop-Location
docker image save --platform linux/arm64 -o integration-control-arm64.tar caribou/integration-control:dev
# Transférer l'archive puis docker load -i integration-control-arm64.tar sur l'Edge.
```

Aucune image n'est publiée dans un registre automatiquement. Le PC peut héberger Java/web/central/zones ; seuls l'agent et son bridge doivent être exécutés sur les Raspberry Pi/Jetson.

## Edge externes : étapes exactes à adapter aux vraies IP

**NON TESTÉ sur le matériel physique** : IP, accès SSH, architecture/OS et routage du laboratoire ne sont pas fournis. Ne pas annoncer les noms `edge-a`/`edge-b` à des machines externes.

1. Copier `topology.json` vers `.runtime/topology.lab.json` et remplacer chaque `endpoint` par `http://IP_REELLE_EDGE:PORT_PUBLIE`. Garder des identités stables et des quotas de projet adaptés. Deux zones A/B sont lancées par ce Compose ; ajouter d'autres zones demande des services correspondants.
2. Sur le PC, dans une session PowerShell dédiée, définir les variables réelles et provisionner :

```powershell
$env:LAB_BIND_IP='IP_LAN_REELLE_PC' # broker et API opérateur
$env:MQTT_PORT='1884'
$env:WEB_BIND_IP='127.0.0.1' # ou IP LAN si des navigateurs externes sont nécessaires
# Le lanceur arrête les quatre services Edge locaux de ce projet pour éviter les identités MQTT en double.
.\deploy\integration\start.ps1 -SkipBuild -ExternalEdges -Topology .\deploy\integration\.runtime\topology.lab.json
.\deploy\integration\export-edge.ps1 -Id A1 -Destination .\deploy\integration\.runtime\exports-A1
.\deploy\integration\export-edge.ps1 -Id B1 -Destination .\deploy\integration\.runtime\exports-B1
```

3. Copier **séparément** chaque dossier exporté vers son Edge et charger l'image. Dans ce dossier sur Linux :

```bash
export CONTROLLERS_IMAGE=caribou/integration-control:dev
export EDGE_ID=A1                      # B1 sur l'autre hôte
export LAB_PC_ADDRESS=IP_LAN_REELLE_PC
export MQTT_PORT=1884
export EDGE_BIND_IP=IP_LAN_REELLE_EDGE
export EDGE_HTTP_PORT=18081            # doit correspondre au topology endpoint
chmod 700 edge-config
sudo chown -R "$(id -u):10001" edge-config/identity
chmod 750 edge-config/identity
chmod 640 edge-config/identity/*
docker compose -f compose.edge.yaml up -d --wait --wait-timeout 180
curl --fail "http://$EDGE_BIND_IP:$EDGE_HTTP_PORT/ready"
```

Le dossier exporté contient seulement l'identité MQTT/HTTP de cet Edge et la topologie publique. Il ne contient jamais les clés des autres Edge ou l'opérateur. Protéger le transfert et les fichiers.

4. Autoriser MQTT 1884 sur le PC depuis les IP des Edge, et chaque port HTTP Edge depuis le PC. Tester avec `Test-NetConnection -ComputerName IP_EDGE -Port PORT_EDGE` et `nc -vz IP_PC 1884` côté Linux. Les règles de pare-feu dépendent du vrai réseau ; aucun port n'est ouvert automatiquement par nos scripts.
5. Dans la console web, vérifier les nœuds frais puis publier une petite vidéo. Lancer le diagnostic avec les deux fichiers Compose : `docker compose -f deploy/integration/compose.yaml -f deploy/integration/compose.external.yaml ps`. Ne pas exécuter `resilience.ps1` sur les machines physiques.

Les identités/secrets sont spécifiques à ce banc : les exports de l'ancien `deploy/lab` ne sont pas interchangeables. Le protocole du banc est authentifié et en clair : accès LAN isolé uniquement. TLS/VPN et exploitation hors réseau de confiance restent à ajouter.

## Limites de livraison

- Live/RTP et SIS/IBS : sources du collègue conservées, non raccordées au parcours opérationnel. SHA-256 ne constitue pas une preuve SIS.
- Anciennes passerelles Python : dépendance `placement.py` absente et round-robin ; conservées, non lancées. La segmentation seule est réutilisée et corrigée.
- Branche Dan `containers` récente : conservée dans `legacy/zone-containers`, pas activée car son nouvel allocateur contredit le scheduler convenu.
- Auth existante : inscription au login, sessions RAM, pas de rôles opérateur/utilisateur, récupération de compte, limitation des tentatives ni déploiement public durci. L'API opérateur conserve son propre secret distinct.
- Pas de réplication/reconstruction d'Edge perdu, pas de GC coordonné ni suppression vidéo, pas de reprise automatique d'upload partiellement transféré. Un échec ne crée pas de faux READY ; ses données/quotas réservés suivent les mécanismes de bail existants.
- Lecture : remux complet avant lecture ; cache local, sérialisation des reconstructions, pas de diffusion HLS adaptative/directe Edge ni de passage à grande échelle. Le catalogue READY est un état de stockage validé, pas une garantie qu'un Edge est encore joignable à chaque instant.
- Compteurs de vues réels, génération de miniatures et variantes de débit : non implémentés ; le frontend affiche actuellement zéro vue et une image neutre.
- Matériel physique, Wi-Fi/VLAN/pare-feu et performances longues vidéos : non testés. Les tests de banc ne prouvent pas ces propriétés.
