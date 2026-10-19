# Audit auth et décisions proposées — à valider avant implémentation

État : **PROPOSITIONS NON VALIDÉES**, 19 octobre 2026. Branche de préparation `codex/conformity-v3-review`, issue de `0007fcb`. Le banc V2 n’est pas modifié. Aucune PR fusionnée.

## Preuves et limites de l’audit

La référence locale `origin/auth` est `f796767` (« Authentification, identifiant et token de connexion »). Les nouveaux `git fetch` échouent avec `Could not resolve host: github.com`. Le connecteur GitHub n’a pas accès au dépôt privé (404). Cet audit porte sur cette référence locale ; **le HEAD distant actuel n’est pas vérifié**. Avant de créer un composant qui pourrait exister dans des commits plus récents, rétablir l’accès et contrôler la branche.

Le répertoire `auth` de cette branche contient trois classes : `AuthService.java`, `CredentialStore.java`, `WebApplicationServer.java`. Il fournit PBKDF2-HMAC-SHA256, identifiants client stables, sessions opaques de 30 minutes, login, validation de session et logout. Il ne contient aucun service par zone d’émission de parts SIS/IBS ni de clés maîtresses cryptographiques. Les seules adaptations déjà intégrées à ces sources portent sur bind Docker, healthcheck et borne des formulaires HTTP ; le cœur Java d’authentification est conservé.

Les fonctions `setup_zone_keys`, `issue_secret_share`, `compute_owner_key`, `compute_visited_PK` et de délégation existent dans `videostreaming/IBSSignature.py`. Ce sont des primitives, pas des services d’autorité raccordés à auth. `SegmentSigner` crée actuellement une clé de zone pour chaque instance/vidéo, à la place d’une autorité persistante par zone.

Un défaut a été **reproduit**, sans clé de production, dans `lab/security-audit/characterize_legacy_ibs.py` : H1(ID) est implémenté par `SHA256(ID) * G1`, avec un coefficient public connu. Une part privée reçue par un propriétaire permet alors de dériver le point maître équivalent, puis une part d’un autre propriétaire. Le test a produit une signature de cet autre propriétaire acceptée par le vérifieur existant. Voir `preuves/ibs-characterization.json`. Cela ne concerne pas le mot de passe Java ; le parcours VOD actuel ne s’appuie pas sur ces signatures. Ce résultat est une vulnérabilité reproduite, **pas un test de sécurité réussi**.

Une seconde caractérisation a trouvé deux blocs synthétiques différents après **80 103 candidats**, avec des SHA256 complets différents mais le même H3 tronqué à 32 bits. Le vérifieur SIS du prototype accepte le bloc modifié avec le V du bloc initial. Voir `preuves/sis-characterization.json` et `lab/security-audit/characterize_legacy_sis.py`. Le contrôle SHA256 complet du parcours V2 refuse cette substitution : ne pas confondre cette preuve sur le prototype SIS avec une compromission du parcours VOD actuel.

Autres limites constatées : tirages `random` dans les secrets IBS ; H3 tronqué à 32 bits par défaut ; sérialisation à versionner et validation des points à durcir. Un simple raccord HTTP des composants actuels ne peut donc pas être présenté comme conforme et sûr.

## D1 — conserver auth et ajouter les autorités de zone

**Proposé :** conserver Java pour les comptes/sessions/client_id ; un service Identification Server Python par zone réutilise les primitives corrigées du collègue. Le service vérifie la session via auth, ne confond jamais cookie de session et clé cryptographique, et garde sa propre clé maîtresse persistante dans un volume privé. Chaque couple (client_id, zone, époque de clé) reçoit une seule part ; une visite répétée est idempotente. Registre durable des émissions, clés publiques historiques et reçus d’identification.

**Motif :** séparer authentification du compte et fonction d’autorité du papier sans recréer un second système de comptes. Aucun mot de passe utilisateur ni clé maîtresse dans MQTT, dans les manifestes publics ou sur les stockages Edge.

**Alternative :** faire aussi porter l’autorité cryptographique par Java ; demande un portage des primitives et modifie davantage le module du collègue. Non recommandé pour cette intégration.

## D2 — corriger/versionner la cryptographie avant le raccord

**Proposé :** conserver les équations SIS et IBS/Hess du collègue, mais introduire une suite explicitement versionnée : BLS12-381 avec hash-to-curve vers G1 selon RFC 9380, aléatoire cryptographique, encodage canonique des objets signés, contrôle des points et sous-groupes. Il s’agit de corriger la primitive H1, pas de remplacer IBS par une signature BLS ordinaire. Le changement de courbe/encodage exige de nouvelles clés et une nouvelle version de signature ; les anciennes signatures BN254 simplifiées ne sont pas déclarées authentiques par le nouveau mode.

**SIS proposé pour les essais :** H3 SHA256 non tronqué (`lambda=256`), `l=128`, `m=4096`, `q=65537` ; m est multiple de lambda et dépasse l*log2(q). Conserver la construction A à partir de paramA du papier, contrôler les bornes et les cas dégénérés, mesurer le temps et la mémoire avant adoption. Ce choix respecte ces contraintes de forme mais ne constitue pas une preuve de niveau de sécurité ; aucune certification cryptographique n’est revendiquée. Toute modification ultérieure des paramètres sera expliquée et soumise à validation.

**Motif :** le test d’usurpation doit devenir refusé avant toute activation du mode SIS/IBS. Les médias V1/V2 existants restent disponibles dans leur ancien mode explicitement identifié ; pas de suppression ni de conversion silencieuse de signatures.

## D3 — où résident les clés du propriétaire

**Recommandé, modèle du papier :** compléter l’agent client du collègue pour détenir la clé propriétaire côté client, dans un volume privé d’un agent local. Il reçoit les parts sur canal authentifié, agrège les parts une seule fois par zone/époque, signe et vérifie avec une ancre fournie par une autorité connue. Central et les stockages Edge ne possèdent pas cette clé. Les fichiers de clés sont exclus de Git et protégés par permissions ; un volume Docker seul ne promet pas un chiffrement de disque.

**Conséquence :** le mode conforme nécessite l’agent client local en plus de la page web. Les clés et paramètres sont persistants, jamais recréés à chaque vidéo. Le mode SIS exige une vérification complète avant publication et lecture ; il échoue si l’agent ou les ancres manquent, sans rétrograder silencieusement vers SHA256.

**Alternative :** signature côté serveur sous délégation explicite. Plus simple pour un navigateur seul, mais le serveur devient un acteur de confiance supplémentaire et la délégation doit être réalisée/vérifiée. Ne pas le présenter comme équivalent au modèle où seul le propriétaire garde sa clé.

## D4 — transport sécurisé et admission des nœuds

**Proposé :** certificats de services/nœuds provisionnés par une autorité d’installation connue, TLS entre les services et mTLS pour les communications interservices et entre brokers. Ce certificat de transport est distinct de la clé maîtresse IBS d’une zone. Le service Java et ses identifiants sont réutilisés ; une session ne prouve pas à elle seule la légitimité d’un nouveau nœud Edge.

Pour rejoindre : jeton d’enrôlement administrateur à usage unique et durée courte, identité de nœud stable liée au certificat, puis renouvellement authentifié. Ni le multicast ni un nom annoncé ne valent confiance. Les anciennes clés publiques nécessaires aux données historiques restent conservées ; une révocation bloque les nouvelles opérations, sans prétendre effacer une connaissance déjà divulguée.

**Conséquence :** distribution initiale d’une racine de confiance et d’un jeton par machine. Les autorités et leurs volumes restent sur des hôtes considérés de confiance. Pas de modification automatique du pare-feu ni du magasin de certificats Windows.

## D5 — autoconfiguration et maillage

**Proposé :** IP attribuées par le DHCP du laboratoire ; découverte DNS-SD/mDNS des services via un petit agent sur l’hôte Windows/Linux, car la diffusion LAN ne doit pas être supposée traverser le bridge Docker Desktop. Choix d’interface LAN configurable, et adresses de secours explicites si le réseau filtre le multicast. L’annonce permet de découvrir ; l’enrôlement D4 autorise réellement l’inscription et la récupération de la configuration.

Chaque Edge conserve son broker. Les pairs découverts et authentifiés établissent leurs liens directs, avec routes bornées aux messages de leurs identités d’origine, délais, identifiants de messages et déduplication ; pas de forwarding bidirectionnel `#` qui créerait des boucles. Départs, nouvelle IP, doublons et reconnexion doivent être testés. Une machine nouvelle doit pouvoir rejoindre sans ajouter son IP à une liste statique de tous les autres nœuds.

**Limite assumée :** ce maillage enlève le broker hub unique pour les échanges de pairs ; il ne crée pas un second catalogue. Le scheduler Go et Central SQLite restent autoritaires pour les nouvelles allocations et les commits. Une panne de Central ne permet pas de déclarer de nouveaux placements READY en dehors du catalogue. Le protocole de découverte ne configure pas lui-même un routeur, un serveur DHCP ni le Wi-Fi du laboratoire.

## D6 — isolation Docker effective par utilisateur

**Proposé :** un volume et un conteneur de stockage distincts par couple (client_id, Edge). L’Edge logique garde son identité et son contrôle ; chaque processus de stockage ne voit que le volume d’un propriétaire. Les demandes autorisées de lecteurs passent par les capabilités de lecture ; lire une vidéo partagée ne donne pas accès au volume ni au token d’écriture du propriétaire.

Création via un provisionneur local isolé, à API limitée aux images/labels/volumes du projet, jamais via un socket Docker monté dans le frontend ou l’API web. Ce provisionneur détient un accès privilégié au moteur Docker et doit être traité comme un composant d’administration ; limites du nombre de conteneurs, quotas et nettoyage contrôlé. Alternative plus simple : création par CLI administrateur, mais elle n’est pas automatique lors du premier upload.

Le quota global projet d’un Edge reste calculé à partir du ledger et couvre tous ses utilisateurs. Des quotas par utilisateur sont ajoutés sans réutiliser la capacité physique brute comme espace offert. Les nouvelles publications utilisent ces volumes ; les anciennes vidéos V2 restent dans leur stockage historique. Une migration des anciens octets sera une opération séparée, sur copie vérifiée, jamais implicite.

## D7 — QoS/prix : contrat explicite, pas de fausse garantie

**Proposé :** devis avant réservation, budget maximum et tarifs configurables en crédits de démonstration par GiB-heure, sans paiement réel. Critères de QoS initiaux : quota disponible et plafond de latence observée, avec affichage des mesures et de leur fraîcheur. Profils proposés : Libre (sans contrainte supplémentaire), Économique (plafond de prix), Réactif (plafond de latence) ; seuils et tarifs sont des paramètres visibles, pas des garanties commerciales.

Les critères filtrent les candidats **dans le chemin Go autoritaire**, puis le scheduler existant conserve sa stratégie et ses priorités. Le devis du plan réellement choisi doit respecter le budget avant commit ; si aucun plan admissible n’existe, refus explicite. Ne pas prétendre calculer un optimum de coût ou garantir une latence future. La réplication/réparation automatique n’est pas ajoutée silencieusement dans ce lot ; le stockage multizone actuel distribue des blocs différents, sans copies redondantes. MQTT QoS1 reste distinct de cette QoS de stockage/lecture.

## D8 — opérations authentifiées et versions

**Proposé :** publication, lecture, partage/retrait du partage, remplacement et suppression autorisés par identité et signature ; manifeste signé liant propriétaire, vidéo, version, paramètres SIS, contenu, droits et version précédente. Nonces et identifiants d’action durables, conditions de version pour éviter rejeux et écrasements concurrents. Les paramètres de réseau/placement mobiles restent séparés du contenu signé pour permettre les déplacements sans falsifier le média.

Modification/insertion/suppression de blocs produisent une nouvelle version immuable, un nouveau V et une nouvelle signature ; un média invalide ne devient pas lisible READY. Les opérations vidéo usuelles passent par une source revalidée et un redécoupage, pas une édition arbitraire de blocs pouvant rendre le MP4 illisible. Le commit du catalogue précède la collecte physique contrôlée ; le quota n’est rendu disponible qu’après preuve de suppression réelle, avec réessais et reprise après panne.

Délégation selon les primitives corrigées du collègue, mais limitée par un mandat signé (objet/version/opérations/durée), révocable pour les nouvelles opérations. Jamais de clé maître ou clé propriétaire complète envoyée au stockage. Les versions déjà publiées restent vérifiables avec leurs clés publiques historiques ; retirer le droit de signer ne signifie pas effacer le passé.

## Ordre proposé et preuves attendues

1. Récupérer le HEAD réel d’auth dès que GitHub redevient accessible, comparer les contrats et réutiliser tout service existant.
2. Corriger le prototype dans la nouvelle suite, puis faire refuser l’usurpation reproduite, les clés/points malformés, paramètres non permis, mauvais propriétaire/contexte et rejeux.
3. Relier auth → autorités par zone → agent propriétaire → publication/lecture avec SIS/IBS obligatoire dans le nouveau mode ; tester mobilité des clés, reprises et historique. Préserver le smoke V2.
4. Activer stockage isolé et opérations versionnées ; prouver l’absence d’accès entre utilisateurs, délégation/révocation, panne partielle, absence de suppression de données hors jeu d’essai.
5. Ajouter admission/découverte et liens de brokers directs ; tester sans hub avec des échanges réels. Tests Docker et émulation ne sont pas présentés comme des essais multicast/Wi-Fi physiques.
6. Ajouter devis/QoS et tests de budget/mesure périmée ; exécuter la CI sur le SHA final dès que l’accès GitHub est disponible, PR draft sans merge.

Les choix D1–D8 restent à valider par l’utilisateur avant de modifier les composants opérationnels. Les ajustements d’algorithme cryptographique ou de paramètres non listés ici seront à nouveau soumis ; les détails internes qui ne changent pas ces contrats seront expliqués dans la livraison.

## Sources primaires utilisées pour préparer ces choix

- Article joint Sellami/Imine/Gallais, §§3.4–3.6 : autorités de zone, parts privées et clés cumulées, SIS/IBS, opérations et délégation.
- Sujet joint Projet_2024_2025, p.2 : découverte/autoconfiguration, plusieurs brokers, pair-à-pair, QoS/prix, isolation Docker par client.
- RFC 6762 / DNS-SD : https://www.rfc-editor.org/rfc/rfc6762 ; https://www.rfc-editor.org/rfc/rfc6763
- RFC 9380 : https://www.rfc-editor.org/rfc/rfc9380.html
- Documentation Mosquitto : https://mosquitto.org/man/mosquitto-conf-5.html
- Sécurité du moteur Docker : https://docs.docker.com/engine/security/
- Volumes Docker : https://docs.docker.com/engine/storage/volumes/

## Reproduire les deux caractérisations sans clé réelle

Depuis la racine du dépôt, les dépendances existent dans l’image V2 déjà construite :

```powershell
docker run --rm -v "${PWD}/videostreaming:/code:ro" -v "${PWD}/lab/security-audit:/audit:ro" -w /code -e PYTHONPATH=/code caribou/edge-v2-reader:dev python /audit/characterize_legacy_ibs.py
docker run --rm -v "${PWD}/videostreaming:/code:ro" -v "${PWD}/lab/security-audit:/audit:ro" -w /code -e PYTHONPATH=/code caribou/edge-v2-reader:dev python /audit/characterize_legacy_sis.py
```

Ces scripts caractérisent volontairement les défauts du prototype conservé. Une sortie montrant une usurpation/collision acceptée est un constat d’échec de sécurité. Ils ne sont pas ajoutés comme des tests verts de la future suite corrigée.
