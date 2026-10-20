# Matrice des contrats avant implémentation — 19/10/2026

Référence : PR #4 HEAD 40eeeae, désormais fusionnée par un humain dans main 885e6ad. Nouvelle branche dédiée issue de ce HEAD puis avancée sur main. Aucun merge de PR par cet agent.

| Domaine | Référence opérationnelle PR4 | Nouveaux travaux | Conflit / correction prévue |
|---|---|---|---|
| HTTP Edge | Bearer, PUT verified, SHA256, fsync | main MQTT ancien point de départ sans ces protections | Résolution de merge garde storageGuard + registerVerifiedRoutes ; tests de refus obligatoires |
| MQTT | MQTT5 lab/placement/v1, RPC corrélés, ACK métier | 82e140e MQTT3.1.1 zone/* et node/* | Adapter transport 3/5 au même protocole autoritaire ; conserver le client standalone sans le lancer en concurrence |
| Compilation MQTT | root Go fonctionnel | collectMetrics/getLocalIP absents du package mqttclient | Relier au monitor existant et configurer les annonces ; reproduire puis corriger compilation |
| Capacité | quota projet net depuis ledger durable | storage_max/used disque physique | Télémétrie standalone seulement ; jamais source de capacité de placement |
| Commandes | PREPARE, STORE_COMPLETE, MOVE, STATUS via ACL | REQUEST_STATUS/REFRESH_STATE, DELETE_OBJECT | Alias de statut normalisés ; suppression de données gérées refusée sans coordination du catalogue |
| Reconnexion | registre + heartbeat périodiques et versions durables | abonnement reconnecté, annonce initiale hors callback | Re-publication et réabonnement bornés ; LWT/TTL ; pas de double identité MQTT |
| Brokers | un broker local | sujet impose plusieurs Edge brokers | Profil multibroker hub + brokers Edge avec routes directionnelles et ACL par identité |
| Reconstruction | gateway remux HLS -> MP4 | e7ca637 agent Python client SIS + cache | Réutiliser son assemblage sur un worker Edge ; manifest Go versionné, pas de placement.py concurrent |
| Confiance | catalogue SQLite + SHA256 attendu | placement.json fourni par stockage + pin facultatif | Manifest du backend authentifié, tickets de lecture limités, clés indépendamment épinglées ; fail closed |
| Blocs | segments de tailles variables, un extent/segment | blocs paddés 125000 ; reconstruction permissive | Version explicite, indices/identités/tailles/digests, manque/doublon/corruption refusés, ordre rétabli |
| Identité source | réencodage HLS et remux, non bit-identique au MP4 source | assemblage des segments existants | Stocker aussi la source en blocs ; cmp et SHA256 source/résultat obligatoires |
| SIS/IBS | non intégré, ne pas confondre avec SHA256 | signer propriétaire + clé de zone générée localement | Conserver calculs, préparer contexte complet/ancre de confiance ; ne pas certifier conformité à l'article sans Identification Servers |
| Utilisateurs | cookie Java, droits privés/partage, profils | agent client sans auth HTTP Edge / imports cassés placement.py | Adaptation à la session côté API et capacités lecture ; aucun secret opérateur dans frontend |

Sources sujet : Projet_2024_2025.pdf (3 pages), article Sellami/Imine/Gallais (14 pages), sections 3.2–3.5 en particulier. Exigences non satisfaites par un seul hash : serveurs d'identification fiables, partage de secret et PK de zones, SIS/IBS, mobilité d'identité, délégation et mises à jour authentiques. Matériel physique et auto-configuration LAN encore NON TESTÉS faute d'accès/IP.

Tests prévus : contrats unitaires Go/Python, comparaison SHA256/cmp source, services réels Docker avec brokers séparés, MQTT3/5, reconnections/ACK/pertes, multizone, mobilité, redémarrages, droits utilisateur, CPU AMD64, ARM64 sous émulation distinct du matériel ; CI sur SHA final.

Implémentation et résultats : [LIVRAISON.md](LIVRAISON.md). Commandes et paramètres : [DEMARRAGE.md](DEMARRAGE.md). Les adaptations du tableau sont réalisées ; les écarts au sujet restent explicitement listés dans la livraison.
