# Décisions bornées à valider avec l'équipe

| Question | Contrat testé ici | Travail restant / propriétaire à confirmer |
|---|---|---|
| Manifest variable et gros manifests | segment_id entier zéro-indexé, tailles positives, total signé 64 bits, 1 MiB MQTT | schéma commun avec découpage vidéo ; resolver HTTP hors MQTT avec auth/limites |
| Cible/limite extent | cible par requête, segment indivisible ; dépassement permis si segment unique | cible en MiB réelle et éventuelle taille maximale avec équipe vidéo |
| Plan consommé par le transport | extent_id, first/last_segment, size, node/zone, reservation_id ; ranges adjacents | locator média, reprise de transfert, checksums et contrat d'extent stocké atomiquement |
| Capacité projet | net des réservations incluses, liste included_reservation_ids | implémentation côté Edge réel ; aucune conversion implicite de Storage.Free ni StorageMax-StorageUsed |
| Autorité de mobilité | mock incrémente generation, source DEPART puis Central puis destination | autorité réelle, handover interrompu, fencing, transferts en vol et départ/arrivée coordonnés avec Dan |
| Transport réel | STORE_COMPLETE mock seulement | RTP+RTCP/NACK/RTX ne garantit pas seul un commit stockage ; protocole métier d'extent complet à définir |
| Brokers / bridging | un broker de lab isolé, MQTT5 | exigence académique multibrokers Edge et bridges, ACL et topics avec collègues ; aucun bridging prétendu |
| Hébergement et modules | Central de référence temporaire + SQLite ; serveur-zone de Dan conservé | hôte du vrai Central, API web, JPBC/SIS, signaling WebRTC et responsables |
| READY / crash / annulation | tous les STORE_COMPLETE committés, retry exact, LOOKUP, leases durables, refus des completions annulées/expirées | nettoyage d'extents partiellement réellement transférés, réconciliation continue et durabilité Edge |
| Legacy cariboutv/commands | topic conservé mais callbacks de mutation refusés ; mode désactivé | schéma Command complet, request/reply corrélé, identité/autorisation avec Dan ; pas de faux succès intégré |
| Évolutions de containers | copie additive du commit 3a56360, allocator raccordé ; branches sans ancêtre commun | stratégie commune de branches/PR avec équipe ; pas de merge main automatique |
| Live | interface LivePolicy seulement ; VOD toujours finie | ingress/relay, politique/cache live partagé, profiling viewer, buffers, cache RTX et security epochs SIS avec autres modules |
| Générations / rétention | compteur snapshot monotone, époque de résumé, 10 000 tombstones bornés | génération durable et protocole de compaction convenus ; aucun effacement automatique risquant de réactiver un vieux RESERVE |

Ces points ne bloquent pas le noyau ni le banc de simulation. Ils empêchent de
présenter le laboratoire comme un déploiement de stockage/streaming complet ou
de raccorder automatiquement les vrais services de l'équipe.
