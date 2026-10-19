# Pipeline vidéo sécurisé — projet_secu_cloud

Ce module s'appuie sur l'Edge Agent du projet (voir README principal) pour
segmenter, sécuriser, répartir et diffuser une vidéo à travers plusieurs
nœuds Edge hétérogènes, sans passer par un serveur HTTP/3 ni modifier
l'Edge Agent.

## Vue d'ensemble

```
UPLOAD (sur la machine de l'owner)
  ffmpeg (segmentation HLS/fMP4)
        │
        ▼
  create_blocks.py          -> découpe chaque segment en blocs de taille fixe
        │
        ▼
  SignBlocks.py              -> empreinte SIS + signature IBS sur tous les blocs
        │
        ▼
  placement.py (stratégie)   -> décide quel Edge Agent reçoit quel segment
        │
        ▼
  sendBlocks.py              -> PUT /v1/storage/{owner_id}/{objet} vers l'Edge Agent
        │                        choisi (tous les blocs d'un même segment
        │                        ensemble), persiste placement.json
        ▼
  blocks_dir/placement.json  -> {video_id, owner_id, placement, manifest}


LECTURE (depuis n'importe quelle machine)
  Lecteur (hls.js, HTTP classique)
        │
        ▼
  vod_gateway.py              -> GET /v1/videos/{id}/index.m3u8
                                  GET /v1/videos/{id}/{segment}
        │   lit placement.json, sait quel Edge Agent a quel segment
        ▼
  GET /v1/storage/{owner}/{objet}  -> récupère les blocs chez l'Edge Agent
        │
        ▼
  reassemble_segment()        -> reconstruction en mémoire, servie au lecteur
```

Le lecteur ne parle **qu'à la passerelle** (`vod_gateway.py`), jamais
directement aux Edge Agents.Tout passe en HTTP/TCP classique, comme un CDN/VOD standard.

Une variante existe aussi en **UDP brut** (sans HTTP du tout) pour tester la
lecture avec pertes de paquets simulées, voir la section dédiée plus bas.

## Scripts

| Fichier | Rôle |
| --- | --- |
| `blocks.py` | Découpe/reconstruction générique en blocs de taille fixe (`BlockEnvelope`, `segment_to_blocks`, `reassemble_segment`) |
| `create_blocks.py` | Découpe tous les segments d'un dossier HLS en blocs, écrit `block_XXXX.bin` + `.json` par segment |
| `reform_segments.py` | Reconstruction **locale** des segments à partir d'un dossier de blocs (debug/tests) |
| `SISIntegrity.py` | Empreinte d'intégrité par algèbre modulaire (problème SIS) — `generate_matrix_A`, `compute_V`, `verify_block` |
| `IBSSignature.py` | Signature identity-based (pairing BN254/py_ecc) sur l'empreinte SIS — pas de PKI, l'identité sert de clé publique |
| `security_demo.py` | Démo bout en bout : blocs -> signature -> vérification avec localisation du bloc perdu/altéré |
| `placement.py` | Choix modulaire du nœud par segment (stratégies `round_robin`, `hash`, extensible) |
| `sendBlocks.py` | Envoie les blocs vers les Edge Agents via `PUT /v1/storage/{tenant}/{objet}`, persiste `placement.json` (+ publication MQTT optionnelle) |
| `vod_gateway.py` | Passerelle de lecture : sert manifest + segments reconstitués à la volée, HTTP classique |
| `udp_vod.py` / `udp_send_from_storage.py` / `udp_player.html` | Variante expérimentale en UDP brut (sans retransmission), pour observer l'effet des pertes réseau |

## Installation

```bash
pip install fastapi uvicorn httpx paho-mqtt py_ecc numpy
```

`ffmpeg` doit être installé et dans le `PATH` pour la segmentation initiale.

## Utilisation

### 1. Segmenter la vidéo (ffmpeg, hors de ce dépôt ou via votre script de segmentation)

```bash
ffmpeg -i film.mkv -c:v libx264 -c:a aac -g 96 -keyint_min 96 \
  -f hls -hls_time 4 -hls_segment_type fmp4 -hls_playlist_type vod \
  -hls_segment_filename "hls_out/seg_%05d.m4s" hls_out/index.m3u8
```

### 2. Découper en blocs

```bash
py create_blocks.py --data-dir ./hls_out --out-dir ./blocks_out --block-size 1024 --video-id <video_id>
```

### 3. Signer (intégrité + authenticité)

```python
from Verifyblocs import load_all_blocks
from SignBlocks import sign_blocks  # à adapter sur le modèle de security_demo.sign_blocks

blocks = load_all_blocks(Path("./blocks_out"))
package = sign_blocks(blocks, owner_id="quentin")
```

### 4. Envoyer vers les Edge Agents

```python
from pathlib import Path
from sendBlocks import send_blocks_distributed

send_blocks_distributed(
    blocks_dir=Path("./blocks_out"),
    node_urls=["http://edge-agent-1:8080", "http://edge-agent-2:8080"],
    video_id="<video_id>",
    owner_id="quentin",
    strategy="round_robin",
    manifest_path=Path("./hls_out/index.m3u8"),
)
```

### 5. Lancer la passerelle de lecture

```bash
export OWNER_ID=quentin
export PLACEMENT_FILE=./blocks_out/placement.json
uvicorn vod_gateway:app --host 0.0.0.0 --port 8000
```

### 6. Lire

N'importe quel lecteur compatible HLS (`hls.js`, `ffplay`, VLC) pointé sur :

```
http://<passerelle>:8000/v1/videos/<video_id>/index.m3u8
```

## Format des blocs

Chaque bloc est une paire `block_XXXX.bin` (payload brut, toujours de
`BLOCK_SIZE` octets, dernier bloc d'un segment paddé avec des zéros) +
`block_XXXX.json` (métadonnées : `video_id`, `segment_name`,
`segment_index`, `block_index`, `block_count`, `segment_byte_length`).
Ce sont ces métadonnées qui permettent à `vod_gateway.py` de savoir combien
de blocs récupérer et dans quel ordre les réassembler.

## Sécurité (SIS + IBS)

- **SIS** (`SISIntegrity.py`) calcule une empreinte `V` (un vecteur par
  bloc) à partir d'une matrice publique `A`, reconstructible par quiconque
  connaît la graine publique `paramA` — pas besoin de transmettre `A`
  elle-même.
- **IBS** (`IBSSignature.py`) signe `V` avec la clé identity-based de
  l'owner (pas de certificat à gérer : l'identité `owner_id` *est* la clé
  publique).
- À la vérification, on ne retransmet **pas** `V` si on veut juste un
  verdict global authentique/intègre (oui/non). Pour **localiser** quel
  bloc précis a été perdu/altéré, il faut transmettre `V` et appeler
  `verify_block()` par bloc — voir `security_demo.py` pour le détail des
  deux modes.

## Hypothèses à confirmer / points ouverts

- **Format de `{object}`** dans `PUT/GET /v1/storage/{tenant}/{objet}` :
  on suppose un nom plat sans `/` (`videoid_segment_block.bin`), faute
  d'avoir le code source de l'Edge Agent. S'il accepte des chemins
  imbriqués, `object_name()` dans `sendBlocks.py` peut être simplifié.
- **Topics MQTT** (`video/{id}/placement`, etc.) : provisoires, à
  remplacer par la convention réelle du control plane dès qu'elle sera
  connue — un seul endroit à changer dans `sendBlocks.py`.
- **Vérification SIS/IBS pas encore branchée dans `vod_gateway.py`** :
  actuellement la passerelle reconstruit les segments sans revérifier la
  signature. À ajouter si l'intégrité doit être garantie aussi en lecture,
  pas seulement à l'upload.
- **`/reconstruct` côté Edge Agent** : non implémenté (absent de l'API
  documentée). La reconstruction est centralisée côté `vod_gateway.py` à
  la place — plus simple, ne nécessite aucune modification de l'Edge
  Agent.
