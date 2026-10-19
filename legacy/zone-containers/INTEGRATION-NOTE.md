# Source Dan conservée

Import de containers c95a3b6 (historique Git indépendant), sans remplacement du serveur-zone actif.
Le module inclut le registre et les topics MQTT5 récents de Dan. Son allocator round-robin, sa capacité disque et ses tailles égales divergent du contrat HRW/quota/extents : il reste hors du profil intégré. Les sources et son Compose sont conservés pour comparaison et exécution séparée, sans connexion au broker opérationnel. Aucun historique distant modifié. Formatage Go uniquement lors de l'import.
