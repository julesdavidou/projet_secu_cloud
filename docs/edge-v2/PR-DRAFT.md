Intègre les travaux MQTT Edge (`82e140e`) et client/reconstruction (`e7ca637`) sur l’intégration PR4, déjà présente dans main `885e6ad`. Les deux historiques sont conservés. Le conflit HTTP garde les protections Bearer et les écritures vérifiées.

Le nouveau profil `deploy/edge-v2` exécute le vrai Edge Agent Go (stockage et contrôle), un broker par Edge et le worker de reconstruction du collègue. A1 utilise MQTT3.1.1, B1 MQTT5, avec les mêmes commandes, ACL, identités, quotas et ACK métier. Scheduler Go, Zone Controllers et catalogue Central SQLite restent seuls responsables du placement. L’ancien parcours V1 est conservé ; le frontend et l’authentification Java ne sont pas remplacés.

Les nouvelles publications conservent aussi la source, découpée en blocs à tailles explicites. L’Edge reconstruit depuis les nœuds réellement choisis par Go, avec des tickets GET à portée limitée ; le backend et le worker vérifient taille et SHA256. Le MP4 source H264/AAC est restitué octet pour octet. Les anciens médias restent lisibles selon leur ancien format. Les réservations annulées sont libérées avec réessai sans supprimer les octets stockés.

Validation locale : trois modules Go (tests/race/vet), six tests Python, neuf tests frontend, vraie publication/lecture et droits privés/partagés, stockage multizone, mobilité logique, corruption et bloc absent sur disque avec restauration, refus d’usurpation MQTT, reconnexion broker, redémarrage durable. Le smoke antérieur PR4 passe sur les nouveaux binaires. Quatre images AMD64/ARM64 construites ; le banc complet ARM64 et son smoke passent sous émulation QEMU.

Documentation et commandes : [démarrage](docs/edge-v2/DEMARRAGE.md), [matrice préalable des contrats](docs/edge-v2/CONTRATS-ET-CONFLITS.md), [livraison, preuves et écarts au sujet](docs/edge-v2/LIVRAISON.md).

Limites : matériel Raspberry/Jetson et mobilité réseau physique non testés. Le parcours VOD utilise SHA256 ; le prototype SIS/IBS est conservé, testé et raccordable avec contexte signé/ancre épinglée, mais la publication web ne produit pas encore ces signatures. Identification Servers, conformité cryptographique à l’article, autodécouverte réseau, maillage autonome et volumes Docker par utilisateur restent à terminer. CI GitHub à vérifier sur le SHA publié ; les résultats locaux ne sont pas présentés comme une CI distante réussie.

PR volontairement draft ; aucun merge automatique.
