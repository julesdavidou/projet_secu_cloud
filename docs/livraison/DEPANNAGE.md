# Dépannage : reconnaître la couche qui bloque

Commencer par lire l'erreur utile, située souvent **avant** la dernière ligne PowerShell. La ligne « Installation refusée ; volumes conservés » ou « Publication interrompue » ne donne pas la cause.

## Le script n'est pas trouvé

On n'est pas dans le bon dossier. Sur le contrôleur, utiliser le chemin de `deploy/fleet/resume-controllers.ps1` depuis le dépôt. Dans un paquet participant, utiliser le dossier contenant `start.ps1`, `profile.json` et `compose.json`.

```powershell
Get-Location
Get-ChildItem start.ps1, profile.json, compose.json
```

Les scripts `deploy/fleet/windows.ps1` et `deploy/fleet/client-publish.ps1` sont des sources : l'export les copie en `start.ps1` et `publish.ps1` dans le paquet personnel. Ne pas lancer la source du lanceur comme si son dossier était un paquet.

## `dockerDesktopLinuxEngine` introuvable

Docker Desktop est arrêté, son moteur n'est pas prêt ou le mauvais mode/contexte est sélectionné.

1. Démarrer Docker Desktop et attendre le moteur.
2. Vérifier le mode Linux.
3. Lire `docker context ls` si nécessaire ; ne choisir `desktop-linux` que s'il existe et correspond à cette installation.
4. Relancer le script dans le même dossier avec le même quota.

```powershell
docker info --format '{{.OSType}}/{{.Architecture}}'
docker context ls
```

Ne pas réinitialiser Docker ou supprimer les volumes pour une erreur de moteur arrêté.

## Adresse `169.254…` ou Docker ne peut pas lier l'IP

Une adresse IPv4 automatique `169.254.*` ne remplace pas une adresse fournie par le DHCP du LAN. L'IP d'un ancien partage peut avoir disparu : Docker ne peut pas ouvrir un port sur une IP absente de Windows.

```powershell
Get-NetAdapter | Select-Object Name, Status
Get-NetIPAddress -AddressFamily IPv4 | Select-Object InterfaceAlias, IPAddress, AddressState
```

Vérifier le Wi-Fi réellement connecté, la configuration IP automatique et le partage de connexion. Si le PC est lui-même point d'accès, l'adresse utilisable est celle de sa carte de point d'accès, pas nécessairement celle de « Wi-Fi ». Ne pas choisir Tailscale, VirtualBox, WSL ou une adresse de lien local à la place du LAN commun.

Si l'IP est bien présente mais qu'un port est refusé, lire les listeners et plages réservées :

```powershell
Get-NetTCPConnection -LocalPort 19693 -ErrorAction SilentlyContinue
netsh interface ipv4 show excludedportrange protocol=tcp
```

Une vérification d'IP/port ne doit pas conduire à ouvrir tous les ports ou à désactiver globalement le pare-feu.

## Les ports répondent depuis Windows, mais l'Edge reste unhealthy

Un TCP réussi confirme uniquement l'ouverture du socket. Il ne valide ni le certificat, ni les ACL MQTT, ni les ACK métier. Surtout, la route **depuis le conteneur** peut être différente de celle de Windows.

```powershell
docker compose -f .\compose.json ps
docker compose -f .\compose.json logs --tail 40 edge broker portal
docker network ls -q | ForEach-Object {
    docker network inspect $_ --format '{{.Name}} {{range .IPAM.Config}}{{.Subnet}} {{end}}'
}
```

Dans la démo, le LAN `172.20.10.*` était englobé par un ancien réseau Docker `172.20.0.0/16`. Les bridges MQTT partaient vers ce bridge au lieu du Wi-Fi. Le conflit existait sur le contrôleur puis sur le PC de Jules ; le réseau du client actif n'était pas celui à supprimer.

Pour un réseau de test **identifié et inutilisé** :

```powershell
docker network inspect NOM_DU_RESEAU --format '{{json .Containers}}'
# Seulement si le résultat est {} et après identification du réseau :
docker network rm NOM_DU_RESEAU
```

Docker refuse normalement un réseau avec des endpoints actifs. Ne pas forcer une suppression ou déconnecter des applications tierces pour masquer le conflit. Choisir un autre LAN ou préparer une migration du réseau Docker actif si nécessaire.

Après correction des adresses/réseaux, un participant peut recréer ses processus puis relancer la découverte :

```powershell
docker compose -f .\compose.json up -d --force-recreate broker reconstructor edge portal owner
pwsh -NoProfile -File .\start.ps1
```

Les volumes restent conservés. Cette opération interrompt un producteur actif ; la faire entre deux essais. Omettre les services absents d'un paquet sans stockage.

## Broker « Healthy », bridge « Connection refused »

Le healthcheck du broker peut seulement constater son écoute locale. Lire le nom et l'adresse de la destination du bridge et les logs du broker central. Un Edge peut rejoindre son broker local tandis que ce broker n'atteint pas le hub.

Les messages TLS `unexpected eof` produits par une simple ouverture TCP locale ne prouvent pas tous un refus de certificat : les sondes de disponibilité peuvent ouvrir puis fermer sans handshake. Examiner les connexions du client concerné et son erreur de registration.

Ne pas changer de CA ou de mot de passe partagé au hasard. Les certificats de l'Edge, les topics, les ACL et le runtime doivent appartenir à la même installation.

## Découverte Windows arrêtée avec `WinError 5`

Docker/Windows peut garder brièvement un fichier publié ouvert. Le helper tente maintenant plusieurs remplacements atomiques et garde la boucle vivante lors d'une erreur d'E/S ; les baux restent bornés dans le temps.

```powershell
Get-Content .\.runtime\discovery-0.err.log -Tail 20
Get-Content .\.runtime\discovery-1.err.log -Tail 20
```

Une nouvelle IP d'Edge nécessite la reprise du helper natif. Une connexion MQTT fonctionnelle ne prouve pas à elle seule que les routes HTTPS et les workers de reconstruction sont joignables.

## Extension Python Cython bloquée par Windows

La démo a rencontré un blocage de `Cython/Plex/Scanners.cp312-win_amd64.pyd` dans un environnement de compilation pip. Le helper LAN utilise désormais `SKIP_CYTHON=1` et une installation Python pure de zeroconf. Ne pas désactiver App Control/Defender pour ce fichier. Une alerte seule ne permet pas de déclarer un fichier sain ou malveillant ; conserver son chemin et le diagnostic précis si elle persiste.

## Devis ou publication refusé avec HTTP 409

Le refus peut venir du budget, de la capacité, d'observations réseau périmées, d'une version ou d'une réservation. Le code HTTP seul ne tranche pas.

Cas prouvé dans la démo : un fragment de 5,2 Mo coûtait 116588 microcrédits, puis un fragment de 7,3 Mo coûtait 163727. Le plafond par défaut était 100000 pour 24 heures. Le même devis passait avec 1000000.

```powershell
pwsh -NoProfile -File .\publish.ps1 -UseBundleAccount -Public -BudgetMicrocredits 1000000
```

Il s'agit de crédits de démonstration et d'un plafond par fragment. Augmenter le budget ne corrige ni le débit ni les blocages de reconstruction. Pour la VOD, vérifier aussi le nombre d'heures du devis et l'étape « Publier » après `QUOTED`.

## HTTP 502 sur `/api/videos/signed/quote`

Ce message signale un service amont indisponible/refusé ou un problème de transport ; il ne signifie pas automatiquement que le fichier est trop gros. Un 502 de la démo est resté de cause exacte non établie. Une VOD synthétique de 8,07 Mo a obtenu un devis ensuite, sans que cela prouve que tous les 502 aient la même origine.

Conserver l'heure, l'identifiant `v-…`, le `trace_id` lorsqu'il existe et les logs des services. Relire un paquet déjà préparé évite de refaire les signatures sans raison. Ne pas supprimer ses clés ou son outbox.

## `Address already in use` dans FFmpeg

Un autre FFmpeg écoute déjà le port 1935 du même agent. Une nouvelle création `l-…` ne libère pas ce port.

1. Arrêter OBS et les commandes productrices précédentes.
2. Quand aucun live de cet agent n'est à conserver, redémarrer uniquement `owner` pour arrêter un processus restant.
3. Relancer une seule publication, attendre `CREATED`, puis démarrer OBS.

```powershell
docker compose -f .\compose.json restart owner
```

Cette reprise conserve le portefeuille et les captures, mais met fin aux traitements de cet agent. Ne pas la faire sur le producteur que l'on veut garder en direct.

## FFmpeg s'arrête avant le manifeste

Lire son log dans le volume, avec le chemin de capture affiché :

```powershell
docker compose -f .\compose.json exec -T owner sh -c 'tail -n 40 /data/captures/IDENTIFIANT_CAPTURE/ffmpeg.log'
```

Le chemin contient l'identifiant de capture, pas le `live_id`. Le message peut expliquer un port occupé, une entrée interrompue ou un codec invalide. Ne pas attribuer automatiquement cette erreur au stockage.

## `publisher backlog limit exceeded`

OBS produit plus vite que la chaîne ne publie. La limite de huit fragments en attente a été dépassée ; aucun fragment n'est volontairement supprimé pour faire semblant de tenir le direct.

Comparer la durée réelle des fragments à `publish_request_ms`, pas seulement la valeur demandée de `SegmentSeconds`. Rechercher : débit excessif, images clés trop espacées, CPU de signature/réencodage saturé, nombreux petits blocs, nœud lent, routage Docker erroné ou stockage distant indisponible.

Les blocs live sont maintenant de 1 Mio au maximum. `-Transcode` peut forcer les images clés. Un profil 4 secondes exige une publication moyenne plus rapide que quatre secondes. Revenir à un profil plus long peut aider à diagnostiquer mais augmente le retard. Augmenter la file sans améliorer le traitement ne résout pas le problème.

## Écran noir ou « terminé sans fragment »

`CREATED` ne prouve pas qu'OBS a envoyé du média. Vérifier successivement :

1. la source visible et les vumètres dans OBS ;
2. le FFmpeg et son premier manifeste ;
3. au moins une ligne `sequence` validée dans PowerShell ;
4. le bon identifiant `l-…`, le bon banc et les droits du lecteur ;
5. la disponibilité des Edge pour la reconstruction ;
6. le message d'erreur du lecteur et son export de mesures.

Le lecteur démarre muet : activer son haut-parleur pour entendre le son. Un HTTP 200 d'un fragment n'est pas un test de décodage complet ; la CI vérifie aussi les codecs, les octets reconstruits et les premières images.

## Collecter un diagnostic utile sans secrets

Dans un paquet :

```powershell
docker compose -f .\compose.json ps
docker compose -f .\compose.json logs --tail 40 edge broker portal owner
Get-ChildItem .\.runtime\measurements\*.json |
    Sort-Object LastWriteTime -Descending | Select-Object -First 3 |
    ForEach-Object { Get-Content $_.FullName -Raw | ConvertFrom-Json |
        Select-Object live_id, error, end_reason, wall_seconds }
```

Transmettre l'identifiant, l'erreur et les mesures du même essai. Ne pas transmettre `account.private.json`, une clé, le runtime complet ou un inventaire SSH contenant les mots de passe.
