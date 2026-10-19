# Banc V3 : signatures locales et stockage utilisateur isolé

Les ajouts réseau/QoS/opérations et le déploiement laboratoire sont décrits dans [RESEAU-QOS-OPERATIONS.md](RESEAU-QOS-OPERATIONS.md).

Ce profil est séparé des bancs `caribou-integration`, `caribou-controllers` et `caribou-edge-v2`. Il réutilise Auth Java, le scheduler Go et l’allocator du Zone Controller, Central SQLite, les vrais Edge Go et le découpage/reconstructeur Python du collègue. Le placement n’est pas réimplémenté. Aucun merge de PR n’est nécessaire pour tester cette branche.

## Décision explicitement validée

Le 19 octobre, l’utilisateur a choisi **« Commande ponctuelle sans service privilégié »**. La création des espaces est donc une commande exécutée par l’administrateur Docker. Ni le web, ni les propriétaires, ni les Edge ne reçoivent le socket Docker ou le droit de créer des conteneurs. Il n’existe aucun service provisionneur permanent.

L’administrateur Docker garde naturellement le contrôle de l’hôte et des données Docker. Les applications ne reçoivent pas ce pouvoir. La revue automatique avait refusé un service avec socket ; les vérifications du Compose et des conteneurs ont confirmé zéro montage de socket dans le mode finalement retenu.

## Lancer sur Windows, PowerShell 7

Depuis la racine du dépôt, Docker Desktop en mode Linux :

```powershell
./deploy/secure-v3/start.ps1
# Sur ce PC uniquement si les téléchargements exigent le proxy Docker :
# ./deploy/secure-v3/start.ps1 -BuildProxy http://http.docker.internal:3128

./deploy/secure-v3/smoke.ps1
./deploy/secure-v3/verify.ps1
./deploy/secure-v3/resilience.ps1
```

Le premier script construit quatre images AMD64, provisionne une fois les secrets/certificats persistants et démarre 16 services. Il recharge les ACL des trois brokers pour éviter les copies périmées. `-SkipBuild` réutilise les images présentes. Les anciens bancs et leurs volumes ne sont pas arrêtés.

- Lecteur/catalogue : <http://localhost:19690/integration/> ; frontend : <http://localhost:19690/>.
- Agent propriétaire A : <http://localhost:19591/> ; agent B : <http://localhost:19592/>.
- Publication V3 : connexion puis fichier MP4 dans l’agent propriétaire. Les clés privées IBS restent dans son volume local. Le même compte Auth Java sert ensuite au lecteur.
- Les comptes `secure-owner-a@example.invalid` et `secure-owner-b@example.invalid`, mot de passe `OnlyForLocalTest!2026`, sont des **fixtures locales de test**. Chaque agent persistant est lié à son premier propriétaire ; le test refuse d’en écraser le wallet.
- L’upload brut V2 reste disponible dans son ancien profil. Le profil V3 renvoie HTTP 428 pour une publication non signée ; son parcours de signature passe explicitement par le portail propriétaire.

## Ajouter un véritable utilisateur

1. Se connecter à son agent propriétaire local : la réponse affiche le `owner_id` stable `client-…` d’Auth Java.
2. L’administrateur crée son espace sur chaque Edge pouvant recevoir ses blocs :

```powershell
./deploy/secure-v3/provision-tenant.ps1 -Owner client-XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX -Node A1
./deploy/secure-v3/provision-tenant.ps1 -Owner client-XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX -Node B1
```

Remplacer l’exemple par le véritable UUID ; la commande le valide. Chaque couple utilisateur/Edge reçoit un conteneur non-root, un volume nommé, un jeton distinct et un certificat serveur. Aucune publication de port sur l’hôte. Les noms sont déterministes ; une seconde exécution vérifie les labels/le volume et réutilise l’espace, sans supprimer de données. Une identité incohérente provoque un refus explicite. Limite de laboratoire : 16 utilisateurs par nœud, 128 MiB RAM et 0,5 CPU par conteneur. Le quota global du projet reste tenu par le ledger et le scheduler existants ; ce volume Docker n’est pas une limite disque physique par utilisateur.

Les certificats de stockage sont émis ponctuellement par une autorité intermédiaire **limitée au DNS de ce nœud et à l’usage serveur**. Elle ne permet pas de créer un certificat client `admin`, `web` ou Edge. Sa clé n’est jamais montée dans un service permanent. Le serveur de stockage accepte le certificat de son Edge seulement, exige son propre jeton et refuse les autres namespaces utilisateur.

L’Edge lit `.runtime/tenant-A1/tenants.json` ou `tenant-B1/tenants.json` via un montage **en lecture seule**. Ces fichiers contiennent des secrets, sont ignorés par Git et ne doivent pas être joints à un rapport. Le client d’HTTP/TLS vérifie la chaîne de confiance ; il n’y a pas de mode `insecure`.

Les anciens objets du namespace `project` restent à leur place et lisibles par leur mapping historique. Aucune migration ou suppression implicite. Pour un nouveau mapping utilisateur dans le profil isolé, une entrée de registre absente ne provoque pas de retour silencieux vers le stockage commun.

## Ce que les tests font réellement

`smoke.ps1` appelle **explicitement la commande d’administration** pour les deux propriétaires de démonstration. Il génère deux MP4 H264/AAC, ajoute un atome MP4 `free` valide afin de forcer plusieurs blocs de tailles variables, signe les assets localement, vérifie leurs preuves avant placement et relit les octets réellement stockés sur A1 et B1. La source reconstruite et celle lue doivent être identiques, y compris l’atome `free`.

Une réservation temporaire de capacité force le débordement sur l’autre zone par le scheduler existant. Cette réservation ne contient **aucun fichier et aucun ACK STORED**. Elle est annulée dans `finally` via le catalogue. Les fichiers des vidéos, eux, sont tous écrits et relus réellement.

`verify.ps1` vérifie chaque fichier et son SHA256, les refus d’un autre namespace, d’un autre jeton et du certificat de l’autre Edge ; les droits privés sur le web ; l’ajout d’une part d’identité dans l’autre zone et la lecture d’une ancienne signature. Une commande de test éphémère reçoit uniquement les certificats et registres nécessaires, jamais la clé de l’autorité racine ou le socket Docker.

`resilience.ps1` altère puis restaure un bloc de sa propre fixture dans `finally`, teste un bloc absent, arrête le hub pendant un échange pair-à-pair, puis redémarre les services et les quatre conteneurs de stockage. Il compare clés publiques persistantes, quotas et fichiers. Le cache de lecture V3 peut servir pendant une minute un fichier déjà vérifié : le test de corruption utilise `/original`, qui reconstruit fraîchement depuis les Edge.

## Diagnostic et arrêt

```powershell
docker compose -f deploy/secure-v3/compose.yaml ps
docker compose -f deploy/secure-v3/compose.yaml logs --tail 80 central zone-a zone-b edge-a edge-b web
docker ps --filter label=caribou.project=caribou-secure-v3
Get-Content deploy/secure-v3/results/owner-a.json
Get-Content deploy/secure-v3/results/resilience.json

# Arrêter les services sans effacer de volume :
docker compose -f deploy/secure-v3/compose.yaml stop
docker ps -q --filter label=caribou.project=caribou-secure-v3 | ForEach-Object { docker stop $_ }
```

Un espace utilisateur est volontairement hors des services Compose statiques. `compose down` seul ne l’arrête pas. Le dernier filtre n’affecte que les espaces du banc V3. Ne pas utiliser `down -v`, `volume prune` ou une commande de suppression globale.

## Raspberry Pi/Jetson et limites de livraison

Les Dockerfiles acceptent `linux/amd64` et `linux/arm64`. Le workflow V3 construit les quatre composants ARM64 ; cela ne prouve pas le fonctionnement sur un Raspberry Pi/Jetson physique. Les conteneurs ARM64 démarrés sur ce PC relèvent de l’émulation. Le LAN, ses pare-feux, les IP réellement annoncées et les performances matérielles restent à vérifier au laboratoire.

Les brokers A et B ont des liens mTLS directs entre pairs **configurés** ; leurs ACK continuent sans le hub. Cela ne constitue pas encore la découverte/admission LAN autonome pilotée par les zones. Aucun placement n’est effectué sans les autorités Go/SQLite existantes.

Restent à intégrer et à valider : découverte dynamique par zone et reconfiguration des liens ; devis/filtres de latence/prix dans le scheduler ; opérations complètes de modification/suppression et délégation révocable sur le parcours métier. Les primitives cryptographiques de signature d’opération/délégation ont des tests unitaires, sans prétendre que leurs endpoints et transactions de reprise sont déjà raccordés.

Le SIS utilise les équations du collègue et son générateur de matrice, avec H3=256 bits et une suite IBS/Hess versionnée BLS12-381/hash-to-curve. Les tests montrent des refus et une intégration réelle ; ils ne constituent pas une preuve cryptographique des paramètres ou du générateur. SHA256 reste un contrôle distinct. Ce banc n’est pas présenté comme une conformité complète à l’article.

Documentation des ACL locales des bridges : [configuration officielle Mosquitto](https://mosquitto.org/man/mosquitto-conf-5.html).
