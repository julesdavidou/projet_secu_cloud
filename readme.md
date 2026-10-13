# projet_secu_cloud

Projet de développement d'une architecture **Edge Computing distribuée** pour le stockage et la diffusion sécurisée de données vidéo.

L'objectif est de construire une plateforme capable de déployer plusieurs nœuds Edge sur des machines hétérogènes, notamment :

- PC classiques ;
- machines Linux ;
- Jetson Nano / ARM64 ;
- environnement Docker.

Les nœuds Edge doivent pouvoir :

- stocker des données localement ;
- communiquer avec le control plane via MQTT ;
- exposer leur état et leurs ressources ;
- recevoir et restituer des données ;
- être ajoutés ou supprimés dynamiquement ;
- isoler les données appartenant aux différents utilisateurs.

---

# Architecture actuelle

Le projet deploie uniquement l'Edge Agent. Il utilise le broker MQTT deja present dans l'infrastructure ; aucun broker n'est fourni par Docker Compose.

```text
                 Control Plane
                       |
                      MQTT
                       |
              Broker MQTT existant
                  /         \
                MQTT        MQTT
                 |            |
          Edge Agent 01  Edge Agent 02
                 |            |
          stockage local stockage local
```

# Demarrage

```sh
cp .env.example .env
docker compose up -d --build
docker compose ps
docker compose logs -f edge-agent
```

Renseigner `MQTT_BROKER` dans `.env` avec l'adresse du broker existant, accessible depuis le conteneur. Exemple a remplacer par l'adresse reelle :

```dotenv
MQTT_BROKER=tcp://ADRESSE_DU_BROKER:1883
```

Si `MQTT_BROKER` est vide, MQTT est desactive. L'API HTTP, le monitoring et le stockage restent disponibles. Apres modification de `.env`, appliquer la configuration avec `docker compose up -d`.

L'API est accessible sur `http://127.0.0.1:8080` depuis la machine qui execute Docker :

| Methode et route | Fonction |
| --- | --- |
| `GET /health` | Disponibilite de l'API |
| `GET /status` | Identite et ressources du noeud |
| `PUT /v1/storage/{tenant}/{object}` | Envoi ou remplacement d'un fichier |
| `GET /v1/storage/{tenant}/{object}` | Recuperation d'un fichier |
| `DELETE /v1/storage/{tenant}/{object}` | Suppression d'un fichier |

Les fichiers sont conserves dans le volume `edge-data`, monte dans `/data`. La separation par repertoire ne remplace pas l'authentification et les droits d'acces des clients.

Pour arreter l'agent en conservant son conteneur et ses donnees :

```sh
docker compose stop
```

Pour retirer les conteneurs et le reseau en conservant le volume de stockage :

```sh
docker compose down
```
