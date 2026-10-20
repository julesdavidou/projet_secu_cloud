# Conformité au sujet et à l’article — état vérifié le 19/10/2026

**Conclusion : conformité partielle, pas une réalisation complète du sujet ou de l’article.** Les tests du banc attestent le comportement exécuté ; ils ne remplacent ni les essais matériels ni une validation cryptographique.

Les sources sont les pièces fournies par l’utilisateur : `Projet_2024_2025.pdf`, 3 pages, et Sellami/Imine/Gallais, *A verifiable data integrity scheme for distributed data sharing in fog computing architecture*, FGCS 150 (2024), 64–77, 14 pages. Ces documents servent de référence aux exigences ; ils ne remplacent pas les instructions explicites de l’utilisateur sur la conservation du travail des collègues et l’interdiction de fusionner une PR.

| Exigence | État réellement livré | Écart restant |
|---|---|---|
| Sujet §1 : cartes et réseau autoconfigurable | Images AMD64/ARM64 ; exécution ARM64 sous émulation, exports de configuration LAN | Pas de déploiement physique Jetson/Raspberry vérifié ; IP/identités/zones provisionnées, pas d’autodécouverte |
| Sujet §2 : traitement et stockage locaux Edge | Vrais fichiers écrits par Edge Agent Go ; assemblage Python sur Edge ; SHA256 et tailles vérifiés | Pas de réparation automatique d’un bloc perdu ; cette lecture est refusée |
| Sujet §3 : plusieurs brokers, MQTT, identifiants uniques, pair-à-pair | Broker par Edge et hub ; ACL par identité ; MQTT3/5, commandes, ACK et reconnexion testés | Fédération via hub, pas de maillage autonome pair-à-pair |
| Sujet §4 : visualisation, partage, récupération ; orchestration | Frontend, catalogue, login, droits privés/partagés, capacités projet ; placement Go autoritaire ; téléchargement et lecture réels | Choix/distribution selon QoS et prix non réalisés comme fonctionnalité utilisateur |
| Sujet §5 : protocole de partage et SIS de l’article | Contrat de blocs versionné ; prototype SIS/IBS du collègue conservé et testé ; contexte signé/ancre de confiance préparés | Le chemin web de publication/lecture utilise encore SHA256, sans production des métadonnées SIS/IBS |
| Sujet §6 : Docker et stockage dédié par client | Conteneurs, volumes par Edge ; isolation applicative des comptes testée | Tenant géré `project` partagé dans chaque volume Edge ; pas d’espace Docker distinct par client |
| Rapport et guide d’installation/utilisation | Guide PowerShell, matrice de contrats, livraison, preuves locales et configurations exportables | Guide matériel à valider sur les machines du laboratoire |
| Article §3.4.1–3.4.2 : autorités d’identification et clés | Fonctions de génération/partage/agrégation disponibles dans le prototype | Pas de service d’identification par zone raccordé, ni de distribution sécurisée des parts et cycle de clés de mobilité opérationnels |
| Article §3.4.3–3.4.4 : matrice SIS et signature/vérification IBS | Calculs du prototype et tests de liaison au contexte ; worker exige une ancre avec les champs signés | Pas de conformité cryptographique démontrée ; paramètres de démonstration, H3 tronqué à 32 bits par défaut et tirages `random` à revoir ; ancre indépendante à provisionner dans la chaîne complète |
| Article §3.5–3.6 : opérations dynamiques et délégation | Fonctions cryptographiques de délégation présentes chez le collègue | Pas de publication des modifications/suppressions/insertions et signatures déléguées orchestrées de bout en bout ; suppression non coordonnée des objets gérés refusée |

**SHA256 n’est pas l’algorithme SIS de l’article.** Les essais de corruption SHA256 prouvent le refus d’une altération par rapport aux digests attendus du catalogue ; ils ne prouvent pas l’authenticité distribuée définie par le protocole de l’article. Les tests de mobilité actuels portent sur la zone d’un nœud, pas sur l’accumulation des clés d’un propriétaire entre autorités de confiance.

Priorités pour terminer : raccorder le prototype SIS/IBS à la publication et à la lecture avec de vraies autorités et ancres de confiance ; définir et réaliser l’isolation Docker par utilisateur ; traiter l’autoconfiguration et le réseau pair-à-pair attendu ; déployer/tester sur cartes ; compléter QoS/prix et opérations authentifiées selon le périmètre retenu avec l’encadrant. Ces écarts sont documentés sans réécrire le travail des collègues dans cette livraison.

Publication : la branche locale est complète ; l’accès GitHub du processus échoue toujours sur DNS/HTTPS et le connecteur retourne 404 pour le dépôt privé. Aucune nouvelle PR ni CI distante réussie n’est revendiquée tant que cette publication n’a pas abouti. Le script `deploy/edge-v2/publish-draft.ps1` pousse la branche et ouvre une draft vers main, sans merge ; il peut être lancé par son chemin absolu depuis un PowerShell Windows disposant de l’accès GitHub.
