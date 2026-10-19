# Livraison — MQTT Edge et reconstruction, 19 octobre 2026

## Périmètre et historique préservés

Base exacte de PR4 : `40eeeae8e77e58a75fae0ad84b64baf6cb23d77d`. Au premier audit GitHub, PR4 et PR1 avaient déjà été fusionnées par un humain ; `main` = `885e6ad545742928b0bb5f625b97acfb5acbba08`. La nouvelle branche `codex/edge-mqtt-reconstruction-v2` part de PR4, avance sur ce main puis intègre les commits collègues par de vrais merges conservant l’historique : MQTT `82e140e` et client vidéo `e7ca637`. Aucun merge de PR exécuté par cet agent.

Les fichiers du scheduler `pkg/placement`, l’allocator de Dan, le frontend et le service Java d’authentification ne sont pas remplacés. Le placement et l’état READY restent décidés par le système Go Zone/Central SQLite. Le nouveau profil gère stockage HTTP et contrôle dans le véritable `edge-agent` Go, et réutilise l’assembleur Python du collègue dans un worker sur l’Edge. Le backend web autorise la demande, confie la reconstruction à l’Edge et contrôle la réponse.

## Corrections compatibles réalisées

- Compilation du client MQTT : raccord au monitor existant ; fonctions manquantes résolues. Gardes HTTP et PUT verified restaurés lors du conflit de merge.
- Politique de transport du collègue partagée entre client standalone et bus MQTT3. Le mode contrôlé utilise une seule identité et un seul ledger. Le standalone conserve ses topics historiques, mais ne tourne pas en parallèle du bus de placement.
- MQTT5 existant conservé ; MQTT3.1.1 transporte la corrélation et la destination de réponse dans une enveloppe JSON versionnable. Même ACL, commande, délai, identité et ACK métier. Compatibilité MQTT5 antérieure conservée sans forcer les nouveaux champs aux anciens pairs.
- Trois brokers réels : hub et broker de chaque Edge, bridges directionnels, ACL propres à chaque identité. Évite un deuxième orchestrateur ; une publication MQTT réussie ne vaut jamais validation d’un stockage.
- Blocs ordonnés explicitement, tailles et hash attendus, doublons/manques/contexte incohérent refusés. Source originale conservée pour permettre une comparaison exacte. Le découpage HLS et les calculs SIS/IBS du collègue restent réutilisés.
- Capabilités GET limitées à un objet, un nœud, une vidéo, un hash et une courte échéance. Un worker ne reçoit aucun token opérateur ni token d’écriture des autres Edge.
- Réservations des uploads annulés ou expirés libérées par réconciliation avec réessai ; les octets déjà STORED ne sont ni supprimés ni rendus artificiellement disponibles.
- Sessions, droits privés/partagés, profils, catalogue et ancien format V1 préservés. Suppression directe d’un objet géré refusée en l’absence de coordination de GC.

## Preuves exécutées localement

Les extraits sont dans [preuves/](preuves/). Les journaux complets sont dans le dossier de travail `outputs/mqtt-reconstruction-2026-10-19/`, en dehors des secrets du dépôt. Les tests sont rejouables avec [DEMARRAGE.md](DEMARRAGE.md).

| Contrôle | Résultat / nature |
|---|---|
| Go racine, serveur-zone et lab/placement | compilation, tests, race et vet réussis dans Docker AMD64 |
| Python | 6 tests réussis : tailles variables, ordre, manque/doublon/corruption, contexte du prototype SIS/IBS |
| Frontend existant | 9 tests réussis, fichiers du frontend inchangés |
| Nouveau parcours réel | login Java, API vidéo, stockage HTTP réel, SQLite READY, source originale exacte, décodage vidéo+audio, Range206, droits privés/partagés et isolation |
| Deux zones et brokers distincts | vrais fichiers répartis A1/B1 ; A1 MQTT3.1.1, B1 MQTT5 ; commandes/ACK et plan Go idempotent |
| Mobilité | changement logique de zone A1 A→B→A ; identités/objets/hashes conservés ; reconstruction exacte après déplacement |
| Corruption / absence | octets sur disque réellement altérés ou fichier temporairement déplacé ; HTTP502, cache lecture invalidé ; restauration exacte dans finally |
| Accès et ACL | tickets GET valides et refus anonyme/expiration/mauvais objet/écriture ; publication MQTT Edge sous identité admin refusée avec reason 135 |
| Reconnexion | arrêt broker A ; absence d’ACK métier fictif ; reprise abonnements/annonces et quota inchangé |
| Redémarrage | Central, Zones, Edge, workers, auth, web ; SQLite/fichiers/identité persistants, ancienne session révoquée |
| Ancien parcours PR4 | smoke antérieur entièrement réussi sur les nouveaux binaires, dans un projet Docker isolé avec pipeline V1 |
| Images multiarch | les quatre images control/reader/web/auth construites AMD64 et ARM64 |
| ARM64 sous émulation | 12 services démarrés et smoke réel réussi sous QEMU ; aucun matériel physique utilisé |

Exemple multi-zone : vidéo `v-a3c89cf95ca5da0cfd77c4f82d7f846c`, source 1 484 041 octets, SHA256 `75c032c12c15d4493dee703835f936d1c3826c8332defada937ce4f3e61e777a`, 16 unités réellement stockées A1/B1. Les réservations temporaires du test réseau ne stockent aucun fichier et ne déclarent aucun READY ; elles sont annulées puis leur libération est vérifiée.

Les tests Python de transport utilisent `httpx.MockTransport` : ils testent le contrat, pas le réseau. Les scripts Compose utilisent réellement Java, Go, SQLite, Mosquitto, FFmpeg, Python et les volumes Edge. Les vidéos sont des mires synthétiques ; leurs fichiers et leur décodage sont réels. La mobilité testée est logique, sans déplacement Wi-Fi physique.

## Écart au sujet et à l’article — reste à terminer

Le sujet `Projet_2024_2025.pdf` et l’article Sellami/Imine/Gallais demandent davantage qu’un hash SHA256. Le raccord du prototype SIS/IBS est préparé : contexte complet propriétaire/vidéo/asset/blocs signé, sérialisation canonique, ancre indépendante exigée, refus en cas de clé ou contexte incohérent. Les tests signent et vérifient réellement avec le code du collègue. **Le parcours VOD livré reste explicitement SHA256 ; les champs SIS/IBS ne sont pas encore produits par la publication web.**

À terminer pour une conformité au protocole de l’article : Identification Servers de confiance par zone, partage secret sécurisé et accumulation des clés lors de la mobilité des propriétaires, validation des paramètres SIS et de leur générateur, fourniture d’une ancre de confiance indépendante, délégation/mises à jour authentifiées et intégration publication/lecture SIS de bout en bout. Le prototype génère encore ses propres clés de zone ; ses paramètres et son PRNG ne sont pas certifiés. SHA256 n’est pas SIS. Les fichiers SISIntegrity.py et IBSSignature.py du collègue ne sont pas réécrits ici.

Autres écarts explicites : autodécouverte réseau et vraie mobilité des machines non réalisées ; la fédération actuelle est un hub avec brokers Edge, pas un maillage pair-à-pair autonome ; les zones/identités/endpoints sont provisionnés. L’isolation applicative des utilisateurs est vérifiée, mais les données gérées utilisent encore le tenant `project` dans le volume de chaque Edge, pas un espace Docker séparé par client. Réplication/réparation des données perdues, GC coordonné, live/RTP et mécanismes de QoS/prix restent hors de cette intégration. Aucun changement sur ces points n’est présenté comme opérationnel.

Accès indispensables au test physique : vraies IP LAN, accès aux cartes et architecture OS ARM64, Docker disponible et connectivité HTTP/MQTT entre PC et cartes. Aucun accès matériel n’a été inventé.

## Publication et CI

Au dernier essai, `git fetch` et HTTPS vers GitHub restent indisponibles depuis le processus de travail (`Could not resolve host`, puis connexion TCP refusée avec l’adresse DNS résolue). L’accès réseau a été demandé et accordé ; le connecteur GitHub répond 404 sur ce dépôt privé. Il n’y a donc pas de PR nouvelle ni de CI distante à revendiquer tant que la publication n’a pas abouti. Les tests locaux ci-dessus sont indépendants de ce blocage.

Le texte de PR est prêt dans [PR-DRAFT.md](PR-DRAFT.md). Une fois l’accès rétabli, depuis la branche dédiée et un arbre propre :

```powershell
./deploy/edge-v2/publish-draft.ps1
```

Cette commande pousse uniquement `codex/edge-mqtt-reconstruction-v2`, réutilise une PR ouverte de cette branche si elle existe, sinon crée une **draft vers main**, et n’effectue aucun merge. La CI ajoutée s’exécute alors sur le SHA publié ; il faut vérifier ses conclusions avant de fusionner. Les limites d’essai sont 512 unités par publication, 64 MiB par asset, blocs de 125000 octets par défaut : utiliser de courtes vidéos de démonstration dans les quotas configurés.
