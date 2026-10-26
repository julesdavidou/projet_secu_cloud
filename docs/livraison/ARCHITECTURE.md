# Architecture et choix techniques

## Qui fait quoi ?

| Composant | Travail réel | Ce qu'il ne décide pas |
|---|---|---|
| Authentification Java | Comptes, sessions et `client_id` stable | Placement des blocs ou admission matérielle d'un Edge |
| Frontend et API web Go | Catalogue visible, droits, upload signé, lecture et profils utilisateur | Algorithme de placement |
| Agent propriétaire local | Garde le portefeuille privé, prépare les signatures et les opérations autorisées | Quota annoncé par un Edge |
| Central Controller | Catalogue SQLite, placements, validation des preuves de stockage, versions et états des directs | Nouveau placement parallèle au scheduler |
| Zone Controller | Registre de sa zone, observations réseau, admission de découverte et orchestration du placement | Identité librement inventée par un annonceur LAN |
| Scheduler Go | Choisit les destinations à partir du manifeste, des capacités et de la politique de service | Stockage vidéo dans SQLite |
| Edge Go et passerelle de contrôle | Réserve sa capacité projet, reçoit les unités, vérifie taille/SHA-256, annonce les ACK métier | Déclare un succès sur un simple ACK MQTT |
| Stockage propriétaire/Edge | Conserve les fichiers dans son volume isolé | Accès aux volumes d'autres propriétaires |
| Worker de reconstruction | Récupère les blocs autorisés, réordonne, vérifie et reconstitue le média | Placement ou possession des clés maîtresses de zone |
| Autorité SIS/IBS de zone | Émet les éléments de clé d'identité selon le profil cryptographique versionné | Équivalence entre une session Java et une signature IBS |
| Brokers MQTT | Transportent commandes, annonces, réponses et échanges entre pairs | Catalogue autoritaire ou contenu vidéo transporté intégralement |

Central, les zones, Java et le frontend peuvent être regroupés sur un seul PC. Les noms A/B sont des zones logiques : ils ne désignent pas automatiquement des machines physiques distinctes. Un Jetson n'est contrôleur que si on choisit et installe ce rôle ; le démarrage d'un Edge ne l'élit pas contrôleur.

## Un seul placement autoritaire

Le scheduler Go existant est conservé. Le registre et le contrôleur de zone existants sont réutilisés ; l'API web et les scripts Python ne choisissent pas un second placement concurrent.

Un envoi est découpé en unités dont les identifiants, tailles et empreintes sont connus. Des réservations bornées sont établies. Le catalogue n'accepte le stockage comme terminé qu'avec des preuves liées à l'unité, au nœud, au propriétaire et à la réservation attendus. Une reprise du même paquet conserve son identité ; elle ne transforme pas un nouvel octet en continuation silencieuse d'un ancien objet.

L'état `READY` signifie que toutes les unités attendues ont été déclarées stockées et vérifiées à l'écriture. Il ne promet pas qu'un appareil ne tombera jamais en panne ni que ses fichiers n'ont pas été modifiés depuis. La lecture effectue ses propres contrôles et refuse les blocs absents ou corrompus.

## Où sont les vidéos ?

Les vidéos sont des **fichiers dans les volumes Docker des Edge**, pas des BLOB SQLite sur chaque ordinateur. Le catalogue Central SQLite conserve les références, états, réservations, identités et opérations nécessaires pour retrouver et autoriser ces fichiers. Le contrôleur de zone conserve aussi ses informations de contrôle ; l'Edge conserve son ledger local et ses métadonnées.

Dans le profil sécurisé, chaque couple `(owner_id, Edge)` a un conteneur et un volume de stockage séparés. Un administrateur les prépare avec `provision-tenant.ps1`. L'application web ne reçoit pas le socket Docker. Les anciens fichiers et anciens profils ne sont pas migrés ou supprimés implicitement.

Les images Docker sont le programme. Les volumes sont les données persistantes. Recevoir une nouvelle image ou faire `git pull` ne transfère pas les vidéos ni les clés d'une installation précédente.

## Quotas et répartition

Le propriétaire du PC choisit combien il contribue, dans le plafond admis par l'administrateur. `1 Gio` vaut `1073741824` octets. Le ledger compte les blocs réservés et stockés de tous les propriétaires autorisés sur cet Edge.

Ce quota est logique : ce n'est ni une partition réservée ni un quota de disque garanti par le système de fichiers. Le disque peut manquer d'espace pour d'autres raisons. Les portefeuilles, captures et fichiers temporaires de l'agent de publication ont aussi un coût hors de cette contribution.

La répartition n'est pas un tour de rôle égalitaire. Elle dépend des capacités, de la zone source, des observations et des filtres du scheduler. Une démo avec deux appareils dans la zone A démontre une répartition entre appareils, mais pas un stockage multizone.

Aucune réplication automatique n'est ajoutée dans cette livraison. Un bloc unique détenu par une machine arrêtée devient indisponible. Les anciens médias restent référencés et conservés ; on ne prétend pas réparer automatiquement leur disponibilité.

## MQTT, HTTP et découverte

Dans le profil sécurisé, un hub Mosquitto est fourni sur le contrôleur et chaque Edge de stockage a son propre broker. Les bridges ont des topics bornés à l'origine autorisée ; ils ne relaient pas arbitrairement `#` dans les deux sens. Les identités de transport viennent des certificats mTLS. Un identifiant de connexion MQTT peut être différent d'une connexion à l'autre sans changer le `node_id` stable du stockage.

Les commandes, réponses et ACK métier utilisent les contrats MQTT existants, compatibles avec les échanges MQTT 3/5 intégrés. Un ACK QoS 1 confirme le transport, pas l'achèvement du stockage. Les corps et corrélations métier gèrent les reprises et doublons.

Les octets des blocs passent par HTTP/HTTPS. Le live reçoit OBS en RTMP/TCP et sert des fragments HLS/HTTP au navigateur. **RTP/UDP n'est pas implémenté par ce parcours.**

Un broker déjà présent dans le laboratoire ne devient pas compatible simplement parce qu'il écoute sur 1883/8883. Il faut les bonnes identités, les ACL, les topics et les bridges. Le profil Fleet HTTP historique emploie son propre broker sur 1884 ; ne pas le raccorder au banc mTLS par un simple remplacement d'adresse.

Les adresses viennent du DHCP existant. Le helper natif annonce les Edge en DNS-SD/mDNS ; les contrôleurs de zone les sondent avec authentification et publient des routes temporaires. La découverte n'accorde aucun droit à une machine inconnue : identité, zone et plafond doivent être admis auparavant.

Sur Docker Desktop, le multicast du LAN n'est pas supposé traverser le bridge Linux. Le helper tourne donc sur Windows et sélectionne l'interface LAN. Il met à jour les routes sans changer les noms TLS. Il ne configure ni le routeur, ni le Wi-Fi, ni un serveur DHCP.

## Identités et clés

Il faut distinguer :

- le compte Java et son `client_id` ;
- le `node_id` stable d'un Edge admis ;
- le certificat de transport d'un rôle ;
- les clés SIS/IBS d'identité du propriétaire et les autorités de zone ;
- l'identifiant de vidéo `v-…` et de direct `l-…` ;
- le nom privé du paquet Windows, par exemple `demo-pc`.

Le portefeuille d'un agent propriétaire est persistant et lié à son premier propriétaire. Le même agent ne doit pas être réutilisé avec le compte d'un collègue. Les paquets ne contiennent que les clés nécessaires à leurs rôles ; les clés maîtresses de zone et la clé racine ne sont pas distribuées aux participants.

Les opérations de partage, suppression logique, remplacement et délégation sont signées et versionnées. Une délégation est limitée et révocable ; une session ou un rôle réseau ne remplace pas cette preuve. Les anciennes versions et les fichiers ne sont pas détruits implicitement par une suppression du catalogue.

## SIS/IBS, empreinte et confidentialité

La chaîne V3 utilise le profil versionné `SIS-HESS-BLS12381-v3`, BLS12-381, un hash-to-curve et de l'aléatoire cryptographique. Les paramètres SIS du profil expérimental sont `l=128`, `m=4096`, `q=65537`, empreinte de 256 bits. Les équations historiques des collègues sont conservées à part ; les tests ne déclarent pas sûre une construction dont les paramètres ou la preuve mathématique restent à évaluer.

SHA-256 vérifie les octets et la cohérence du découpage. **SHA-256 n'est pas l'équivalent de SIS/IBS.** Les tests de signatures portent aussi sur le propriétaire, le contexte, les versions, les opérations et les blocs attendus.

TLS/mTLS protège les échanges. Les signatures protègent l'intégrité et l'authenticité ; elles ne chiffrent pas les fichiers vidéo au repos. Un volume Docker n'est pas un chiffrement de disque. Le coût de « déchiffrement vidéo SIS/IBS » ne doit donc pas être inventé dans les mesures.

## QoS et prix

La politique de service peut limiter le RTT observé, le débit de sonde minimal, le prix autorisé et le budget en microcrédits de démonstration. La durée de rétention demandée intervient dans le calcul du prix ; elle ne déclenche pas une suppression automatique.

Le prix dépend des octets et du nombre d'heures. Les répétitions identiques d'un paquet réutilisent ses identifiants et placements. Le devis ne réserve aucune capacité et est recontrôlé à la publication. Le profil live transmet un plafond par fragment ; ce n'est pas un paiement réel ni un plafond monétaire global garanti pour toute la durée du direct.

La sonde de zone ne mesure pas le trajet complet caméra-lecteur. Ni un devis accepté, ni un port ouvert, ni une valeur de RTT ne constituent une garantie de latence vidéo.
