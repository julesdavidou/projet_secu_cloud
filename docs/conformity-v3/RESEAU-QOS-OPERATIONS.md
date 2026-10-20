# Réseau, QoS/prix et opérations signées — livraison V3

Ce lot prolonge `codex/conformity-v3-review` et la PR draft #6. Aucun merge. Les versions des collègues, Auth Java, l’allocator de Dan, le placement Go, le catalogue SQLite, le stockage HTTP et le frontend restent utilisés. Les anciens bancs ne sont pas arrêtés et leurs volumes ne sont pas migrés.

## Décisions et raisons

| Besoin | Choix implémenté | Conséquence pratique |
|---|---|---|
| Découverte automatique | DNS-SD/mDNS `_caribou-edge._tcp.local`, puis sondes mTLS par les Zone Controllers | Une annonce n’est jamais une autorisation. Identité, zone, génération et certificat sont vérifiés. |
| Admission | Identités/quota provisionnés une fois par l’administrateur ; IPv4 privées ; bail réseau de 30 s | Un appareil inconnu ou un autre certificat ne peut pas prendre la place d’un Edge. Pas d’inscription publique sans contrôle. |
| Docker Desktop et multicast | Petit assistant Python natif sur le PC/LAN, qui transmet les candidats aux zones | Le multicast ne dépend pas de sa traversée du NAT Docker Desktop. La bibliothèque DNS-SD existante est utilisée. |
| Plusieurs brokers | Liens Mosquitto directs générés depuis les pairs admis ; seuls les topics de l’acteur local sont exportés | Pas de `topic # both`. mTLS, identités stables, déduplication et ACK métier sont conservés. MQTT 3.1.1 et 5 restent interopérables. |
| Coupure du hub | Les zones conservent leurs baux réseau vérifiés localement et peuvent les renouveler sans le hub | La découverte des pairs et leurs liens directs survivent au-delà du bail initial. Aucun nouveau placement n’est déclaré commis sans Central. |
| Changement d’adresse | Adresse de connexion distincte du nom TLS stable ; SNI et contrôle des certificats conservés | Ni `insecure`, ni changement du `node_id`, ni réécriture des signatures ou des extents. Le helper réannonce les changements de l’interface DHCP automatique. |
| Découverte d’un pair hors zone | Seules ses routes réseau sont échangées | Aucun registre détaillé de capacité étranger et aucun second allocateur. La mobilité reste celle du protocole existant. |
| Faible latence | Sondes réelles de 64 KiB, filtre RTT/débit local et mesures inter-zones injectées dans le classement existant | La sonde mesure zone→Edge ; le classement inter-zone utilise son propre trajet mesuré. Ce ne sont pas des garanties de latence vue par le lecteur. |
| Prix et budget | Microcrédits de démonstration par GiB/heure, plafonds et devis calculés par le même scheduler | Le devis ne réserve rien ; le placement revérifie le budget. Entiers, arrondi par nœud, protection contre les débordements ; aucun paiement ni débit d’un compte bancaire. |
| Qualité vidéo | Aucune baisse de résolution/bitrate pour satisfaire le prix ou la RTT | Une contrainte impossible provoque un refus explicite. Les sources H264/AAC testées sont restituées octet pour octet. |
| Opérations | Preuves IBS versionnées, nonce persistant, révision attendue et hash du commit précédent ; transaction SQLite | Refus des substitutions, reprises idempotentes, contrôle de concurrence. Le web ne devient pas une autorité de placement. |
| Délégation | Sous-clé `DK = S + y H1(mandat)`, certificat IBS du propriétaire, terme supplémentaire de couplage à la vérification | Le worker reçoit une sous-clé liée au mandat ; aucune clé privée propriétaire n’est transmise. Il ne peut pas redéléguer. |
| Limites de délégation | Vidéo racine, acteur, opérations, durée ≤1 h, révision maximale, taille et budget maximaux ; révocation Central | Le défaut du portail est 5 minutes. Expiration/révocation sont vérifiées à l’admission puis dans la transaction de modification de la tête. |
| Insertion/suppression/modification | Éditions ordonnées de blocs de la source ; nouveau découpage, nouveau vecteur SIS et nouvelle signature ; nouvelle version immuable | Pas de changement arbitraire de l’ordre ou de la taille sans vérification. La source modifiée doit encore passer le pipeline vidéo du collègue. |
| Conservation | Les anciennes versions et leurs octets restent stockés ; `delete` crée une tombstone | Aucun effacement implicite et aucun faux crédit de quota. La suppression de blocs change la nouvelle version ; la purge physique de l’historique n’est pas implémentée. |
| Isolation Docker | Commande d’administration ponctuelle, conteneur/volume par utilisateur/Edge | Aucun service avec socket Docker. Ce choix respecte l’accord explicite de l’utilisateur. |

Les routes découvertes concernent HTTP, reconstruction et brokers. Les autorités cryptographiques et le contrôleur PC ont des adresses de bootstrap administrées. Ce profil ne remplace ni DHCP ni le pare-feu et ne prétend pas être un système d’admission universel pour appareils non provisionnés. Le profil laboratoire fourni comporte les zones A et B.

## Démarrer et vérifier sur le PC Windows

PowerShell 7, Docker Desktop en conteneurs Linux, depuis la racine du dépôt :

```powershell
./deploy/secure-v3/start.ps1
# Seulement si ce PC exige ce proxy pour les téléchargements Docker :
# ./deploy/secure-v3/start.ps1 -BuildProxy http://http.docker.internal:3128

./deploy/secure-v3/discovery.ps1 -WithVod
./deploy/secure-v3/operations.ps1
./deploy/secure-v3/resilience.ps1

docker compose -f deploy/secure-v3/compose.yaml ps
docker compose -f deploy/secure-v3/compose.yaml logs --tail 80 central zone-a zone-b edge-a edge-b web
```

Frontend : <http://localhost:19690/>. Portails propriétaires : <http://localhost:19591/> et <http://localhost:19592/>. Un portail persistant reste lié à son premier propriétaire. Les deux comptes de test et leur mot de passe sont décrits dans `LANCEMENT-ET-ISOLATION.md` ; ce sont des fixtures, pas des comptes de production.

Le portail prépare un paquet signé, affiche un devis sans réservation, puis permet sa publication/reprise avec le même identifiant. Il expose aussi partage/retrait de partage, tombstone, révocation, création d’un mandat et remplacement délégué. Pour un nouvel utilisateur, exécuter explicitement `provision-tenant.ps1` sur les Edge prévus avant la publication. Une entrée absente n’entraîne jamais un repli vers le stockage partagé.

Les paramètres supplémentaires sont accessibles par `service_policy` :

```json
{
  "max_rtt_ms": 2000,
  "min_probe_bytes_per_second": 100000,
  "max_microcredits_per_gib_hour": 1000000,
  "budget_microcredits": 100000,
  "retention_hours": 24
}
```

`retention_hours` sert au calcul du devis ; il ne programme aucune suppression. Les tarifs se modifient dans `deploy/secure-v3/topology.json`, champ `microcredits_per_gib_hour` de chaque nœud, puis se rechargent par `start.ps1`. Les contraintes absentes gardent le comportement historique. Le devis et le reçu exposent coût, unités, observations et horodatages. Un échantillon de plus de 30 secondes ne satisfait plus une nouvelle admission QoS.

## API des opérations

L’agent local authentifie le compte Java et exige son jeton CSRF. Il crée lui-même les preuves ; le navigateur ne reçoit aucune clé privée. Exemples de corps JSON envoyés à l’agent :

```json
{"video_id":"v-…","kind":"share","action_id":"op-identifiant-unique-au-moins-20-caracteres"}
```

Routes locales : `POST /owner/operate`, `/owner/delegate`, `/owner/delegated/apply`. Les trois passent par la session locale et `X-CSRF-Token`. Pour une reprise, conserver le même `action_id` et le même corps : changer le corps sous la même identité est refusé. Les paquets et preuves sont conservés dans l’outbox privée du service. Deux premières requêtes simultanées portant la même action sont sérialisées par identité et nonce : une seule version est créée et les deux réponses portent le même commit. Les actions indépendantes ne partagent pas ce verrou.

`/owner/operate` accepte `share`, `unshare`, `delete`, `revoke`, `replace`, `insert_blocks`, `delete_blocks`, `modify_blocks`. Un remplacement contient `source`, encodée en base64. Les éditions de blocs utilisent :

```json
{"kind":"modify_blocks","edits":[{"index":1,"delete_count":1,"blocks":["BASE64_DU_BLOC"]}]}
```

Les index se rapportent à la source avant l’opération ; les éditions sont ordonnées, sans chevauchement. `insert_blocks` impose zéro suppression, `delete_blocks` impose zéro insertion. La dernière taille peut varier. Le propriétaire peut publier une nouvelle version lui-même ou confier ces opérations à un worker via un mandat. Sa durée commence à la signature, avant les calculs et le transfert : un mandat trop court peut expirer avant son installation et sera refusé. Le test d’expiration attend réellement l’échéance d’un mandat de 60 secondes, afin de laisser passer les calculs sur un runner chargé ; le portail conserve son défaut de 5 minutes. Un mandat n’autorise pas les autres vidéos, les autres comptes ou les opérations absentes de sa liste.

`GET /api/videos/:id/operations` fournit la tête autoritaire au propriétaire, y compris après tombstone. Les URL de lecture et le catalogue vérifient l’état Central à chaque accès. Les anciennes URL de médias en cache ne permettent pas de contourner un retrait de partage ou une tombstone. La révocation d’un mandat empêche de nouvelles opérations et conserve la vérifiabilité des signatures déjà admises.

## Préparer les Raspberry Pi / Jetson sans remplacer les nœuds PC

Le matériel physique n’est pas accessible dans cette session. Les étapes suivantes préparent le déploiement ; elles ne constituent pas une preuve de fonctionnement matériel.

1. Sur le PC, provisionner les nouvelles identités en conservant A1/B1 :

```powershell
./deploy/secure-v3/start.ps1 -TopologyPath ./deploy/secure-v3/topology.lab.example.json
$pcIP = Read-Host "Adresse IPv4 privée du PC sur le LAN du laboratoire"
./deploy/secure-v3/enable-lan.ps1 -BindAddress $pcIP -Apply
python ./deploy/secure-v3/export_edge.py --node PI1 --controller-ip $pcIP --output ./deploy/secure-v3/.runtime/bundles/PI1
python ./deploy/secure-v3/export_edge.py --node JETSON1 --controller-ip $pcIP --output ./deploy/secure-v3/.runtime/bundles/JETSON1
```

L’export refuse un dossier existant et refuse de réaffecter A1/B1. Chaque bundle contient uniquement ses certificats Edge/broker/reconstructor, son émetteur de certificats de stockage contraint, ses secrets Edge et la configuration publique. Aucune clé racine, clé d’autorité de zone, clé propriétaire ou jeton opérateur n’est exporté. Transférer chaque bundle à son seul appareil par un canal privé ; ne jamais le mettre dans Git.

L’activation LAN expose uniquement les ports mTLS sur l’IP demandée : broker 19683, Central 19688, web API 19689, zones 19691/19692, autorités 19581/19582, sessions Auth 19583, Edge PC 19681/19682, brokers PC 19684/19685 et workers PC 19584/19585. Les portails propriétaires HTTP et le frontend loopback ne sont pas exposés au LAN. Le pare-feu du PC doit laisser passer ces ports depuis le sous-réseau du laboratoire ainsi que DNS-SD UDP5353 pour l’assistant natif ; aucun changement de pare-feu n’est exécuté implicitement.

2. Construire/transférer les images de l’architecture voulue, avec des tags distincts des images PC :

```powershell
docker buildx build --platform linux/arm64 -f deploy/lab/Dockerfile --load -t caribou/secure-v3-control:lab-arm64 .
docker buildx build --platform linux/arm64 -f deploy/secure-v3/Dockerfile.security --load -t caribou/secure-v3-security:lab-arm64 .
docker image save -o ./deploy/secure-v3/.runtime/edge-arm64-images.tar caribou/secure-v3-control:lab-arm64 caribou/secure-v3-security:lab-arm64
```

Le bundle généré doit être validé avec `docker compose -f compose.json config --quiet` sur chaque appareil avant lancement.

Sur chaque appareil, charger l’archive avec `docker image load -i edge-arm64-images.tar`. `eclipse-mosquitto:2.0.22` est aussi nécessaire ; le tirer avant le laboratoire s’il n’aura pas Internet. Les tags ARM n’écrasent pas les tags AMD64 du banc PC.

3. Depuis le bundle PI1 sur l’appareil, préparer le réseau et les espaces utilisateurs avant de lancer l’Edge :

```bash
# Pour les fichiers copiés : propriétaire = compte courant ; groupe lecture = 10001.
docker run --rm --user "$(id -u):10001" --cap-drop ALL --security-opt no-new-privileges:true \
  --mount "type=bind,source=$PWD/.runtime,target=/runtime" caribou/secure-v3-control:lab-arm64 \
  sh -c 'find /runtime -type d -exec chgrp 10001 {} \; -exec chmod 750 {} \; ; find /runtime -type f -exec chgrp 10001 {} \; -exec chmod 640 {} \;'
docker compose -f compose.json up -d broker reconstructor
# PowerShell 7/pwsh est nécessaire pour la commande d’administration fournie.
pwsh ./provision-tenant.ps1 -Node PI1 -Owner client-UUID_REEL \
  -Project caribou-edge-pi1 -ControlImage caribou/secure-v3-control:lab-arm64 \
  -SecurityImage caribou/secure-v3-security:lab-arm64
docker compose -f compose.json up -d edge
```

Remplacer `client-UUID_REEL` par l’identifiant stable obtenu après connexion Java ; répéter pour les propriétaires du test. Pour Jetson, utiliser `JETSON1` et `caribou-edge-jetson1`. Aucun conteneur n’obtient le moteur Docker. La commande est exécutée ponctuellement par l’administrateur de l’appareil. Si `pwsh` ou Docker manque sur le matériel, c’est un prérequis à installer explicitement, pas une intégration prétendument disponible.

4. Sur le PC, lancer l’assistant de découverte natif pour chaque zone, dans des terminaux séparés :

```powershell
python -m venv ./deploy/secure-v3/.runtime/lan-venv
$lanPython = "$PWD/deploy/secure-v3/.runtime/lan-venv/Scripts/python.exe"
& $lanPython -m pip install -r ./deploy/secure-v3/lan-requirements.txt
& $lanPython ./deploy/secure-v3/lan.py browse --zone-url https://zone-A:19691 --zone-ip 127.0.0.1 --tls-dir ./deploy/secure-v3/.runtime/pki/zone-A
# Second terminal : zone-B:19692 et pki/zone-B.
```

Pour permettre aux appareils physiques de relire aussi les fichiers des nœuds PC A1/B1, annoncer leurs ports LAN dans deux autres terminaux. Ne pas annoncer leurs IP internes Docker au réseau physique :

```powershell
& $lanPython ./deploy/secure-v3/lan.py advertise --node A1 --interface $pcIP --http-port 19681 --broker-port 19684 --worker-port 19584
& $lanPython ./deploy/secure-v3/lan.py advertise --node B1 --interface $pcIP --http-port 19682 --broker-port 19685 --worker-port 19585
```

Python 3.11+ est nécessaire. `--interface auto` utilise les interfaces LAN ; choisir explicitement une IPv4 privée avec `--interface` en présence de plusieurs réseaux. Les certificats de zone restent sur le PC ; ils ne sont pas copiés aux Edge.

Sur PI1, dans deux terminaux, avec son propre certificat Edge en lecture seule pour le maillage :

```bash
python3 -m venv .runtime/lan-venv
.runtime/lan-venv/bin/python -m pip install -r lan-requirements.txt
.runtime/lan-venv/bin/python lan.py advertise --node PI1 --http-port 8443 --broker-port 8883 --worker-port 9443
# Second terminal ; remplacer ADRESSE_PC par la même IP de bootstrap.
.runtime/lan-venv/bin/python lan.py mesh --self-node PI1 --zone-url https://zone-A:19691 \
  --zone-ip ADRESSE_PC --tls-dir .runtime/pki/edge-PI1 --topology .runtime/config.json --output .runtime/lan-PI1
```

Sur Jetson, utiliser `JETSON1`, `zone-B:19692` et les chemins correspondants. Les assistants annoncent/adaptent les adresses DHCP ; les Zone Controllers valident les routes, tandis que Go/SQLite gardent seuls la responsabilité du placement. Un nouvel appareil ne reçoit pas automatiquement les fichiers historiques d’un autre identifiant.

## Tests et preuves : limites exactes

| Vérification | Nature |
|---|---|
| Compilation, tests Go, race, vet dans les trois modules | Exécution Linux AMD64 dans Docker ; App Control Windows empêche les exécutables de test Go natifs |
| Compatibilité Python du banc V2 | 17 tests depuis un montage en lecture seule, exécutés sans privilèges ; import des services sans création implicite de `/data` |
| Crypto, contexte, sous-clé et terme de couplage délégué | Vraie bibliothèque `py_ecc`, vrais tirages et calculs ; pas un mock cryptographique |
| Transactions CAS/révocation SQLite | Vrai SQLite ; double explicite du résultat de vérification dans le test unitaire |
| Contrat de bridge/export | Fixtures explicites, fausses chaînes de certificat ; teste la sélection des rôles exportés, pas le matériel |
| Découverte DNS-SD | Vrais paquets multicast entre processus séparés sur le réseau Docker ; admission HTTPS/mTLS réelle |
| Coupure du hub >30 s | Vrais brokers arrêtés/redémarrés, revalidation des baux et ACK métier directs réels |
| QoS/prix | Vraies sondes réseau ; refus de RTT/prix/budget, devis sans réservation puis coût au placement |
| Deux propriétaires/deux zones | Fichiers réellement écrits, relus et reconstruits ; la réservation temporaire de capacité ne simule aucun STORED |
| Édition déléguée | Deux premières soumissions simultanées du même nonce, puis source MP4 valide, insertion/suppression/modification et dernière taille variable, nouvelles signatures, reconstruction exacte après chaque opération |
| Contrôle d’accès | Session Java, refus hors mandat, retrait de partage d’un fichier déjà en cache, révocation, expiration, tombstone |
| Résilience | Corruption et absence de la seule fixture injectée, restauration dans finally ; clés/quota/fichiers et journal signés persistants après redémarrage |
| ARM64 | Construction d’images et exécution du binaire Go/health sous QEMU ; pas un Raspberry Pi ou Jetson physique |

Les fichiers `deploy/secure-v3/results/*.json` sont les sorties non secrètes des derniers tests. Ils sont ignorés par Git ; des copies sélectionnées sont conservées sous `docs/conformity-v3/preuves/`. Les journaux locaux contiennent les commandes et erreurs rencontrées ; ne jamais joindre `.runtime/` à un rapport.

Le 502 ponctuel du lot précédent n’a pas de cause racine établie. Ce lot corrige deux défauts de reprise identifiés : rejet d’un paquet identique déjà enregistré et renouvellement incohérent d’un bail de réservation après perte de réponse. Les reprises retransmettent le même paquet, le même identifiant et la même preuve. Les erreurs exposent maintenant `trace_id`, `stage` et `retryable`, sans clés ni cookies. Cela ne prouve pas que ces défauts causaient le 502 historique.

La conformité intégrale à l’article n’est toujours pas une affirmation justifiée : le profil SIS et son générateur demandent une évaluation cryptographique indépendante ; la sécurité de paramètres n’est pas prouvée par des tests, les mesures QoS ne sont pas un SLA, le prix est une démonstration et les essais physiques restent à faire. SHA256 reste un contrôle d’octets distinct de SIS/IBS. Le live/RTP reste hors de ce lot, conformément au mandat de ne pas le redévelopper.

Références d’implémentation réseau : [API python-zeroconf](https://python-zeroconf.readthedocs.io/en/latest/api.html) pour DNS-SD ; [documentation officielle Mosquitto](https://mosquitto.org/man/mosquitto-conf-5.html) pour les bridges et le contrôle des noms TLS.
