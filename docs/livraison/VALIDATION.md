# Validation, preuves et limites

## Règle de lecture

Un conteneur démarré n'est pas forcément prêt. Un port ouvert n'est pas une authentification réussie. Un ACK MQTT n'est pas une preuve de stockage. Un catalogue `READY` vérifié à l'écriture n'est pas une relecture exhaustive de tous les fichiers à une date ultérieure.

Ce document sépare les observations du laboratoire, les tests réexécutés et les tests de CI. Pour l'état exact de la CI, consulter le **SHA rendu**, pas le dernier badge d'une autre branche :

```powershell
git rev-parse HEAD
git log -1 --oneline
```

Les workflows sont visibles dans [GitHub Actions](https://github.com/julesdavidou/projet_secu_cloud/actions). Les images sont publiées avec des références de commit lorsque leurs jobs de publication réussissent. Un commit présent sur main n'est pas à lui seul une preuve que toutes ses images sont déjà publiées.

## Vérifications de remise du 26 octobre 2026

| Contrôle local | Résultat et portée |
|---|---|
| Go : modules racine, serveur-zone, placement | Compilation des packages, tests, `-race` et `go vet` réussis dans le stage Linux `verify` |
| Frontend | 9 tests Node réussis |
| Fleet sous Windows | 52 tests découverts : 47 exécutés, 5 tests shell réservés à Linux ignorés |
| Shell de déploiement sous Linux | Les 5 tests shell exécutés et réussis avec un faux Docker, sans SSH/réseau |
| Bibliothèques vidéo/sécurité | 21 tests réussis : live, SIS/IBS réel, opérations, reprises et contrats de reconstruction |
| Découverte/export et verrou Windows | 4 tests réussis sous Linux ; verrou Windows simulé dans le test de régression |
| Manifeste de replay | Test Go vérifiant tous les fragments après la fin et une URL d'initialisation stable |
| VOD Docker réelle | Deux propriétaires, originaux byte-identiques, Range HTTP, devis et refus de politiques, stockage isolé A/B : réussis |
| Live RTMP avec audio | Huit fragments de quatre secondes, H.264/AAC et octets identiques après reconstruction A/B, premier fragment vérifié avant fin de la source : réussis |
| Résilience live | Corruption et absence détectées, fichiers restaurés, ordre/état/droits et relecture froide identique après redémarrage complet : réussis |

Les tests Python de crypto utilisent les vraies bibliothèques, mais certains tests de contrats/reprises simulent leur transport. Les tests Fleet ne prouvent pas qu'un Raspberry Pi est connecté. Les tests shell ont besoin d'un conteneur de test pouvant créer leurs répertoires sous `/home` ; ce n'est pas une permission donnée aux services applicatifs.

Les vérifications d'intégration Docker de la remise sont exécutées dans une copie de travail isolée nommée `caribou-submission-live-20261026`, sur des ports distincts (frontend 21690) et avec des volumes propres. Les adaptations du nom de projet et des ports appartiennent au banc d'essai ; le code applicatif testé est celui de cette livraison. La démo d'origine n'est pas réinitialisée. La source de ces tests est une mire vidéo avec sinus audio, pas une caméra physique.

Un premier essai de ce banc a utilisé par erreur le port d'un ancien agent propriétaire : son identité n'avait donc pas d'espace dans le nouveau banc, et le stockage a correctement refusé. Le routage du banc d'essai a été corrigé. Le test VOD exige aussi désormais la même politique et un coût sous le plafond, plutôt qu'un prix absolument identique entre un devis sans réservation et une publication ultérieure. La reprise d'un placement déjà engagé reste vérifiée identique.

Le résumé machine de la remise est dans [preuves/validation-remise.json](preuves/validation-remise.json). Les résultats de CI associés au SHA final complètent ces preuves, notamment le navigateur et l'émulation ARM.

## Reproduire les tests

Depuis un environnement de validation, pas pendant un direct à conserver :

```powershell
# Tests Go Linux, analyse statique et concurrence sur les trois modules.
docker build --target verify -f deploy/lab/Dockerfile -t caribou/verify:local .

# Frontend sans dépendance npm à installer.
Push-Location frontend
npm test
Pop-Location

# Contrats d'automatisation, dont des tests avec faux Docker/SSH.
py -3 -m unittest discover -s deploy/fleet -p 'test_*.py' -v

# Bibliothèques réelles de l'image de sécurité construite depuis ce code.
docker run --rm --network none --cap-drop ALL --security-opt no-new-privileges:true caribou/secure-v3-security:dev python -m unittest test_live test_secure_sis test_secure_operations test_publication_retry test_reconstruction_contract test_sis_binding -v
```

Les environnements Windows peuvent refuser certaines synchronisations de répertoires dans des tests Go de persistance. Le résultat de référence pour ces tests est le stage Linux `verify`, pas un contournement du fsync applicatif pour rendre Windows vert.

Les intégrations existantes :

```powershell
# Banc sécurisé VOD : crée ses propres comptes et médias de test.
./deploy/secure-v3/start.ps1
./deploy/secure-v3/discovery.ps1 -WithVod
./deploy/secure-v3/operations.ps1
./deploy/secure-v3/resilience.ps1

# Banc live indépendant : ports 20690, 20591/20592, RTMP 21935/21936.
./deploy/live-v4/start.ps1
./deploy/live-v4/smoke.ps1
./deploy/live-v4/resilience.ps1
```

Ces commandes ne sont pas un conseil de relancer un runtime réel depuis une topologie d'exemple. La CI utilise des installations jetables. Le test navigateur complet est `deploy/live-v4/browser-check.cjs`, avec Playwright installé comme dans le workflow `live-v4.yml`.

## Ce qui a été observé pendant la démonstration

Le 20 octobre, plusieurs Windows physiques ont rejoint le banc sécurisé avec des comptes distincts. JULES1/WIN2/WIN3/WIN4 ont été observés dans les annonces à différents moments ; une admission ne suffit pas à déclarer chacun testé pour chaque parcours.

Pour le direct `l-2b7355639662cbed9b8ce3d1b365ac45`, le catalogue a été observé avec **59 fragments READY et 177 blocs** portant la preuve `HTTP_SHA256_VERIFIED` : 148 blocs sur JULES1 et 29 sur A1. Ce relevé montre une répartition entre deux PC **dans la zone A**. Il ne montre ni réplication ni stockage multizone de ce direct. Tous les blocs n'ont pas été relus exhaustivement lors de ce relevé.

Les intégrations synthétiques VOD/live réalisent séparément des lectures sur les zones A/B. Il faut donc distinguer « deux zones logiques testées dans Docker » de « deux zones physiques du laboratoire validées ».

La lecture réelle du premier essai de Tizio a produit 270 images en neuf secondes sur le navigateur utilisé, après un premier affichage à 6116 ms. Cela ne qualifie pas toutes les sources, tous les navigateurs ou la qualité HD. Un autre export, figé dans ce dossier, décrit un direct à fragments de 24 secondes et à retard important.

La relecture d'un ancien direct terminé a été vérifiée sur huit fragments, avec retour au premier et marqueur de fin : [preuve](preuves/relecture-verifiee-2026-10-20.json). Cette preuve porte sur le manifeste et le catalogue ; elle n'est pas une relecture exhaustive de toutes les données du laboratoire.

## AMD64, ARM64 et matériel

- Les tests Docker locaux de remise sont exécutés sur Linux AMD64 via Docker Desktop Windows.
- Les workflows construisent le contrôle et l'Edge pour AMD64/ARM64 et, pour le binaire de contrôle, ARMv7. Le worker sécurisé utilise des images AMD64/ARM64.
- Les tests ARM utilisent QEMU lorsqu'indiqué ; ils exécutent notamment la bibliothèque cryptographique et l'API Edge.
- Les vrais Raspberry Pi et Jetson n'ont pas été validés dans cette session pour l'intégralité du profil sécurisé, le son, les redémarrages et la charge.
- Les déploiements Fleet HTTP existants sur cartes ne deviennent pas des Edge sécurisés V3 sans la configuration et l'admission correspondantes.

## Limites à annoncer au jury

| Sujet | État honnête |
|---|---|
| VOD, catalogue, comptes et stockage | Intégrés ; tests avec vrais fichiers et contrôles d'accès ; une nouvelle erreur doit être diagnostiquée avec son identifiant |
| Live avec son | Parcours H.264/AAC, source RTMP/fichier et fichiers reconstruits ; les haut-parleurs physiques ne sont pas testés par FFmpeg |
| Faible latence | Non garantie ; profil 4 secondes/transcodage disponible mais pas déclaré validé de bout en bout sur les PC du laboratoire |
| Mesure caméra-écran | Non réalisée ; les exports donnent une estimation depuis les horodatages FFmpeg et des temps cumulés |
| Stockage distribué | Répartition réelle ; pas de réplication automatique ou de réparation d'un bloc perdu |
| Multizone physique | Pas démontré par le live observé A1/JULES1 ; séparé des tests Docker A/B |
| SIS/IBS | Bibliothèque réelle intégrée et testée ; pas de preuve de sécurité complète du choix de paramètres expérimental |
| Chiffrement au repos | Pas fourni par les signatures ni par un volume Docker |
| Identités et clés | Java, autorités de zone et mTLS intégrés ; provisioning et conservation des clés restent des tâches d'administration |
| Autoconfiguration | Découverte et routes authentifiées après admission ; pas configuration autonome du Wi-Fi/DHCP |
| Isolation utilisateur | Espaces préparés explicitement, pas de provisionneur permanent exposant le moteur Docker au web |
| Prix/QoS | Crédits et observations de démonstration ; pas de paiement, pas de SLA |
| RTP/UDP | Non réalisé dans le parcours HLS livré |
| Incidents résiduels | Un ancien 502 de devis reste sans cause exacte démontrée ; ne pas déclarer tous les 502 résolus par le budget |

La remise contient la dernière intégration et les correctifs conservés de la démo. Elle ne prétend pas que toutes les exigences du sujet et de l'article sont satisfaites sans ces réserves.
