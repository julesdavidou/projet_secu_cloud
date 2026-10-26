# CaribouTV — stockage distribué et diffusion vidéo sécurisée

Ce dépôt rassemble le travail du groupe : authentification Java, interface web, catalogue Central SQLite, contrôleurs de zone, scheduler Go, agents Edge et chaîne vidéo avec audio AAC.

**Pour installer, présenter ou évaluer le projet, commencer par le [guide de livraison](docs/livraison/README.md).** Il décrit la version intégrée, les commandes exactes, les données persistantes et les limites observées.

## Ce qui fonctionne et ce que cela signifie

- Une vidéo est découpée, signée par l'agent de son propriétaire, placée par Go, stockée réellement sur les Edge puis reconstruite avec vérification.
- Un direct entre par un fichier ou OBS en RTMP/TCP. Il suit le même parcours sécurisé sous forme de fragments HLS avec vidéo H.264 et audio AAC.
- Les rôles contrôleur, stockage, publication et lecture sont distincts. Un PC peut en cumuler plusieurs.
- Le mot « distribué » signifie que les blocs sont répartis entre des Edge. Cette livraison n'ajoute pas de réplication automatique : un Edge détenant un bloc doit rester disponible pour sa lecture.
- Les mesures disponibles ne constituent pas une mesure optique caméra-écran. La faible latence et la conformité complète à l'article ne sont pas déclarées acquises.

## Choisir le bon déploiement

| Besoin | Point d'entrée | Usage |
|---|---|---|
| Version sécurisée intégrée, contrôleurs sur un PC | `deploy/secure-v3/start.ps1` puis `deploy/fleet/resume-controllers.ps1` | Profil principal de la livraison ; frontend local sur 19690 |
| Participant Windows avec ses rôles autorisés | Un bundle privé exporté par `deploy/fleet/prepare-client.ps1` | Dans le bundle : `start.ps1`, `publish.ps1`, frontend sur 18790 |
| Banc de validation live indépendant | `deploy/live-v4/start.ps1` | Autres ports et volumes ; lecteur sur 20690 ; utilisé en CI |
| Déploiement Fleet de Jules sur SSH/GHCR | `deploy/fleet/deploy.ps1` | Profil HTTP historique, frontend sur 18090 ; ne pas le confondre avec le profil mTLS |
| Agent Edge historique seul | `compose.yaml` à la racine | Préservé ; [documentation historique](docs/livraison/ANCIEN-README-EDGE.md) |

**Ne pas lancer tous les bancs pour une même démonstration.** Les ports 18090, 19690 et 20690 désignent des déploiements différents.

## Lire la documentation

1. [Vue d'ensemble et parcours de démonstration](docs/livraison/README.md).
2. [Installation, comptes et raccord des machines](docs/livraison/DEMARRAGE.md).
3. [Architecture, stockage, MQTT et sécurité](docs/livraison/ARCHITECTURE.md).
4. [Live, son, relecture et mesures](docs/livraison/VIDEO-ET-MESURES.md).
5. [Dépannage Windows, Docker et réseau](docs/livraison/DEPANNAGE.md).
6. [Tests, preuves de la démo et limites](docs/livraison/VALIDATION.md).

Les documents des étapes précédentes restent disponibles dans `docs/placement`, `docs/integration`, `docs/edge-v2`, `docs/conformity-v3` et `docs/live-v4`. Les décisions proposées ou états de validation anciens n'y sont pas tous des garanties de la version finale : le guide de livraison précise la portée actuelle.

## Repères dans le code

| Dossier | Responsabilité |
|---|---|
| `cmd`, `pkg/controllers`, `internal/edgecontrol` | Exécutables Go, contrôle, catalogue et passerelle Edge |
| `pkg/placement`, `lab/placement`, `serveur-zone/app` | Scheduler et intégration avec le contrôleur de zone existant |
| `auth` | Comptes, sessions et identifiants stables Java |
| `frontend/dist` | Interface et lecteur live |
| `videostreaming`, `integrations/video` | Découpage, signatures SIS/IBS, publication et reconstruction |
| `deploy/fleet` | Déploiement SSH/GHCR existant et bundles Windows sécurisés |
| `deploy/secure-v3`, `deploy/live-v4` | Compose, autorités, stockage isolé et bancs de validation |
| `.github/workflows` | Compilation, tests, intégrations et images AMD64/ARM64 |

## Données privées

Git contient le code, les configurations d'exemple et des preuves expurgées. Il ne contient ni les clés de la démo, ni ses mots de passe, ni ses volumes Docker, ni les bundles privés. **Un clone de main ne restaure pas les vidéos et identités d'une installation existante.** Voir la procédure de sauvegarde avant de changer de machine.
