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

La première version met en place :

```text
                         Control Plane
                              │
                             MQTT
                              │
                         Mosquitto
                              │
                              │
                ┌─────────────┴─────────────┐
                │                           │
                ▼                           ▼

          Edge Node 01                Edge Node 02
          Docker / Go                 Docker / Go

          Edge Agent                  Edge Agent
              │                           │
              ▼                           ▼
          stockage                    stockage
           local                       local