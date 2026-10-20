# Participants Windows : profil sécurisé V3

Les outils Fleet de Jules et leur déploiement historique restent disponibles.
Ce complément utilise **le même Edge Go**, les mêmes contrôleurs, les autorités
SIS/IBS et le stockage V3. Ne pas raccorder les Edge Fleet HTTP/1884 à ce banc
mTLS en changeant seulement leur adresse : les contrats de sécurité diffèrent.

## Ce que chacun lance

| Rôle choisi par l'administrateur | Sur le portable |
|---|---|
| `viewer` | Portail local, navigateur et session Java habituelle |
| `publisher` | Portail + agent personnel avec portefeuille persistant ; source MP4 ou OBS |
| `storage` | Edge Go, broker MQTT, worker et un conteneur/volume par propriétaire autorisé |

Un bundle est personnel : **ne pas distribuer celui de Jules à tous les PC**.
Il contient seulement les certificats de ces rôles et de ce nœud. Ni clé racine,
ni clé d'autorité de zone, ni portefeuille d'un autre propriétaire n'est exporté.
Le rôle producteur crée ses propres clés dans son volume après authentification.

Prérequis : Windows 11, Docker Desktop démarré **en mode conteneurs Linux AMD64**,
Python 3.10+ accessible par `py -3` ou `python`, et PowerShell 7 (`pwsh`).
Les scripts ne les installent pas et ne changent aucune politique Windows.
L'aide LAN fonctionne en Python pur (`SKIP_CYTHON=1`) : ne pas désactiver
App Control/Defender pour charger une extension Cython refusée.

Le 20 octobre, cinq identités ont été admises explicitement : JULES1, WIN2,
WIN4 en zone A ; WIN3, WIN5 en zone B. Plafond : 1 Gio chacune. Les IP peuvent
rester inconnues jusqu'au lancement : Windows sélectionne la route vers le
contrôleur, puis la zone valide l'annonce mTLS du nœud. Un paquet distinct
correspond à chaque PC, avec son propre compte de démo et mot de passe aléatoire
dans `account.private.json`. Ne pas échanger ces paquets.

## Préparation unique sur le PC contrôleur

Depuis la racine du dépôt contenant cette intégration :

```powershell
docker build -f deploy/lab/Dockerfile -t caribou/windows-control:dev .
```

Conserver le runtime et la CA du banc V3 déjà démarré. Exemple sans stockage :

```powershell
./deploy/fleet/prepare-client.ps1 -ClientID jules-pc -ControllerAddress 192.168.2.51 -Roles 'viewer,publisher' -OutputDirectory C:/BundlesPrives/jules-pc -ControlImage caribou/windows-control:dev -IncludeImages
./deploy/fleet/enable-client-gateway.ps1 -BindAddress 192.168.2.51 -ControlImage caribou/windows-control:dev
```

La passerelle n'accepte que les certificats `viewer-lab-*` et écoute en mTLS
sur le LAN, port 19693. Le navigateur reste sur `localhost` via son portail.
Les droits Java et l'obligation SIS/IBS sont conservés. Le magasin de certificats
Windows n'est pas modifié. Les contrôleurs ne deviennent pas publics.

Après redémarrage du PC contrôleur, conserver le runtime et utiliser :

```powershell
./deploy/fleet/resume-controllers.ps1 -BindAddress 192.168.2.51
```

Cette commande réutilise les admissions actuelles et relance les aides LAN
natives. Ne pas réinitialiser avec l'exemple à deux nœuds : il ne contient pas
les nouvelles admissions. Aucun volume n'est supprimé.

**Ajouter le stockage nécessite une admission administrative explicite.**
Vérifier d'abord le plan sans `--apply` ; l'application recharge les contrôleurs
et ACL de brokers et peut interrompre brièvement leurs connexions.

```powershell
py -3 deploy/fleet/admit-storage.py --runtime deploy/secure-v3/.runtime --compose deploy/secure-v3/compose.yaml --override deploy/secure-v3/.runtime/compose.lan.json --override deploy/secure-v3/.runtime/client.gateway.json --control-image caribou/windows-control:dev --node JULES1 --zone A --maximum-gib 1
# Après validation du plan, même commande avec --apply.
```

Puis générer un **nouveau dossier** de bundle avec les mêmes options et :
`-Roles 'viewer,publisher,storage' -NodeID JULES1 -Owners @('client-UUID_REEL')`.
Les propriétaires autorisés viennent de la connexion Java réelle ; ils ne
sont pas inventés. Plusieurs propriétaires peuvent être admis explicitement.
Pour un nouveau producteur, commencer par son accès viewer/publisher, obtenir
son identifiant Java puis provisionner ses espaces avec `provision-tenant.ps1`.
Une nouvelle personne n'obtient pas implicitement un espace chez tous les Edge.

Le nœud JULES1 n'est actif qu'après son lancement et son admission mTLS LAN.
L'ajout conserve les anciens nœuds, fichiers, volumes et clés. La sauvegarde de
configuration est conservée dans le runtime. Ne pas réexécuter l'admission pour
modifier un nœud existant : une migration explicite est requise.

`--image-commit SHA_COMPLET --registry-user COMPTE` dans `secure_client.py bundle`
permet aussi de résoudre les images GHCR au digest, avec vérification de la
révision OCI et de la capacité portail. Les nouvelles images ne seront publiées
que par la CI de main après merge humain. Le mode `-IncludeImages` évite cette
attente et fournit `images.tar` (images locales figées par leur ID).
L'archive des images n'inclut pas Python/Docker ni les dépendances LAN à installer
au premier lancement ; Internet ou un environnement LAN déjà préparé reste requis.

## Sur le PC de Jules (192.168.2.64)

Transférer le dossier privé entier, par un canal privé, puis dans ce dossier :

```powershell
pwsh -NoProfile -File ./start.ps1
# Au premier lancement : 0 = sans stockage ; 0,5 = ajoute 0,5 Gio.
# Avec le rôle storage : quota demandé, ou valeur explicite sous le plafond :
pwsh -NoProfile -File ./start.ps1 -QuotaGiB 0.5 -InterfaceIPv4 192.168.2.64
```

Le portail est `http://localhost:18790/` et le direct
`http://localhost:18790/live/`. Se connecter avec son compte Java.
Le travail existant crée un compte au premier login si l'adresse n'existe pas :
choisir son propre mot de passe, ne pas partager les comptes de test.

Pour produire, après admission des espaces de stockage de ce propriétaire :

```powershell
./publish.ps1 -Email MON_COMPTE -FilePath C:/Videos/demo.mp4 -Title 'Essai Jules'
# Ou source OBS ; attendre CREATED, puis démarrer OBS :
./publish.ps1 -Email MON_COMPTE -Title 'Caméra Jules'
```

OBS : serveur `rtmp://127.0.0.1:22935/live`, clé `stream`, H.264/AAC et GOP 2 s.
Le direct est privé par défaut ; `-Public` le publie aux autres spectateurs.
Les mots de passe sont demandés interactivement et transmis au processus par
stdin. Le contrôle reste MQTT/TLS ; l'entrée live est RTMP/TCP et la lecture
HLS/HTTP dans le portail, puis mTLS sur le LAN. **Ce n'est pas RTP/UDP.**

Les paquets de démo permettent aussi `./publish.ps1 -UseBundleAccount -FilePath
C:/Videos/demo.mp4 -Public`, sans mot de passe dans la commande. Le compte privé
est vérifié contre les propriétaires de ce paquet. Les mesures producteur sont
exportées dans `.runtime/measurements/` ; le lecteur conserve son propre export.

Le quota est un **plafond logique des blocs du projet**, réservés et stockés,
partagé entre les espaces utilisateurs de cet Edge. Il ne réserve pas une
partition du disque. Une écriture réelle peut échouer si le disque est plein. Après premier
lancement, changer le quota est refusé sans migration explicite, sans effacer
les données. Les anciens A1/B1 sont configurés à 256 Mio chacun ; ce n'est pas
une valeur universelle imposée à tous les appareils.

Le choix 0 ne lance aucun Edge/broker/worker ni espace de stockage local. Les
choix sont conservés aux relancements ; leur modification demande une migration
explicite, sans supprimer les fichiers d'état pour contourner les contrôles.

Le stockage utilise des fichiers dans les volumes Docker par propriétaire,
avec métadonnées locales persistantes. SQLite Central référence les placements ;
ce ne sont pas des BLOB vidéo SQLite sur chaque appareil.

## Vérifier avant la démo

```powershell
docker compose -f ./compose.json ps
docker compose -f ./compose.json logs --tail 80
Test-NetConnection 192.168.2.51 -Port 19693
Test-NetConnection 192.168.2.51 -Port 19683
Get-Content ./.runtime/discovery-0.log -Tail 20
Get-Content ./.runtime/discovery-1.err.log -Tail 20
```

Pour le stockage, vérifier dans l'autre sens les ports de Jules 8443/8883/9443
et l'admission dans sa zone. MQTT et HTTPS nécessitent les certificats du même
banc ; un port ouvert ne suffit pas. Ne pas ouvrir globalement le pare-feu.
Les règles Windows éventuelles doivent être limitées au réseau de laboratoire
et aux programmes/ports nécessaires. Le script n'ajoute aucune règle.

Si le Wi-Fi filtre le multicast UDP 5353 ou isole les clients, la découverte
automatique ne peut pas fonctionner : diagnostiquer ce point, sans prétendre
qu'un stockage est actif. `-NoDiscovery` sert aux diagnostics contrôlés seulement.
Après redémarrage de Windows, relancer `start.ps1` pour les aides LAN natives ;
ce ne sont pas des services Windows persistants. Les conteneurs ont leur propre
politique de redémarrage et conservent leurs volumes.

`--client-port-offset 100` crée un banc local distinct (18890/18891/23035) ;
`--port-offset` concerne séparément les ports des contrôleurs d'un banc isolé.
Une IP du PC contrôleur différente impose de mettre à jour sa publication LAN
et les adresses de bootstrap des bundles, pas de régénérer les clés.

## Preuves et limites

`smoke-client.py` vérifie connexion, original SHA-256, lecture Range, isolation
utilisateur et refus Host/Origin avec une vidéo privée existante. Exemple :

```powershell
py -3 deploy/fleet/smoke-client.py --portal http://localhost:18790 --email PROPRIETAIRE --other-email AUTRE_COMPTE --video ID_VIDEO --expected-sha256 SHA_ORIGINAL --output preuve-client.json
```

Les tests `test_secure_client.py` utilisent des certificats synthétiques et un
faux Docker : ils vérifient les contrats, **pas** un stockage réel distant.
Le test réel de portail sur le PC contrôleur a récupéré 1 484 048 octets avec
SHA-256 `760377b11c6480dbd192017a65677a31563d5b7007f309e8970eff46168a3e1e`,
et refusé la lecture par un autre compte. Les tests Go/race/vet et Fleet sont
exécutés séparément. Le nouveau JULES1 a aussi été testé **sur le PC contrôleur**
avec 0,5 Gio : admission MQTT, routes LAN, fichiers réels de 31/65 537 octets
relus après redémarrage ; corruption et croisement d'utilisateurs refusés.
`smoke-storage.py --bundle CHEMIN --allow-test-restart --output preuve.json`
reproduit ce test des backends ; il ne teste pas à lui seul Go/SIS/IBS.
Le nœud local temporaire a été arrêté avec un ledger sans blocs catalogue pour
laisser Jules installer la même identité sur son PC. Ses volumes sont conservés.
Le nouvel agent a publié deux fragments signés puis reconstruits à l'identique
via Go et A1/B1. Le choix 0 a été installé réellement, puis le compte PC-5 a
publié un direct public avec export de mesures, relu par le compte Jules.
Le PC physique de Jules, ses règles réseau, OBS/caméra,
les RPi/Jetson et la charge multi-producteurs restent à valider sur place.
Les mesures live existantes restent des horodatages FFmpeg/lecteur ; elles ne
mesurent pas seules la latence optique caméra-écran.

Les contrôleurs tolèrent jusqu'à une seconde d'avance d'horloge d'un Edge
authentifié : seules les dates légèrement futures des messages vivants sont
ramenées à l'heure de réception. Les messages retenus/périmés ne gagnent aucune
fraîcheur ; versions, générations et scheduler ne changent pas. Au-delà de cette
borne, un diagnostic demande de synchroniser Windows/WSL. Cette correction côté
contrôleur reste compatible avec les paquets clients construits à `f55fadc`.
Leurs archives privées restent inchangées ; elles ne lancent aucun contrôleur.
