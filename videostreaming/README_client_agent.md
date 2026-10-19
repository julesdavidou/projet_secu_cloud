# Agent client local (Docker sur la machine du lecteur)

Le lecteur (hls.js / ffplay / VLC) parle uniquement à `http://localhost:9200`. L'agent récupère les blocs
directement sur les Edge Agents, **vérifie SIS + IBS**, reforme les segments et les sert. Aucun changement
côté Edge Agent ; `vod_gateway.py` n'est plus nécessaire pour la lecture (il reste utilisable sans vérification).

```
Lecteur ─► agent local ──GET /v1/storage/{owner}/{objet}──► Edge Agents
            │ vérifie (pool de processus) → reforme → video/mp4
            └─ placement.json (fichier ou URL) + clé publique épinglée
```

## Côté uploader
```python
from segment_signer import SegmentSigner
signer = SegmentSigner("quentin")
send_blocks_distributed(blocks_dir, nodes, "vid1", "quentin", manifest_path=..., signer=signer)
json.dump(signer.trust_anchor(), open("pinned.json", "w"))   # à transmettre au lecteur par un canal de confiance
```
Le live (`live_watcher.py`) utilise le même `SegmentSigner` : mêmes fichiers, même agent côté lecteur.

## Côté lecteur
```
docker compose -f docker-compose.client.yml up --build
ffplay http://localhost:9200/v1/videos/vid1/index.m3u8
```
Variables : `PLACEMENT_FILE` ou `PLACEMENT_URL`, `PINNED_PK_S`, `WORKERS`, `PREFETCH`, `CACHE_SEGMENTS`, `VERIFY`.

## Ce qui a été testé (mock d'Edge Agent, 2 nœuds, vraie vidéo ffmpeg)
- lecture VOD signée : segments reformés identiques à l'original (`cmp`), init.mp4 inclus ;
- segment dont un bloc a été modifié sur le nœud → **HTTP 502**, rien n'est servi ;
- placement avec une clé publique différente de celle épinglée → **HTTP 502** ;
- préchargement des segments suivants en parallèle.
Non testé : vrai Edge Agent, MQTT, OBS en réel, lecteur navigateur.

## Points d'attention
- **Coût** : vérification ≈ 4,6–5 s CPU par segment (2 pairings py_ecc), signature ≈ 2,5 s. Avec des segments
  de 2–4 s, la lecture ne tient en temps réel que si `WORKERS` ≥ 2–3 et avec le préchargement ; le premier segment
  a toujours ~5 s de latence. Sur ARM, plus lent. Leviers : segments plus longs, signer par groupe de segments,
  ou une lib de pairing native (py_ecc est du Python pur).
- **Ancre de confiance** : sans `PINNED_PK_S`, un stockage malveillant peut fournir sa propre clé et ses signatures :
  vous n'auriez qu'une vérification d'intégrité. `pinned.json` doit venir d'un canal fiable.
- La signature couvre `[video_id, nom_du_segment, V]` (pas seulement V) : un segment valide ne peut pas être servi à la place d'un autre.
  Les signatures produites par l'ancienne version de `live_watcher.py` (V seul) ne sont plus valides.
- Live : si `live_watcher.py` redémarre sur un `placement.json` existant, il régénère des clés ; supprimez l'ancien dossier de sortie.
- `include_v=True` (live) stocke V pour localiser un bloc fautif, mais alourdit `placement.json`.
