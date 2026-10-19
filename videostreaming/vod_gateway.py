"""Passerelle VOD : sert manifest + segments au lecteur en HTTP classique
(TCP, aucun HTTP/3 nécessaire), en reconstituant chaque segment à la volée
à partir des blocs stockés sur les Edge Agents (GET /v1/storage/{tenant}/{object}).

Reconstruction CENTRALISÉE ici : aucune modification de l'Edge Agent requise,
pas besoin d'un /reconstruct côté nœud (qui n'existe pas dans l'API
documentée du projet).

Le lecteur (hls.js) ne parle qu'à CETTE passerelle, jamais directement aux
Edge Agents.

Lancement :
    OWNER_ID=quentin PLACEMENT_FILE=./blocks_out/placement.json \
    uvicorn vod_gateway:app --host 0.0.0.0 --port 8000

Test :
    curl http://localhost:8000/v1/videos/<video_id>/index.m3u8
"""
from __future__ import annotations

import json
import os
from pathlib import Path

import httpx
from fastapi import FastAPI, HTTPException
from fastapi.responses import PlainTextResponse, Response

from blocks import BlockEnvelope, reassemble_segment
from sendBlocks import object_name

OWNER_ID = os.getenv("OWNER_ID", "quentin")
PLACEMENT_FILE = Path(os.getenv("PLACEMENT_FILE", "./blocks_out/placement.json"))

app = FastAPI(title="Passerelle VOD")


def load_placement() -> dict:
    if not PLACEMENT_FILE.is_file():
        raise HTTPException(500, f"placement introuvable: {PLACEMENT_FILE}")
    return json.loads(PLACEMENT_FILE.read_text())


async def fetch_object(client: httpx.AsyncClient, node_url: str, tenant: str, obj: str) -> bytes:
    url = f"{node_url}/v1/storage/{tenant}/{obj}"
    r = await client.get(url)
    r.raise_for_status()
    return r.content


def build_live_manifest(data: dict) -> str:
    """Génère le m3u8 à la volée depuis placement.json (relu à chaque requête,
    donc toujours à jour pendant un live — pas de cache ni d'EXT-X-ENDLIST)."""
    lines = [
        "#EXTM3U",
        "#EXT-X-VERSION:7",
        f"#EXT-X-TARGETDURATION:{data.get('target_duration', 4)}",
        "#EXT-X-MEDIA-SEQUENCE:0",
    ]
    if data.get("init_segment"):
        lines.append(f'#EXT-X-MAP:URI="{data["init_segment"]}"')
    for seg in data.get("segments", []):
        lines.append(f"#EXTINF:{data.get('target_duration', 4)},")
        lines.append(seg)
    # pas d'#EXT-X-ENDLIST : dit au lecteur qu'il doit repoller ce manifest
    return "\n".join(lines) + "\n"


@app.get("/v1/videos/{video_id}/index.m3u8")
def manifest(video_id: str):
    data = load_placement()
    if data["video_id"] != video_id:
        raise HTTPException(404, "vidéo inconnue sur cette passerelle")

    if data.get("live"):
        return PlainTextResponse(build_live_manifest(data), media_type="application/vnd.apple.mpegurl")

    manifest_text = data.get("manifest")
    if not manifest_text:
        raise HTTPException(500, "manifest non disponible (passez manifest_path à sendBlocks.py)")
    return PlainTextResponse(manifest_text, media_type="application/vnd.apple.mpegurl")


@app.get("/v1/videos/{video_id}/{segment_name}")
async def get_segment(video_id: str, segment_name: str):
    data = load_placement()
    if data["video_id"] != video_id:
        raise HTTPException(404, "vidéo inconnue sur cette passerelle")
    owner_id = data["owner_id"]
    node_url = data["placement"].get(segment_name)
    if not node_url:
        raise HTTPException(404, f"segment {segment_name} absent du placement")

    async with httpx.AsyncClient(timeout=30) as client:
        # 1er bloc : lit les métadonnées pour connaître block_count
        first_meta = object_name(video_id, segment_name, "block_0000.json")
        try:
            meta0 = json.loads(await fetch_object(client, node_url, owner_id, first_meta))
        except httpx.HTTPStatusError:
            raise HTTPException(502, f"segment {segment_name} introuvable sur {node_url}")
        block_count = meta0["block_count"]

        envelopes: list[BlockEnvelope] = []
        for i in range(block_count):
            suffix = f"block_{i:04d}"
            meta_obj = object_name(video_id, segment_name, f"{suffix}.json")
            bin_obj = object_name(video_id, segment_name, f"{suffix}.bin")
            try:
                meta = json.loads(await fetch_object(client, node_url, owner_id, meta_obj))
                payload = await fetch_object(client, node_url, owner_id, bin_obj)
            except httpx.HTTPStatusError:
                raise HTTPException(502, f"bloc {i} manquant pour {segment_name} sur {node_url}")
            envelopes.append(BlockEnvelope(
                video_id=meta["video_id"],
                segment_name=meta["segment_name"],
                segment_index=meta["segment_index"],
                block_index=meta["block_index"],
                block_count=meta["block_count"],
                segment_byte_length=meta["segment_byte_length"],
                payload=payload,
            ))

    try:
        data_bytes = reassemble_segment(envelopes)
    except ValueError as e:
        raise HTTPException(502, f"reconstruction échouée pour {segment_name}: {e}")

    return Response(content=data_bytes, media_type="video/mp4")
