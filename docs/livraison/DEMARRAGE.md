# Démarrer et raccorder les machines

Toutes les commandes de la partie contrôleur sont à lancer **à la racine du dépôt**, là où se trouvent `deploy`, `cmd`, `frontend` et `go.mod`. Le dossier `outputs/correctif-live` d'une ancienne session n'est pas cette racine.

Les adresses `192.168.50.10` et `192.168.50.11` ci-dessous sont des exemples. Il faut les remplacer par les adresses réelles. Ne pas recopier une ancienne IP de la démo.

## 1. Prérequis

Sur le PC contrôleur et les participants Windows :

- Docker Desktop démarré, moteur Linux ; bundles Windows prévus pour AMD64 ;
- PowerShell 7 : commande `pwsh` ;
- Python 3.10 ou plus récent : `py -3` ou `python` ;
- assez d'espace disque pour les images, les volumes et les captures ;
- même réseau local, sans isolation des clients et sans chevauchement avec les réseaux Docker.

Go et Node sont nécessaires pour les tests locaux hors Docker, pas pour importer les images d'un paquet.

```powershell
docker info --format '{{.OSType}}/{{.Architecture}}'
pwsh --version
py -3 --version
```

Pour identifier le réseau, consulter `ipconfig` et les plages Docker :

```powershell
Get-NetIPAddress -AddressFamily IPv4 | Select-Object InterfaceAlias, IPAddress, AddressState
docker network ls -q | ForEach-Object {
    docker network inspect $_ --format '{{.Name}} {{range .IPAM.Config}}{{.Subnet}} {{end}}'
}
```

Une adresse `169.254.*` n'est pas une adresse de laboratoire à utiliser. Un partage `172.20.10.*` entre en conflit avec un bridge Docker `172.20.0.0/16`, même si un test TCP lancé depuis Windows réussit. Voir le dépannage avant de supprimer ou reconfigurer un réseau utilisé.

## 2. Installation neuve des contrôleurs

**Cette section est pour une installation neuve.** Si le runtime existe déjà avec les comptes et les Edge de la démo, passer à la section « Reprendre une installation ».

```powershell
git clone https://github.com/julesdavidou/projet_secu_cloud.git
cd projet_secu_cloud
git switch main
git pull --ff-only
pwsh -NoProfile -File .\deploy\secure-v3\start.ps1
```

La première compilation télécharge les images de base et les dépendances. Docker doit avoir accès aux registres. Ne pas confondre une compilation longue avec une signature de vidéo longue.

Ce démarrage construit les images et crée le runtime, les certificats et deux Edge locaux A1/B1 de 256 Mio chacun. Il démarre Central, les deux zones, les autorités, Java, le web, les brokers et les agents propriétaires de test. A1/B1 sont des processus réels sur le même PC : ce ne sont pas deux cartes physiques.

| Interface locale du contrôleur | Adresse |
|---|---|
| Frontend | `http://localhost:19690/` |
| Vue d'orchestration | `http://localhost:19690/integration/` |
| Lecteur de directs | `http://localhost:19690/live/` |
| Agent propriétaire de test A | `http://localhost:19591/` |
| Agent propriétaire de test B | `http://localhost:19592/` |

Sur un banc neuf réservé à la validation, on peut ensuite lancer :

```powershell
pwsh -NoProfile -File .\deploy\secure-v3\smoke.ps1
```

Cela crée deux comptes de fixture et leurs stockages isolés, publie des vidéos et conserve ses preuves dans `deploy/secure-v3/results`. Les comptes publics de test sont décrits dans les anciens guides V3 ; ils ne doivent pas remplacer les comptes personnels des participants.

Pour exposer les interfaces mTLS sur le réseau local et lancer la découverte :

```powershell
$ipControleur = '192.168.50.10'
docker tag caribou/secure-v3-control:dev caribou/windows-control:dev
pwsh -NoProfile -File .\deploy\fleet\resume-controllers.ps1 -BindAddress $ipControleur
```

La commande attend que les services soient prêts. Lire le message de réussite final, pas seulement « Started ».

## Reprendre une installation

Ne pas remplacer le dossier `deploy/secure-v3/.runtime`, ne pas relancer un provisionnement à partir de l'exemple à deux nœuds si des nœuds ont été ajoutés, et ne pas supprimer les volumes.

```powershell
git pull --ff-only
$ipControleur = '192.168.50.10'
pwsh -NoProfile -File .\deploy\fleet\resume-controllers.ps1 -BindAddress $ipControleur
```

Cette reprise conserve les admissions. Elle utilise les images locales présentes ; **elle ne reconstruit pas le code à elle seule**. Pour mettre à jour les images d'une installation existante, reconstruire les services avec les overlays du runtime :

```powershell
$dc = @('compose', '--project-directory', 'deploy/secure-v3',
    '-f', 'deploy/secure-v3/compose.yaml',
    '-f', 'deploy/secure-v3/.runtime/compose.lan.json',
    '-f', 'deploy/secure-v3/.runtime/client.storage.json',
    '-f', 'deploy/secure-v3/.runtime/client.gateway.json')
docker @dc build
docker tag caribou/secure-v3-control:dev caribou/windows-control:dev
# À faire en dehors d'un live : changement de processus applicatifs.
docker @dc up -d --wait --wait-timeout 240
```

Les fichiers d'overlay ci-dessus sont créés par la préparation/reprise des contrôleurs. Une passerelle utilisant le tag `caribou/windows-control:dev` doit être reconstruite ou retaguée depuis l'image contrôle mise à jour ; `resume-controllers.ps1` permet aussi de préciser `-ControlImage caribou/secure-v3-control:dev`.

## 3. Créer un compte et préparer son stockage

Un compte Java et une identité Edge sont deux choses différentes. `demo-pc` identifie un paquet ; `DEMO1` identifie un Edge ; l'`owner_id` de forme `client-…` vient de Java.

Les exemples conservent les sorties privées **hors du dépôt** :

```powershell
$prive = Join-Path (Split-Path (Get-Location) -Parent) 'cariboutv-prive'
$comptes = Join-Path $prive 'comptes'
py -3 .\deploy\fleet\prepare-demo-accounts.py --clients demo-pc --output $comptes
$compte = Get-Content (Join-Path $comptes 'demo-pc.json') -Raw | ConvertFrom-Json
```

Le mot de passe aléatoire est écrit dans ce fichier privé et n'est pas affiché. Le code Java existant crée un compte au premier login si l'adresse n'existe pas ; ce comportement est conservé. Ne pas copier le mot de passe d'un collègue et ne pas changer de propriétaire dans un agent déjà enrôlé.

Chaque propriétaire qui publie doit disposer d'un espace sur les Edge susceptibles de recevoir ses blocs. Pour le nouveau compte sur A1/B1 :

```powershell
foreach ($node in @('A1', 'B1')) {
    pwsh -NoProfile -File .\deploy\secure-v3\provision-tenant.ps1 -Owner $compte.owner_id -Node $node
}
```

C'est une commande d'administration ponctuelle : elle accède au moteur Docker. Le frontend n'a pas cet accès. Répéter pour les autres propriétaires et Edge admis. Aucun espace existant n'est supprimé.

## 4. Admettre un nouveau PC de stockage

Sauter cette section pour un simple spectateur/producteur sans contribution de stockage.

Le contrôleur doit autoriser le nœud avant l'export. Exemple pour `DEMO1`, zone A, plafond 1 Gio :

```powershell
$admission = @('--runtime', 'deploy/secure-v3/.runtime',
    '--compose', 'deploy/secure-v3/compose.yaml',
    '--override', 'deploy/secure-v3/.runtime/compose.lan.json',
    '--override', 'deploy/secure-v3/.runtime/client.gateway.json',
    '--control-image', 'caribou/secure-v3-control:dev',
    '--node', 'DEMO1', '--zone', 'A', '--maximum-gib', '1')
py -3 .\deploy\fleet\admit-storage.py @admission
# Après examen du plan, appliquer l'admission administrative :
py -3 .\deploy\fleet\admit-storage.py @admission --apply
```

L'application recharge certains contrôleurs et brokers. Elle ne se connecte pas au PC distant. Une admission existante n'est pas remplacée : réutiliser son identité et son paquet plutôt que tenter de l'admettre une seconde fois.

## 5. Exporter le paquet personnel Windows

Préparer la liste de **tous les propriétaires autorisés à utiliser ce stockage**, pas seulement celle du producteur qui tient le PC. Les fixtures créées plus haut peuvent être ajoutées depuis leurs résultats :

```powershell
$proprietaires = @($compte.owner_id)
foreach ($slot in @('a', 'b')) {
    $f = "deploy/secure-v3/results/owner-$slot.json"
    if (Test-Path $f) {
        $proprietaires += (Get-Content $f -Raw | ConvertFrom-Json).owner
    }
}
$proprietaires = @($proprietaires | Select-Object -Unique)
# Ajouter ici les owner_id des autres participants déjà autorisés.
$paquet = Join-Path $prive 'demo-pc'
pwsh -NoProfile -File .\deploy\fleet\prepare-client.ps1 `
    -ClientID demo-pc -ControllerAddress $ipControleur `
    -Roles 'viewer,publisher,storage' -NodeID DEMO1 `
    -Owners $proprietaires -AccountFile (Join-Path $comptes 'demo-pc.json') `
    -OutputDirectory $paquet -IncludeImages
```

L'export refuse d'écraser un dossier existant. `-IncludeImages` inclut les images Linux AMD64 figées ; le paquet peut être volumineux. Cela n'inclut pas Docker Desktop, Python, PowerShell ni toutes les dépendances LAN du premier lancement. Transférer **son paquet à son destinataire**, pas l'ensemble des paquets du groupe.

Pour un participant sans stockage : utiliser `-Roles 'viewer,publisher'`, omettre `-NodeID` et conserver son compte/son agent personnel. Pour un simple spectateur, `-Roles viewer` suffit ; son compte Java reste nécessaire pour le contenu authentifié.

## Sur le PC participant

Démarrer Docker Desktop en mode Linux. Ouvrir PowerShell dans le dossier contenant `start.ps1`, `profile.json` et `compose.json` :

```powershell
pwsh -NoProfile -File .\start.ps1
```

Au premier lancement, `0` désactive la contribution de stockage ; une valeur positive sous le plafond active l'Edge et ses espaces. `0,5` et `0.5` sont acceptés. L'installation mémorise ce choix ; changer ensuite quota ou rôle demande une migration explicite, pas la suppression de son état.

| Usage du participant | Adresse |
|---|---|
| Catalogue et connexion | `http://localhost:18790/` |
| Lecteur live | `http://localhost:18790/live/` |
| Signature/publication personnelle | `http://localhost:18791/` |
| Entrée RTMP locale du producteur | `rtmp://127.0.0.1:22935/live`, clé `stream` |

Le compte du paquet de démonstration est dans `account.private.json`. Le script n'en affiche pas le mot de passe. Le portail et l'agent propriétaire ont chacun leur session : se connecter sur les deux si l'interface le demande.

Le helper lance deux processus Python natifs pour l'annonce LAN et le maillage. Ils ne sont pas des lecteurs vidéo. Les logs sont dans `.runtime/discovery-*.log` et `.runtime/discovery-*.err.log`. Après redémarrage de Windows, relancer `start.ps1`.

## Changer de Wi-Fi ou d'adresse du contrôleur

Le certificat représente une identité stable, pas une IP. Conserver les certificats et les volumes. Le contrôleur doit republier ses ports sur son adresse **réellement présente** :

```powershell
pwsh -NoProfile -File .\deploy\fleet\resume-controllers.ps1 -BindAddress NOUVELLE_IP_PRIVEE
```

Puis dans chaque paquet Windows, remplacer son ancienne adresse de bootstrap :

```powershell
$ancienne = (Get-Content .\profile.json -Raw | ConvertFrom-Json).controller_ip
$nouvelle = '192.168.50.10' # Adresse actuelle du contrôleur, à remplacer.
foreach ($f in @('profile.json', 'compose.json')) {
    $p = (Resolve-Path $f).Path
    $texte = [IO.File]::ReadAllText($p).Replace($ancienne, $nouvelle)
    [IO.File]::WriteAllText($p, $texte, [Text.UTF8Encoding]::new($false))
}
docker compose -f .\compose.json up -d --force-recreate broker reconstructor edge portal owner
pwsh -NoProfile -File .\start.ps1
```

La recréation conserve les volumes. Pour les paquets sans rôle stockage, omettre les services `broker reconstructor edge` absents du Compose. Vérifier également les conflits de sous-réseaux sur **chaque** PC. La découverte met ensuite à jour l'IP des Edge ; elle ne peut pas joindre un contrôleur dont le bootstrap pointe encore vers l'ancien réseau.

## Raspberry Pi et Jetson

Le rôle stockage sécurisé complet nécessite Linux **ARM64**, Docker/Compose, un helper LAN natif et l'administration des espaces propriétaires. Le binaire Edge Go existe aussi en ARMv7 ; cela ne rend pas le worker cryptographique complet disponible en ARMv7.

Le matériel physique n'a pas été validé dans cette livraison. Un Jetson avec ancien OS ou un Raspberry Pi 32 bits doit être vérifié avant de choisir l'image. Ne pas confondre un test QEMU avec ce contrôle.

Après admission d'une identité nouvelle, l'administrateur peut préparer les images ARM64 et exporter les seules clés de cette identité :

```powershell
docker buildx build --platform linux/arm64 --load -f deploy/lab/Dockerfile -t caribou/secure-v3-control:lab-arm64 .
docker buildx build --platform linux/arm64 --load -f deploy/secure-v3/Dockerfile.security -t caribou/secure-v3-security:lab-arm64 .
py -3 deploy/secure-v3/export_edge.py --node PI1 --controller-ip $ipControleur --output ../paquets-prives/PI1 --arch arm64
```

`PI1` doit déjà être admis ; l'export n'admet pas un nœud et n'embarque pas automatiquement les images. Transférer le paquet et les images (registre ou `docker save --output` puis `docker load --input`) sur la carte. Le Compose exporté référence les tags `lab-arm64` ci-dessus et Mosquitto.

Sur la carte : démarrer le broker/worker, provisionner chaque propriétaire avec `provision-tenant.ps1` dans PowerShell 7 en précisant le projet `caribou-edge-pi1`, le runtime du paquet et les deux images `lab-arm64`, puis démarrer le Compose complet. Lancer `lan.py advertise` et `lan.py mesh` sur l'hôte avec l'interface LAN réelle et les certificats de l'Edge. Le fichier `export_edge.py` et le [guide ARM64 existant](../conformity-v3/LANCEMENT-ET-ISOLATION.md) doivent être consultés avec l'OS effectivement installé ; ne pas présenter cette séquence comme un installateur matériel validé sans intervention.

## Conserver et sauvegarder

Arrêter un service avec `docker compose stop` conserve les données. `docker compose down` sans `-v` conserve les volumes nommés mais retire les conteneurs et le réseau. Ne pas lancer de nettoyage global Docker.

Pour retrouver la même installation, sauvegarder de manière cohérente, services arrêtés :

- le runtime contrôleur, sa CA, les clés de zones et le catalogue ;
- les volumes Central, Java, web et brokers ;
- chaque volume propriétaire/Edge et les ledgers des Edge ;
- le portefeuille de chaque agent propriétaire, ses captures et son outbox ;
- les paquets/configurations nécessaires au bootstrap.

Ces sauvegardes contiennent des secrets. Les remettre dans un dépôt public n'est pas une sauvegarde acceptable. Sans les Edge contenant les blocs, le catalogue seul ne reconstitue pas les vidéos.
