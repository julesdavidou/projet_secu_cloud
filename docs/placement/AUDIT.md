# Audit préalable — 13 octobre 2026

Dépôt privé réellement cloné : julesdavidou/projet_secu_cloud.
HEAD main initial : `80a8d45f73569f86b14bf730be5eecd43880d157`, checkout propre.
Branche distante `containers` : `46da1d8c64de523b7beb9099d81cc75176d1a866`.
Les deux branches sont des racines indépendantes (`git merge-base` ne trouve aucun ancêtre).
Archive fournie : mêmes contenus que main (normalisation CRLF uniquement).
Authentification Git : compte CyrilN68, pull/push autorisés, admin/maintain non.
API GitHub authentifiée : aucune PR (ouverte ou fermée) au moment de l'audit.
Les réponses 404 non authentifiées provenaient du caractère privé du dépôt.

| Existant | Réutilisable | Absent / frontière | À préserver |
|---|---|---|---|
| [Edge Agent](../../cmd/edge-agent/main.go), HTTP :8080 | conventions Go, identités | pas de transfert d'extent ni auth HTTP | fichier entier |
| [Stockage](../../internal/storage/storage.go), tests | stockage atomique de fichier par rename | aucun commit de transfert distribué | module entier |
| [Monitoring](../../internal/monitor/monitor.go) | identité, horodatage | capacité projet absente ; disk.Usage physique incompatible | module entier |
| [MQTT](../../internal/mqttclient/client.go), [protocole](../../pkg/protocol/messages.go) | topics edge/{id}/status, heartbeat, commands/# | MQTT 3 Paho, pas de placement ni RPC métier | modules entiers |
| [Compose](../../compose.yaml), [Mosquitto au HEAD initial](https://github.com/julesdavidou/projet_secu_cloud/blob/80a8d45f73569f86b14bf730be5eecd43880d157/deploy/mosquitto/mosquitto.conf) | schéma non-root, réseau control, volumes | broker anonyme local, pas de catalogue | configuration entière |
| [CI](../../.github/workflows/ci.yml) | gofmt, vet, race, multiarch, intégration | pas de tests placement | workflow entier |
| [serveur-zone de Dan](https://github.com/julesdavidou/projet_secu_cloud/tree/46da1d8c64de523b7beb9099d81cc75176d1a866/serveur-zone) | future importation du paquet pur | allocator.go vide, mqtt.go commenté, DB locale nodes seulement | branche entière |

Langages : Go, YAML, Dockerfile. Aucun web, SIS/JPBC, codec, RTP/WebRTC,
Central, manifest ou scheduler existant. README main se termine dans un bloc
ASCII non fermé. Go 1.27 déclaré ; CI actions v7 / Docker actions v4/v7.
`containers/serveur-zone/app/nodes.go:getNode` sélectionne six colonnes mais
n'en scanne que quatre : défaut existant signalé, pas corrigé hors périmètre.

Plan précis : `pkg/placement/*.go` domaine pur sans nouvelle dépendance racine ;
`lab/placement/` module Go indépendant (MQTT 5, catalogue SQLite de référence,
processus central/zone/mock/CLI, Compose dédié) ; `docs/placement/` contrats,
questions et preuves. L'intégration au serveur de Dan restera contractuelle :
aucun rebase/merge des deux racines, aucune migration de sa base SQLite.
Le laboratoire ne commande jamais les vrais Edge Agents.

Outillage initial : Git présent ; Go et Docker absents du PATH. Une distribution
WSL docker-desktop est déclarée ; son accessibilité et le daemon restent à vérifier.
Go portable sera installé uniquement sous work/ pour les tests.

## Complément — archive containers et instruction de raccordement allocator.go
La seconde archive correspond au nouveau commit distant
`3a56360cd3eb1722dd800dccddf09247c4f3378f` (Dan, ajout .env et début MQTT).
HEAD main reste inchangé. L'utilisateur demande explicitement de placer son
intégration dans `serveur-zone/app/allocator.go` et d'utiliser cette branche.
Le répertoire serveur-zone est importé de cette version, sans fusion de ses
suppressions implicites des fichiers main (histoires sans ancêtre commun).
Les originaux des points de raccordement sont conservés sous upstream/.

Défauts observés : mqtt.go contient @TODO syntaxiquement invalide, imports log/json
absents, alias mqtt absent, Command/handleAllocate/handleDeallocate absents.
main exige APP_PORT ; le compose de la branche ne le configure pas et ne lance
aucun broker. Le .env est ignoré et absent de l'archive ; aucun secret importé.
Le branchement allocator nécessite des réparations de compilation ciblées et
un mode MQTT5 séparé, explicitement activé pour le lab. Pas de remplacement du
registre SQLite de Dan. storage_max/storage_used ne deviennent pas silencieusement
une capacité projet. Le bug getNode reste signalé et hors de cette modification.

Contrôle de formatage global : le checkout Windows convertit les six fichiers
Go historiques en CRLF ; gofmt -l . les signale pour cette seule raison.
Vérification octet à octet des blobs du HEAD via gofmt : tous sont déjà canoniques
PASS. Aucun défaut de formatage préexistant de la CI Linux n'est donc déduit
à partir du checkout Windows. Les fichiers des collègues restent inchangés.
Les copies upstream de raccordement normalisent uniquement les espaces de fin
de ligne et l'indentation du texte ; la version Git distante reste la référence.

## Contrôle de collision avant publication
main a avancé pendant la tâche à `7860a4aac90c085e2c5bb281cf2aca6105d95532`
(the-sayreth, 13 octobre 17:59 Europe/Paris). Ce commit rend MQTT optionnel dans
l'Edge Agent, retire le broker/config Mosquitto du Compose racine et adapte
README/.env/CI. Son diff intégral a été lu : aucune collision avec nos ajouts.
La branche de travail a été rebasée sur ce HEAD, sans modifier main ni rétablir
son broker supprimé. Le banc conserve uniquement son broker local isolé.
Les modules, dépendances, Compose et workflow Edge sont identiques à ce nouveau
main. Les suites Go ont été relancées après le rebase. containers reste 3a56360.
