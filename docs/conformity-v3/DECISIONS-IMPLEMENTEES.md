# Décisions retenues après le retour utilisateur du 19 octobre 2026

L’utilisateur demande le meilleur niveau de sécurité possible avec un coût raisonnable, une découverte gérée par les Zone Controllers, de la faible latence et une bonne qualité vidéo, en réutilisant les collègues. Les choix sont donc appliqués par lots vérifiables. Aucun merge de PR n’est autorisé.

## Identités et MQTT

- Le `node_id` reste stable entre les connexions : il est référencé dans les placements, le ledger durable, les objets et les topics. Le renouveler à chaque connexion ferait apparaître un autre nœud et romprait les références des fichiers.
- Le `client_id` MQTT désigne la session de transport ; il ne remplace pas l’identité persistante du nœud. Les connexions de bridges ont leurs propres client_id. La corrélation des requêtes est aléatoire et indépendante des deux.
- Le client standalone du collègue conserve la possibilité d’utiliser le username/password partagé du laboratoire. Ce secret partagé ne prouve pas quel nœud publie : il n’est pas utilisé seul pour autoriser les commandes sensibles du nouveau profil.
- Le nouveau profil authentifie chaque acteur par certificat mTLS et Mosquitto utilise son identité certifiée pour les ACL, indépendamment d’un username commun. Les topics métier sont conservés, avec déduplication bornée par identité/corrélation/délai.
- `origin/auth` revérifié sur GitHub : `f796767`, aucun nouveau service d’autorité. Les derniers correctifs MQTT `4bb755d` sont intégrés en conservant l’historique et les protections V2.

## Cryptographie et lecture — premier lot opérationnel

Les équations SIS et IBS/Hess du collègue sont réutilisées dans une suite versionnée BLS12-381 avec hash-to-curve, tirages cryptographiques et H3 complet à 256 bits. Le profil d’essai SIS est l=128, m=4096, q=65537. Les points invalides, identités/contextes altérés et l’usurpation constatée dans le prototype sont refusés dans les tests de la nouvelle suite. Cela ne constitue pas une preuve de sécurité des paramètres ou du générateur de matrices de l’article.

Auth Java continue de gérer comptes et sessions. Les autorités par zone ont des clés persistantes et émettent des parts idempotentes après contrôle d’une session. Les clés privées du propriétaire restent dans son agent local ; elles ne sont pas envoyées au web, à Central, à MQTT ou aux disques de stockage Edge. Les clés de transport et les clés IBS sont distinctes.

Le nouveau parcours exige une publication signée : le web valide les preuves avant le placement Go et les workers Edge vérifient à nouveau les blocs reçus et leurs preuves SIS/IBS. Les clés publiques de confiance sont récupérées auprès des autorités configurées via mTLS, pas acceptées depuis un manifeste non fiable. Le parcours signé n’a pas de rétrogradation automatique vers SHA256 seul.

L’agent propriétaire signe aussi les assets HLS préparés avec le découpage existant du collègue. La source H264/AAC peut donc être lue sans réencodage supplémentaire, avec restitution exacte de ses octets. Les nouveaux médias conservent la source. Le frontend existant reste le lecteur/catalogue.

Pour la latence, les reconstructions vérifiées sont mises en cache une minute, liées aux preuves/version et contrôlées par SHA256 avant réutilisation. Les droits et le statut du catalogue sont vérifiés à chaque requête ; les clés privées n’entrent jamais dans ce cache. Les lectures de vidéos différentes ne partagent plus un verrou global pendant la vérification cryptographique. Le profil V2 garde son comportement de relecture et de refus de corruption testé précédemment.

Preuve locale : vidéo `v-eec8eab45ff37cbb38ba967bbd05b9cb`, source 229040 octets, SHA256 `46a18630584a545188667a835fdc1cd35b2ee86bf9735ff24e9481ff549f8612`. Publication 11,393 s, préparation froide 2,188 s, préparation chaude 0,049 s sur PC AMD64 ; fichier et reconstruction réels. Le matériel ARM physique reste non testé.

## Lots suivants en cours

La découverte/admission sera rattachée aux Zone Controllers ; les liens de brokers seront directs entre pairs connus des zones. Le stockage dispose maintenant d’un volume et d’un conteneur distincts par utilisateur et Edge, créés par une commande d’administration ponctuelle explicitement validée. Aucun service ne reçoit le moteur Docker. Les tests réels couvrent deux utilisateurs sur deux zones, les refus croisés et les redémarrages. Voir `LANCEMENT-ET-ISOLATION.md`. Prix et latence observée filtreront l’admission dans le chemin de placement Go, avec des paramètres visibles ; le calcul de placement ne sera pas redéveloppé. Les garanties commerciales de latence et les paiements réels ne sont pas introduits.

Les opérations signées, leur contrôle de version et la délégation devront être vérifiés avec leur reprise après panne. Ces derniers points ne sont pas déclarés terminés dans ce document avant leurs tests d’intégration.

La PR V2 est désormais https://github.com/julesdavidou/projet_secu_cloud/pull/5 (draft), SHA 0007fcb ; ses cinq workflows sont verts. Ce résultat ne couvre pas encore les modifications V3.

## Validation complémentaire du stockage isolé

Deux sources MP4 de 1 484 048 et 1 483 738 octets ont été signées puis réparties réellement entre A1 et B1, avant reconstruction identique. Une réservation temporaire non stockée force la répartition et est annulée dans finally ; elle ne simule aucun fichier STORED. Les quatre conteneurs et quatre volumes sont distincts. Namespace d’un autre utilisateur, jeton d’un autre conteneur et certificat de l’autre Edge sont refusés.

La corruption et l’absence d’un bloc sont refusées, puis le fichier est restauré. Les ACK directs passent avec le hub arrêté après correction des ACL locales des bridges et d’un verrou qui retardait le ping pendant l’enregistrement à Central. Après redémarrage, les quotas, clés des autorités et fichiers sont identiques. Les anciennes signatures restent vérifiables après accumulation d’une part d’une seconde zone.

Le binaire Edge Go ARM64 a démarré et répondu sur /health sous émulation Docker ; ce résultat n’est pas un essai sur matériel physique. La CI V3 ajoutée couvre le parcours réel AMD64 et la construction des quatre images ARM64. Son résultat doit être vérifié sur le SHA publié, séparément des anciennes CI V2.
