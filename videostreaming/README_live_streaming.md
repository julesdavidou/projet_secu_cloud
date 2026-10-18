# Streaming live signé — projet_secu_cloud

Extension du [pipeline vidéo](./README_video_pipeline.md) pour diffuser un
flux **OBS en direct** à travers les mêmes Edge Agents, avec la même
signature SIS/IBS que la VOD, et sans HTTP/3 ni modification de l'Edge Agent.

## Différence avec la VOD

La VOD segmente un fichier une fois, signe tout, distribue tout, puis sert
un manifest figé. Le live fait la même chose **en continu, segment par
segment**, au fur et à mesure qu'OBS pousse le flux :

```
OBS ──RTMP──► live_ingest.sh (ffmpeg, écoute RTMP + segmentation HLS live)
                    │
                    ▼  nouveau segment détecté dans index.m3u8
            live_watcher.py
                    │
      ┌─────────────┼──────────────────────┐
      │ blocs (blocks.py)                   │
      │ signature SIS/IBS (clés générées    │
      │   UNE FOIS au démarrage, réutilisées│
      │   pour tous les segments du live)   │
      │ placement (placement.py)            │
      │ envoi -> Edge Agent choisi          │
      └─────────────┬──────────────────────┘
                    ▼
       blocks_out/placement.json (réécrit de façon
       atomique à chaque segment : "live": true,
       liste de segments qui grandit, signatures
       par segment)
                    │
                    ▼
            vod_gateway.py
      (génère le manifest à la volée depuis
       placement.json, pas de cache — donc
       toujours à jour pendant le live)
                    │
                    ▼
          Lecteur (hls.js, mode live)
```

Le lecteur ne parle qu'à `vod_gateway.py`, exactement comme en VOD — aucune
différence côté lecture, le gateway détecte lui-même le mode live via le
champ `"live": true` de `placement.json`.

## Scripts spécifiques au live

| Fichier | Rôle |
| --- | --- |
| `live_ingest.sh` | Lance `ffmpeg` en écoute RTMP (port 1935) + segmentation HLS fMP4 continue (4s/segment) |
| `live_watcher.py` | Surveille le dossier HLS, détecte chaque segment **finalisé** (apparu dans `index.m3u8`, jamais un fichier en cours d'écriture), le découpe en blocs, le signe, le place, l'envoie à l'Edge Agent choisi, met à jour `placement.json` |
| `crypto_serialize.py` | Sérialisation JSON des points elliptiques py_ecc (`PK_s`, `w2` de `sigma`) — `V` et `w1` sont déjà des entiers Python natifs |
| `live_verify.py` | Vérifie un segment en récupérant ses blocs **depuis le réseau** (Edge Agent réel), pas depuis le disque local — reflète ce que le lecteur recevra vraiment |
| `vod_gateway.py` (mis à jour) | Génère le manifest à la volée en mode live (pas de cache, pas de `#EXT-X-ENDLIST` tant que le stream continue) ; la route segment est inchangée, partagée avec la VOD |

## Installation

Mêmes dépendances que la VOD, plus `ffmpeg` compilé avec le support RTMP
(présent par défaut dans les builds standards) :

```bash
pip install fastapi uvicorn httpx paho-mqtt py_ecc numpy
```

## Démarrage, dans l'ordre

### 1. Lancer l'ingestion RTMP

```bash
chmod +x live_ingest.sh
./live_ingest.sh ./live_out 1935
```

Le script affiche la configuration OBS à utiliser.

### 2. Configurer OBS

Paramètres → Stream → Service = **Personnalisé...**
- Serveur : `rtmp://<IP_de_cette_machine>:1935/live`
- Clé de stream : `stream` (valeur libre, non vérifiée ici)

Démarrer le streaming dans OBS.

### 3. Lancer le watcher (signature + distribution)

```bash
py live_watcher.py --hls-dir ./live_out --out-dir ./blocks_out \
    --video-id live01 --owner-id quentin \
    --nodes http://edge-agent-1:8080,http://edge-agent-2:8080
```

Au démarrage, le watcher génère les clés IBS et la matrice SIS (étape la
plus coûteuse, faite une seule fois), puis traite chaque nouveau segment au
fil de l'eau.

### 4. Lancer la passerelle de lecture

```bash
export OWNER_ID=quentin
export PLACEMENT_FILE=./blocks_out/placement.json
uvicorn vod_gateway:app --host 0.0.0.0 --port 8000
```

### 5. Lire

```
http://<passerelle>:8000/v1/videos/live01/index.m3u8
```

Dans `hls.js`, le lecteur repolle automatiquement ce manifest (pas de
`#EXT-X-ENDLIST`) et affiche les nouveaux segments au fur et à mesure.

### 6. Vérifier un segment (optionnel, test)

```bash
py live_verify.py --placement ./blocks_out/placement.json --segment seg_00007.m4s
```

Va chercher les blocs réellement stockés sur l'Edge Agent et vérifie
authenticité + intégrité — contrairement à une vérification en mémoire
locale, ça teste ce que le lecteur recevra vraiment.

## Signature : ce qui est généré une fois vs par segment

| Élément | Fréquence | Pourquoi |
| --- | --- | --- |
| `paramA` (graine SIS) + matrice `A` | une fois, au démarrage du watcher | coûteux à générer, doit être cohérent pour toute la session |
| Clés IBS (`t_s`, `PK_s`, `S_w`) | une fois, au démarrage du watcher | le vérifieur fait confiance à `owner_id`/`PK_s` pour toute la session, pas segment par segment |
| `V` (empreinte) + `sigma` | **par segment** | chaque segment est vérifiable indépendamment dès son arrivée, sans attendre la fin du stream (qui n'a pas de fin connue à l'avance en live) |

## Latence

Chemin critique par segment : durée du segment (4s par défaut) + encodage
ffmpeg + découpage en blocs + **calcul de signature** (`build_data_matrix` +
`compute_V`, proportionnel au nombre de blocs du segment) + envoi à l'Edge
Agent + prochain polling du manifest côté lecteur. Pour un live "quasi
temps réel" de test, comptez 10-15s de retard total. Si la signature
devient le goulot, augmenter `BLOCK_SIZE` (moins de blocs par segment,
donc moins de vecteurs à calculer) est le premier levier à essayer.

## Limitations connues

- Pas de retransmission ni de récupération automatique si un segment ne
  peut pas être envoyé à l'Edge Agent choisi (`send_one_segment` lève une
  exception et arrête le watcher) — à durcir (retry, fallback sur un autre
  nœud) avant un usage prolongé.
- Pas de fenêtre glissante : `placement.json` garde tous les segments
  depuis le début du live, sans purge. Pour un live long, prévoir soit une
  purge des segments anciens côté manifest (DVR limité), soit un archivage
  séparé.
- Topics MQTT toujours provisoires (voir [README_video_pipeline.md](./README_video_pipeline.md)).
