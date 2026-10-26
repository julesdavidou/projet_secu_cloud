# Vidéos, son, direct et relecture

## Publier une VOD depuis son paquet

1. Démarrer `start.ps1` et ouvrir `http://localhost:18791/`.
2. Se connecter avec le compte du paquet, trouvé dans `account.private.json` pour les comptes de démonstration.
3. Choisir le MP4 et préparer le devis. L'agent prépare les signatures : cette étape peut durer plusieurs dizaines de secondes selon le média et le CPU.
4. Lire le résultat : `QUOTED` signifie « devis calculé », pas « vidéo publiée ».
5. Cliquer sur **Publier ou reprendre le même paquet** avec le `v-…` obtenu ; attendre `READY`.
6. Lire la vidéo dans le catalogue sur `http://localhost:18790/`.

L'interface de l'agent propriétaire limite actuellement la source VOD à 16 Mio. Ce seuil ne doit pas être confondu avec les limites internes de l'API Go et les quotas de stockage. Un fichier de 8 Mo n'est pas trop gros par définition, mais le devis doit couvrir aussi les fichiers/fragments du paquet et la durée choisie.

Le budget initial de l'interface est de 100000 microcrédits pour 24 heures. Dans la démo, ce plafond a refusé des médias pourtant valides. Pour un premier essai, réduire la durée du devis à 1 heure ou choisir un budget de démonstration suffisant. Ne pas supprimer les contrôles de prix pour contourner un refus.

Une VOD est privée par défaut dans cette interface. Pour la rendre visible aux autres comptes, utiliser l'opération signée `share` sur son identifiant racine. Les autres utilisateurs se connectent avec leur propre compte ; ils n'ont pas à recevoir le mot de passe du producteur.

## Direct : ce qui se passe réellement

Le chemin est :

```text
Fichier ou OBS (caméra/écran + audio)
  -> RTMP/TCP local au PC producteur
  -> FFmpeg : fragments H.264/AAC en fMP4
  -> signatures du propriétaire
  -> API web + Central + zones + placement Go
  -> fichiers réellement écrits sur les Edge
  -> reconstruction et vérification sur Edge
  -> HLS au navigateur
```

C'est une diffusion segmentée persistante, pas RTP/UDP. Un fragment doit être finalisé et validé avant de devenir lisible. Le délai dépend donc du découpage, des images clés, du calcul, du réseau et du tampon du lecteur.

La nouvelle source live découpe les fragments en blocs de 1 Mio au maximum, avec un dernier bloc variable. Le manifeste porte leur taille et leurs empreintes. Les contrôles Go, SHA-256 et SIS/IBS restent actifs. Les paquets VOD conservent leur découpage existant.

## Lancer un direct OBS

Dans le paquet personnel récent, PowerShell reste ouvert pendant le direct :

```powershell
pwsh -NoProfile -File .\publish.ps1 -UseBundleAccount -Public -BudgetMicrocredits 1000000 -SegmentSeconds 4 -Transcode
```

Pour une installation déjà utilisée avec les correctifs V2/V3, s'assurer que `publish.ps1` accepte les paramètres ci-dessus et que le module live de l'agent a aussi été mis à jour. Dans un nouveau paquet construit depuis main, ils viennent des sources et images récentes ; on ne réinstalle pas une ancienne archive par-dessus.

`-Transcode` active un réencodage H.264/AAC et force des images clés au découpage demandé. Cela consomme du CPU. Sans cette option, FFmpeg copie le flux et attend les images clés de la source : `-SegmentSeconds 4` seul ne garantit pas des fragments réels de 4 secondes.

Après le message `CREATED`, ouvrir ou utiliser OBS :

| Paramètre OBS | Valeur du paquet Windows standard |
|---|---|
| Service | Personnalisé |
| Serveur | `rtmp://127.0.0.1:22935/live` |
| Clé | `stream` |
| Codec vidéo | H.264 |
| Codec audio | AAC |
| Intervalle d'images clés | 2 secondes |

OBS ne s'ouvre pas automatiquement. Ajouter une source visible (caméra, écran ou média) et une source audio ; vérifier les vumètres, puis cliquer sur Démarrer le streaming. La présence de H.264/AAC dans le programme ne crée pas du son si OBS n'en capture pas.

Pour un premier contrôle de stabilité, un débit OBS modéré comme 800 kbit/s et un audio à 64 kbit/s évite d'envoyer plusieurs Mo par fragment. C'est un profil de diagnostic à qualité réduite, pas la qualité maximale visée par le projet. Vérifier les caractéristiques réellement reçues avant d'annoncer 720p ou 30 images/s.

Si le traitement dépasse continuellement les 4 secondes, ce profil ne tient pas le rythme. Un essai à 8 ou 12 secondes peut être plus stable, mais augmente le délai. Augmenter `max_pending` masque temporairement le retard ; cela ne rend pas la chaîne plus rapide.

Une seule commande productrice doit écouter le port RTMP d'un paquet à la fois. Pour un second producteur, utiliser un autre paquet/agent personnel ; un second navigateur n'exige pas un nouvel agent de publication.

## Diffuser un fichier à vitesse réelle

```powershell
pwsh -NoProfile -File .\publish.ps1 -UseBundleAccount -Public `
    -FilePath 'C:/Videos/demo.mp4' -Title 'Démonstration' `
    -BudgetMicrocredits 1000000 -SegmentSeconds 4 -Transcode
```

Le fichier remplace la caméra comme source et est lu à vitesse réelle. Ce test ne prouve pas le fonctionnement d'une caméra ou d'un micro physiques.

Sans `-UseBundleAccount`, utiliser `-Email` et la demande de mot de passe sécurisée. Le mot de passe ne doit pas être collé dans un argument de ligne de commande.

## Regarder et activer le son

- Participant : `http://localhost:18790/live/`.
- PC contrôleur V3 : `http://localhost:19690/live/`.
- Banc indépendant V4 : `http://localhost:20690/live/`.

Ces adresses ne sont pas interchangeables. Le spectateur se connecte avec son compte, choisit un direct public/autorisé ou colle le `live_id` complet `l-…` affiché dans PowerShell, puis clique sur Lire le direct. Il peut être aussi producteur ou stockeur.

Le lecteur démarre **muet** pour permettre l'autoplay. Cliquer sur le haut-parleur du lecteur et vérifier le volume Windows. La CI contrôle que les fragments reconstruits contiennent une piste AAC et se décodent ; cela ne teste pas physiquement les haut-parleurs du jury.

Le lecteur vise maintenant un fragment de retard et tolère un écart plus grand avant correction. Ce réglage ne supprime ni le temps de finalisation, ni le temps de stockage sécurisé. Avec des fragments réels de 24 secondes, même un tampon d'un fragment reste élevé. Après mise à jour du web, recharger avec Ctrl+F5.

## Arrêter proprement et revoir le direct

1. Arrêter le streaming dans OBS.
2. Laisser la commande de publication traiter les derniers fragments finalisés et annoncer la fin.
3. Conserver l'identifiant `l-…` et les fichiers de mesures.
4. Choisir ce direct marqué terminé dans le lecteur.

Pendant un direct, le manifeste présente une fenêtre des six derniers fragments. **Après une fin enregistrée, le manifeste présente tous les fragments validés depuis le début**, avec un marqueur de fin. L'initialisation MP4 a une URL stable pendant la vie du direct.

La relecture dépend de la disponibilité des Edge qui détiennent les blocs et des droits du lecteur. Les fragments qui n'ont jamais été finalisés/publiés ne sont pas inventés. Si le PC s'éteint brutalement, le catalogue peut garder un direct « en cours » : un simple enregistrement de création ne garantit pas une source toujours vivante.

Le direct reste dans la liste des directs ; il n'est pas automatiquement transformé en une nouvelle VOD unique du catalogue. Les blocs et métadonnées servent à la relecture HLS. Aucun nettoyage automatique n'est associé au nombre d'heures du devis.

## Les mesures disponibles

Le producteur écrit un journal et `measurements.json` dans son volume de captures. Le script exporte ce dernier vers `.runtime/measurements/<capture>.json`, y compris après un échec quand le fichier existe.

| Champ producteur | Unité et portée |
|---|---|
| `source_bytes` | Octets du fragment avec son initialisation |
| `publish_request_ms` | Durée de l'appel local de publication ; inclut les étapes aval |
| `owner_publish_ms` | Préparation/signature/transfert/vérification/stockage cumulés dans l'agent ; pas une signature pure |
| `programme_age_at_commit_ms` | Âge du programme au commit, dépend des horloges |
| `pending_observed` | Nombre de fragments déjà produits en attente lors de l'observation |
| `source_sha256`, `video_id`, `live_id` | Rapprochement des mesures avec les médias et preuves |

Le navigateur propose **Exporter les mesures JSON** :

| Champ navigateur | Ce qu'il mesure |
|---|---|
| `first_display_ms` | Attente avant première image rendue après la demande de lecture |
| `download_ms` | Requête du fragment jusqu'au dernier octet ; inclut l'attente serveur |
| `ttfb_ms` | Requête jusqu'au premier octet |
| `download_ms - ttfb_ms` | Réception du corps après le premier octet, pas tous les trajets Edge/réseau |
| `server_timing` | Reconstruction/vérification serveur cumulées ; un cache chaud peut rendre la valeur très petite |
| `stall_count`, `stall_ms` | Blocages observés par le lecteur |
| `dropped_frames`, `rendered_frames` | Compteurs de rendu, pas pertes de paquets RTP |
| `programme_age_estimate_ms` | Retard estimé depuis l'horodatage FFmpeg, sous hypothèse d'horloges cohérentes |

Les durées se recouvrent : ne pas additionner la reconstruction au téléchargement qui l'inclut déjà. Conserver le même `live_id`, les séquences et la période pour comparer deux exports. Les mesures du navigateur peuvent inclure des reprises et du cache. Une fréquence de callback réduite sur une fenêtre en arrière-plan ne suffit pas à définir la cadence encodée ; utiliser aussi `ffprobe`.

**Non isolés actuellement :** temps CPU pur de signature IBS, temps pur de calcul SIS, temps pur de vérification, coût TLS de chiffrement/déchiffrement, transmission réseau seule entre chaque paire de machines et latence optique caméra-écran. SIS/IBS n'est pas un chiffrement vidéo. Ces valeurs demanderaient une instrumentation supplémentaire ; elles ne doivent pas être déduites d'une durée cumulée.

## Mesures de la démonstration du 20 octobre

Voir les [preuves expurgées](preuves/mesures-live-2b.json) et leur [export navigateur figé](preuves/live-2b-browser.json). Le fichier téléchargé a été remplacé pendant l'analyse ; le rapport publié ne mélange pas les versions et identifie sa source par SHA-256.

Pour la dernière version figée : 154 échantillons sur 163022 ms, six téléchargements des séquences 17, 18, 21, 22, 24, 25 ; statistiques Central rapprochées de ces mêmes séquences.

| Mesure | Valeur |
|---|---|
| Durée réelle de chaque fragment | 24000 ms |
| Publication propriétaire moyenne | 5152 ms |
| Première image | 1671 ms |
| Requête média moyenne jusqu'au dernier octet | 424 ms |
| Reconstruction/vérification moyenne, cache inclus | 402,285 ms |
| Réception du corps après premier octet | 11,333 ms en moyenne |
| Blocages | 2, total 5154 ms |
| Images perdues signalées | 0 |
| Retard de programme médian estimé | 97885,5 ms ; sauts d'horodatage observés |
| Source observée avec ffprobe | H.264, 728 × 410, 10 images/s |

Ces chiffres démontrent une lecture réelle avec un retard important ; ils ne prouvent pas un live à 4 secondes ni une qualité HD à 30 images/s. Le profil court avec transcodage est fourni comme réglage à valider sur le réseau et le CPU utilisés.
