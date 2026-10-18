# Audit contrôleurs — 18 octobre 2026

Base exacte PR #1 : d8eea1b8ea5f5f05595da55ccaadd1b4b8ba42e4, draft ouverte,
non mergée, CI existante et placement en succès. Checkout initial propre.
main : bc31c7066bb3e467d2005f21458109c93a15800a ; containers :
3a56360cd3eb1722dd800dccddf09247c4f3378f ; video_streaming :
3e85d3f7541afa95c44f71b4d6d6cb836fcecfe4.

Le diff main pertinent ajoute MQTT_USERNAME/PASSWORD au vrai Edge Agent et à
son client legacy : deux fichiers repris exactement avant adaptation. Aucun
merge de branches, aucune modification de main ou de la PR #1.
La branche vidéo reste séparée : imports placement.py absent, stratégie
round_robin dans sendBlocks/live_watcher, SIS et live non raccordés au Go.
Aucun fichier de cette branche n'est importé ou réécrit.

Réutilisés sans réécriture : pkg/placement, catalogue SQLite, mqttbus MQTT5,
ledger de réservations et entrée serveur-zone/app/allocator.go. Le banc entier
lab/placement est conservé sans modification. Son réseau interne, identités
fixes et STORE_COMPLETE mocks ne sont pas qualifiés de mode réel.

Structure opérationnelle ajoutée : pkg/controllers (adaptateurs configurables,
Central et couche métier de zone) ; internal/edgecontrol (bridge explicite
MQTT5 vers vrai HTTP Edge) ; deploy/lab (Compose, configuration/provisionnement,
scripts Windows/Linux, réseau explicite) ; endpoints vérifiés et authentifiés
opt-in dans le vrai Edge. Le catalogue d'extents global reste SQLite au Central,
zone.db reste le registre de Dan. Métadonnées de transport/outbox durables dans
le volume Central, sans seconde autorité de placement.

Docker Desktop était arrêté ; démarré en arrière-plan. Go portable disponible
sous work/toolchain (1.27.2). Les exécutables de tests Windows peuvent être
bloqués par Application Control : suites Linux Docker prévues dans trois
modules indépendants. Matériel ARM physique absent : NOT_TESTED.
