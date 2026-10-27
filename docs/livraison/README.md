# Guide de livraison CaribouTV

État documenté le **26 octobre 2026**. Ce guide décrit le code intégré et les observations de la démonstration du 20 octobre. Il ne transforme pas les tests anciens en validation du matériel encore indisponible.

## Le projet, simplement

Le propriétaire d'une vidéo se connecte avec son compte. Son agent local prépare et signe la vidéo. Le contrôleur Central tient le catalogue et les contrôleurs de zone demandent au scheduler Go où placer les blocs. Les Edge conservent les octets dans leurs volumes. Pour regarder, le système retrouve ces blocs, les vérifie et reconstitue la vidéo.

Les Raspberry Pi et Jetson peuvent servir au stockage sans héberger une interface utilisateur. Sur un portable, on peut ajouter les rôles producteur et spectateur. Le navigateur seul ne reçoit pas les clés privées de signature.

Le direct réutilise cette chaîne : OBS produit des fragments vidéo et audio qui sont signés et stockés avant d'être servis au lecteur. Cela donne une diffusion HLS persistante, avec du retard ; cela ne constitue pas le transport RTP/UDP à faible latence de la dernière partie du sujet.

## Ce qui est livré sur main

- Le travail existant des collègues est conservé : comptes Java, frontend, registre et contrôleur de zone, Edge Go, découpage et reconstruction.
- Les contrôleurs, le scheduler, SQLite et les interfaces VOD sont intégrés.
- Le profil sécurisé utilise TLS/mTLS, des autorités de zone, des signatures SIS/IBS versionnées et des volumes séparés par propriétaire/Edge.
- Les participants Windows disposent de paquets personnels, d'un quota choisi à l'installation et des commandes `start.ps1` et `publish.ps1`.
- Le live accepte un fichier ou OBS, transporte H.264/AAC et exporte des mesures.
- Les correctifs de la démo sont dans les sources : budget explicite, blocs live de 1 Mio, reprise de découverte Windows, transcodage optionnel, tampon réduit et relecture complète d'un direct terminé.

Les anciens ZIP nommés « correctif » ne sont pas une dépendance d'un nouveau déploiement construit depuis ce code. Leurs modifications utiles sont intégrées. Un ancien bundle doit toutefois recevoir une image récente ou le correctif de compatibilité prévu ; un `git pull` sur le contrôleur ne modifie pas à distance les conteneurs d'un autre PC.

## Commencer selon son rôle

| Vous êtes… | À lire |
|---|---|
| La personne qui héberge Central et les deux zones | [Démarrage, étapes 1 à 3](DEMARRAGE.md) |
| La personne qui prépare les comptes et paquets des participants | [Démarrage, admission et export](DEMARRAGE.md) |
| Un participant qui a reçu son paquet | [Installer son paquet](DEMARRAGE.md#sur-le-pc-participant) |
| Le producteur du live | [Vidéo, son et mesures](VIDEO-ET-MESURES.md) |
| Un évaluateur | [Architecture](ARCHITECTURE.md), puis [validation et limites](VALIDATION.md) |
| Bloqué par une erreur | [Dépannage](DEPANNAGE.md) |

## Parcours de démonstration conseillé

1. Démarrer les contrôleurs et les Edge prévus, sur un réseau commun sans conflit de sous-réseaux Docker.
2. Vérifier que chaque participant se connecte avec son propre compte et que les Edge deviennent disponibles.
3. Publier une petite VOD depuis l'agent local : préparer le devis, publier, obtenir `READY`, puis lire.
4. Vérifier qu'une vidéo privée n'est pas visible avec le compte d'un autre propriétaire. Pour la montrer aux autres, effectuer l'opération signée `share`.
5. Lancer un seul producteur live ; attendre `CREATED`, puis démarrer OBS. Utiliser le nouvel identifiant `l-…` dans le lecteur.
6. Exporter les mesures du navigateur et celles du producteur. La durée réelle des fragments et le temps de publication doivent être vérifiés, pas supposés.
7. Arrêter la diffusion dans OBS et laisser la commande vider sa file. Le direct terminé peut être relu depuis le début tant que ses blocs restent accessibles.

Les commandes de tests de résilience créent des médias, modifient leurs propres objets de test et redémarrent certains services. Elles sont destinées à un banc de validation, pas à une démonstration en cours.
