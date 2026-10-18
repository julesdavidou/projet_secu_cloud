# Contrats effectifs des contrôleurs

Le nom historique du namespace reste `lab/placement/v1/` afin de réutiliser **sans modification** le bus MQTT5 de PR #1. Le broker du déploiement est distinct de celui du banc de régression. Ce préfixe ne signifie pas que les fichiers du mode real-http sont simulés. Toute future migration de namespace doit être versionnée côté producteurs, ACL et consommateurs.

## Transport MQTT5 effectif

Notations : R=`lab/placement/v1/`, actor=`central`, `admin`, `zone-{id}` ou `edge-{id}` provisionné. IDs validés, 128 caractères maximum (préfixe compris), pas de slash. Identités issues du JSON ; deny-by-default Mosquitto. Les ACL lient le dernier composant des topics requête/réponse au compte MQTT ; un champ JSON node_id n'est jamais la seule authentification.

| Topic | Émetteur → destinataire | QoS / retained / expiration | Contenu et garantie |
|---|---|---|---|
| R{target}/request/{sender} | Acteur provisionné autorisé → target | 1 / non / TTL métier | Request JSON `op, body, expires_at` ; MQTT5 ResponseTopic et CorrelationData 16 octets ; JSON strict, max 1 MiB |
| R{sender}/reply/{target} | target → sender | 1 / non | `body` ou `error`, même CorrelationData ; ACK métier après persistance, distinct du PUBACK MQTT |
| Rzone/{z}/node/{n}/state | Edge n → zone(s) autorisée(s) | 1 / oui / node_ttl_seconds | Snapshot net : node_id, zone_id, state, available_for_project_bytes, generation, snapshot_version, updated_at, heartbeat_at, included_reservation_ids |
| Rzone/{z}/node/{n}/heartbeat | Edge n → propriétaire z | 0 / non / node_ttl_seconds | Snapshot net et liveness fraîche ; un état retained ne rafraîchit pas l'heure de heartbeat reçue par la zone |
| Rzone/{z}/summary | Zone z → voisines configurées | 1 / oui / node_ttl_seconds | Summary agrégée seulement : zone_id, generation, updated_at, available_for_project_bytes, state ; coalescence 250 ms, publication périodique 1 s |
| Rpresence/{actor} | Testament acteur | 1 / oui | UNAVAILABLE ; informatif, l'éligibilité dépend aussi du heartbeat frais |
| edge/{id}/… | Agent Go legacy | protocole hérité | Conservé dans main, **non connecté** à MQTT5 ; MQTT_BROKER non fourni au backend agent dans ce Compose |

Connexion/reconnexion : MQTT5 autopaho existant, keepalive 10 s, session 60 s, resubscription. RPC borné 12 s par l'adaptateur ; requête expirée, retained ou non corrélée rejetée. Temps horloge/NTP requis. OFFER/RESERVE interzones gardent le timeout du coordinateur 2 s ; fanout initial 2. Les variables d'état et compteurs sont locaux ; les summaries ne transportent aucun registre détaillé étranger.

## Opérations métier réellement implémentées

| Cible / opération | Autorité admise | Corps / résultat | Effet durable et limite |
|---|---|---|---|
| Central PING | Compte connecté | `{}` → ready | Disponibilité du handler, API /ready vérifie aussi le bus |
| Central REGISTER_NODE | edge-{node_id} exact | Registered : Node + endpoint | Provisionnement zone/quota/endpoint et génération ; événement NODE SQLite lors de nouvelle génération |
| Central NODE | Contrôleurs ou Edge lui-même | node_id → Registered | Lookup de propriétaire pour admission de zone |
| Central LOOKUP | Contrôleurs | request_id → UploadRecord ou null | Réconciliation requête immuable, lecture du catalogue |
| Central PLAN | Zone actuelle du source_node_id | Upload + DistributedPlan + expires_at | Catalogue existant valide les limites du manifeste et enregistre PLAN transactionnel ; outbox avant commit |
| Central AUTHORIZE_STORE | Edge destination exact | video_id, reservation_id, node_id → unités et bail | Unités/hashes attendus dérivés du seul plan Go ; pas d'allocation supplémentaire |
| Central PREPARED | Zone du reservation_id | video_id, reservation_id | TRANSFERRING dans métadonnées ; **aucun STORED/READY** |
| Central STORE_COMPLETE | Edge affecté exact | video_id, extent_id, node_id, reservation_id, size_bytes, sha256, proof | Tuple immuable contrôlé ; proof doit correspondre au mode provisionné ; événement déterministe et transaction idempotente ; READY si toutes unités STORED |
| Central CANCEL | Zone source actuelle, central/admin | video_id | Catalogue CANCELED et vue FAILED ; une confirmation tardive est refusée |
| Central SNAPSHOT | Contrôleurs | `{}` → catalogue, nœuds, uploads | Vue consultable ; jointure node→zone à chaque lookup |
| Zone REGISTER | Edge exact autorisé dans cette zone | Registered | Vérifie propriétaire Central, admission initiale UNAVAILABLE avant heartbeat ; cache dans registre de Dan |
| Zone OFFER | Contrôleurs | ReservationRequest → offre partielle et remaining | Aucun débit ; compteurs observables via STATS |
| Zone RESERVE | Contrôleurs | Même contrat → réservation | Ledger original persistant avant réponse ; idempotence, expiration, rollback |
| Zone RELEASE | Contrôleurs | reservation_id | Relais release des admissions Edge puis libération du ledger ; données déjà STORED jamais libérées implicitement |
| Zone COMMIT | Contrôleurs | reservation_id | Central réconcilie périodiquement les réservations des vidéos READY, sans double débit |
| Zone PREPARE | Contrôleurs | video_id, reservation_id | PREPARE_STORE à chaque Edge de la réservation locale, puis PREPARED Central |
| Zone UPLOAD | central/admin | Upload (manifest + target_size_bytes + contents[{extent_id,sha256}]) | Appelle le coordinateur Go existant, publie PLAN puis prépare chaque zone ; annule et libère après erreur de préparation |
| Zone DEPART | Edge exact | node_id, zone_id, generation | Quarantaine à génération supérieure ; destination doit ensuite enregistrer et envoyer un heartbeat frais |
| Zone STATS | Contrôleurs | `{}` → summary, voisins, nœuds locaux, offers, summary_publications | Diagnostic authentifié ; aucune décision dans le client |
| Edge PREPARE_STORE | Zone propriétaire actuelle | video_id, reservation_id, node_id | Autorisation Central + vérification quota net + ledger durable ; `stored=false` |
| Edge RELEASE | Zone actuelle ou central | reservation_id | Annule uniquement RESERVED ; ne libère jamais les bytes STORED |
| Edge MOVE | central | node_id, zone_id, génération suivante ou 0 | Destination dans allowed_zones, DEPART ancien propriétaire, ledger, Central puis REGISTER nouvelle zone ; extent garde son node_id |
| Edge STATUS | Contrôleurs | `{}` → mode, node, quota_bytes, units | Diagnostic du quota durable |
| Edge CONTROL / RETAINED_ONLY | central + ENABLE_TEST_CONTROLS=1 | état/pause ou `{}` | Injection de conditions pour le smoke, désactivée dans compose.edge.yaml |
| Edge REPLAY_COMPLETE | central + ENABLE_TEST_CONTROLS=1 | video_id | Republie via MQTT une confirmation déjà STORED ; revérifie les bytes en real-http ; teste processed_events unique |
| Edge MOCK_STORE | central + mode mock | video_id | Confirmation explicitement MOCK, pas de fichiers ; refusée en real-http |

Les commandes contrôleurs sont des contrats de laboratoire opérationnels et authentifiés, pas des contrats déjà acceptés par SIS/vidéo. Le bridge suppose l'Edge provisionné honnête et conserve ses secrets privés ; ce n'est pas une attestation contre un hôte Edge compromis.

## HTTP effectif et contrats futurs

| Interface effective | Authentification / retour | Interface souhaitée non livrée / impact |
|---|---|---|
| GET /health et /ready (Central, bridge, zone :8088) | Public, liveness / readiness 200 ou 503 | Pas de métriques Prometheus ni supervision de production |
| GET /api/v1/snapshot | Bearer opérateur | API de consultation stable à versionner avec SIS |
| GET /api/v1/videos/{id}/placements | Bearer opérateur ; tailles, hashes, node, zone, endpoint, états et preuve | Aucun manifeste HLS, autorisation multi-utilisateur ou signed URL |
| POST /api/v1/uploads | Bearer opérateur ; 201 plan, 409 refus | Uploader vidéo devra construire les manifests et hash de chaque extent exact ; aucun round_robin de secours |
| POST /api/v1/rpc/{target}/{op} | Bearer opérateur ; relayé comme central | Contrôle de démonstration large ; à remplacer par API administrative fine avant multi-utilisateur |
| PUT /store/{video}/{extent} (bridge) | Bearer propre au nœud, Content-Length exact ; 201 après vraie vérification, 200 répétition | Pas de reprise partielle ni de streaming ; unité complète avant expiration du bail |
| GET /store/{video}/{extent} | Bearer propre au nœud ; octets depuis le vrai agent | Future autorisation lecture SIS par consommateur à définir |
| PUT /v1/verified/{tenant}/{object} (agent backend privé) | Token fichier configuré + X-Content-SHA256 ; taille/hash/fsync/rename | Extension isolée ; backend non publié sur le LAN |
| /v1/storage/* legacy de main | inchangé si aucun token ; protégé si HTTP_AUTH_TOKEN_FILE défini | Ne pas annoncer son MQTT hérité comme compatible MQTT5 |
| RTP / RTX / WebRTC / SIS | **NOT_IMPLEMENTED dans ce chantier** | Aucun port média ni réécriture des travaux vidéo ; établir interfaces et ownership avec l'équipe |

États exposés : le plan interne passe par PLANNED (offre), RESERVED puis COMMITTED/RELEASED/EXPIRED. Les unités de la vue Central passent RESERVED → TRANSFERRING → STORED ; la vidéo est READY uniquement après les confirmations attendues du mode actif, FAILED en cas d'annulation/expiration. Le catalogue historique garde RESERVED/READY/CANCELED ; les états de transport supplémentaires sont des métadonnées, pas un second catalogue autoritaire.

## Priorités après cette livraison

| Priorité | Travail restant | Condition / périmètre |
|---|---|---|
| P0 demain | Vérifier LAN/pare-feu/NTP, charger ARM64, enregistrer chaque vraie machine, faire PUT/GET + hash et redémarrage physique | **NOT_TESTED** sans matériel ni IP réelles ; aucun résultat physique inventé |
| P0 VOD | Définir propriétaire du manifeste, SHA de chaque extent, upload/retry de segments réels et droits de lecture avec vidéo/SIS | Le client de démonstration prouve le contrôle et stockage, pas une VOD intégrée |
| P1 exploitation | TLS/VPN, rotation/révocation de credentials, API admin plus fine, logs/métriques/quotas et limites de débit | Avant utilisation hors réseau de laboratoire isolé |
| P1 durabilité | Essais coupure électrique/disque plein réels, réparation des orphelins, GC transactionnel, sauvegarde cohérente DB+metadata+files, migration schéma | Injection HTTP d'erreurs testée ; pas de qualification stockage physique ARM |
| P1 capacité | Réconciliation après changement de quota/identité, grandes topologies, limites mémoire et compaction des ledgers (10 000 réservations, metadata 32 MiB) | Admission ferme en erreur ; pas de libération optimiste de bytes stockés |
| P1 mobilité | Bascule réseau réelle, reprise d'un MOVE interrompu entre deux étapes, relocalisation média si IP change | Génération/zone persistantes et join testés ; changement d'IP nécessite reprovisionnement ; pas d'autodécouverte réseau |
| P2 VOD | Lecture HLS et intégration effective du pipeline vidéo, performances/temps d'estimation réseau | Aucun allocateur Python concurrent |
| P2 live | Cache/live, réplication et adaptation dynamique | Travaux collègues à conserver séparés |
| P2 SIS/RTP | Contrats SIS, signaling, RTP/RTX/WebRTC, NAT, synchronisation et QoS média | Non implémentés ici ; accélération Jetson hors scope |
