# Livraison CaribouTV intégrée — 19 octobre 2026

PR distincte **[draft #4](https://github.com/julesdavidou/projet_secu_cloud/pull/4)**, sans merge. Branche : `codex/integration-cariboutv-v1`, base `codex/placement-edge-v1` après fusion préexistante de PR #2. Les trois merges locaux compatibles réunissent main, auth et video_streaming. Aucun merge de PR ni réécriture des branches des collègues. Les changements de collègues incompatibles restent accessibles et nommés dans l'[audit](AUDIT-2026-10-19.md).

## Prêt à tester maintenant

Les dix services du nouveau banc PC sont démarrés et sains. L'ancien projet `caribou-controllers` est resté actif, avec ses huit services et ses propres volumes. Le banc ARM64 de vérification a été arrêté après succès, en conservant ses données. Guide complet : [deploy/integration/README.md](../../deploy/integration/README.md).

```powershell
.\deploy\integration\start.ps1 -SkipBuild
.\deploy\integration\smoke.ps1
.\deploy\integration\diagnostics.ps1
```

Ouvrir **http://localhost:18090/integration/** ; se connecter/créer un compte de test ; publier un petit MP4 ou utiliser la vidéo partagée « Démo intégrée – fichiers Edge réels » ; ouvrir le frontend spectateur. La session Java est réellement utilisée par le catalogue et le lecteur.

## Preuves exécutées

| Vérification | Résultat et portée | Preuve |
|---|---|---|
| Go : racine, serveur-zone, lab/placement | PASS tests, race detector et vet Linux ; scheduler inchangé | [log](evidence/go-checks.txt) |
| Frontend existant | PASS 9 tests Node, aucun changement des fichiers dist | [log](evidence/frontend-checks.txt) |
| Intégration AMD64 | PASS authentification Java, isolation privée, partage entre comptes, profils/favoris/historique, rejet faux MP4, HLS réel, placement Go, stockage et lecture Edge SHA-256, décodage audio/vidéo, Range206 | [log](evidence/smoke-amd64.txt) |
| Intégration ARM64 | PASS même parcours sous émulation, dix services ARM64 ; pas du matériel physique | [log](evidence/smoke-arm64-emulated.txt) |
| Contrôleurs interzone | PASS 2 extents réels de 6 Mio, source locale puis zone B, plan idempotent, PREPARE non READY, relecture HTTP SHA-256 des deux zones | [log](evidence/interzone.txt) |
| Disque/intégrité/persistance | PASS SHA-256 directement dans les volumes Edge ; octet de mire altéré → playback502/cache409 ; octets restaurés exactement ; redémarrages → credentials/ID/profils/catalogue/fichiers/lecture conservés | [log](evidence/resilience.txt) |
| Navigateur | PASS login depuis console, catalogue existant, reprise à 2 secondes et lecture dans le lecteur natif | [capture](evidence/frontend-playback.png) |
| Sources Dan récentes | PASS compilation et vet du module isolé ; aucun test fonctionnel fourni, non activé dans le banc | [log](evidence/legacy-dan-build.txt) |
| Python préservé | PASS syntaxe de 15 fichiers ; ce contrôle ne valide pas SIS/IBS/live ni les anciennes passerelles | [log](evidence/python-syntax.txt) |
| Images | Trois images contenant AMD64 + ARM64, chargées localement, aucune publication registre | [web](evidence/image-web.txt), [control](evidence/image-control.txt), [auth](evidence/image-auth.txt) |

[Comparaison Git des composants préservés](evidence/preservation.json) : scheduler/contrôleurs, frontend dist et métier auth inchangés. Les vérifications GitHub supplémentaires sont consultables dans la PR ; les preuves locales ci-dessus ont été exécutées indépendamment.

Les tests unitaires `internal/webapp` utilisent des doubles HTTP pour les cas de refus/simulation et de métadonnées corrompues. Les tests de bout en bout ci-dessus utilisent de vrais services Java/Go/MQTT/SQLite/Edge et de vrais octets vidéo. Seule leur mire d'entrée est synthétique. Aucun mock n'est accepté comme preuve READY dans l'API spectateur.

Limite de l'environnement Windows : lancement direct du binaire de test Go refusé par Application Control ; les mêmes tests, `-race` et vet ont réellement été exécutés avec succès dans Linux/Docker. Le premier build Docker a échoué sur le DNS/proxy ; le build avec proxy Docker Desktop a réussi. Le premier upload a révélé le chemin init HLS doublé du script existant ; correction minimale et réexécution réussie. Ces erreurs n'ont pas été présentées comme des succès.

## Reste à faire pour terminer le projet

1. **Labo réel** : vraies IP/quotas stables, exports d'identités par Edge, ports/routage/pare-feu, exécution sur Raspberry Pi/Jetson et preuve du même parcours. L'overlay et les commandes sont prêts ; l'accès au matériel/réseau manque.
2. **SIS/IBS** : raccorder les signatures/vérifications existantes et leurs métadonnées aux vrais segments stockés, tester altération/perte/réordonnancement. Le hash SHA-256 actuel ne remplace pas SIS.
3. **Vidéo/live** : décider le raccord des passerelles Python dépendant d'un `placement.py` absent ; remplacer leurs décisions round-robin par les plans Go ; HLS adaptatif/direct Edge, live/RTP et performances ne sont pas livrés.
4. **Fiabilité/exploitation** : reprise d'upload, suppression/GC coordonnée, réplication et récupération d'Edge perdu ; TLS/VPN, rôles, durcissement auth et limitation des tentatives pour un accès public.
5. **Unification** : décider la migration du registre/topics et de l'allocateur alternatif de Dan, sans remplacer le scheduler ; définir la reprise des vidéos anciennes sans propriétaire/mapping web. Compteurs de vues et miniatures restent basiques.

La base pour avancer ensemble est cette branche intégrée, avec le [guide de lancement](../../deploy/integration/README.md), l'[audit des branches](AUDIT-2026-10-19.md) et ces preuves. Les données existantes n'ont pas été supprimées ; les fichiers synthétiques des tests restent dans les volumes de leurs bancs respectifs.
