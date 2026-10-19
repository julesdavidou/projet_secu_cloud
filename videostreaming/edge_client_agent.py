"""Agent local (à lancer dans Docker sur la machine du LECTEUR).

Le lecteur (hls.js, ffplay, VLC) ne parle qu'à cet agent sur localhost. L'agent :
  1. lit le placement (fichier local, ou URL) : segment -> Edge Agent + signatures
  2. récupère les blocs DIRECTEMENT sur l'Edge Agent (GET /v1/storage/{owner}/{obj})
  3. vérifie SIS + IBS (rejette tout segment altéré / mal identifié -> HTTP 502)
  4. reforme le segment (blocks.reassemble_segment) et le sert en video/mp4
Aucun changement côté Edge Agent. La vérification est lourde (~4,6 s/segment en
Python pur) : elle tourne dans un pool de processus, avec cache et préchargement
des N segments suivants, pour ne pas bloquer la lecture.

Variables d'environnement :
  PLACEMENT_FILE   chemin de placement.json         (ou)
  PLACEMENT_URL    URL d'un placement.json (ex: servi par la passerelle/MQTT bridge)
  PINNED_PK_S      chemin d'un JSON {"owner_id","PK_s"} reçu par un canal de confiance.
                   Sans lui, la vérification prouve l'intégrité mais PAS l'authenticité
                   (le stockage pourrait fournir sa propre clé). Fortement recommandé.
  VERIFY           1 (défaut) / 0 pour désactiver (debug uniquement)
  WORKERS          processus de vérification (défaut : nb de CPU)
  PREFETCH         segments préchargés en avance (défaut 2)
  CACHE_SEGMENTS   segments vérifiés gardés en mémoire (défaut 16)
"""
from __future__ import annotations

import asyncio
import json
import os
import time
from collections import OrderedDict
from concurrent.futures import ProcessPoolExecutor
from pathlib import Path

import httpx
from fastapi import FastAPI, HTTPException
from fastapi.responses import PlainTextResponse, Response

from blocks import BlockEnvelope, reassemble_segment
from hls_utils import build_live_manifest, parse_manifest
from sendBlocks import object_name
from segment_signer import verify_segment

PLACEMENT_FILE = os.environ.get("PLACEMENT_FILE")
PLACEMENT_URL = os.environ.get("PLACEMENT_URL")
PINNED_PK_S = os.environ.get("PINNED_PK_S")
VERIFY = os.environ.get("VERIFY", "1") != "0"
WORKERS = int(os.environ.get("WORKERS", os.cpu_count() or 2))
PREFETCH = int(os.environ.get("PREFETCH", "2"))
CACHE_SEGMENTS = int(os.environ.get("CACHE_SEGMENTS", "16"))

app = FastAPI(title="SecuCloud edge client agent")
_pool: ProcessPoolExecutor | None = None
_cache: "OrderedDict[tuple, bytes]" = OrderedDict()
_inflight: dict[tuple, asyncio.Future] = {}
_pinned: dict | None = None


def pool() -> ProcessPoolExecutor:
    global _pool
    if _pool is None:
        _pool = ProcessPoolExecutor(max_workers=WORKERS)
    return _pool


async def load_placement() -> dict:
    if PLACEMENT_URL:
        async with httpx.AsyncClient(timeout=10) as c:
            r = await c.get(PLACEMENT_URL)
            r.raise_for_status()
            return r.json()
    if PLACEMENT_FILE:
        return json.loads(Path(PLACEMENT_FILE).read_text())
    raise HTTPException(500, "ni PLACEMENT_FILE ni PLACEMENT_URL défini")


def check_trust(data: dict) -> None:
    """Le placement vient du stockage/réseau : on ne lui fait confiance que si la clé publique
    et l'owner correspondent à ce qui a été épinglé par un canal de confiance."""
    global _pinned
    if not VERIFY or not PINNED_PK_S:
        return
    if _pinned is None:
        _pinned = json.loads(Path(PINNED_PK_S).read_text())
    sec = data.get("security") or {}
    if sec.get("PK_s") != _pinned["PK_s"] or sec.get("owner_id") != _pinned["owner_id"]:
        raise HTTPException(502, "clé publique / owner du placement différents de ceux épinglés : placement rejeté")


async def fetch_blocks(client: httpx.AsyncClient, node: str, owner: str, video_id: str, seg: str) -> list[BlockEnvelope]:
    base = f"{node}/v1/storage/{owner}"
    r = await client.get(f"{base}/{object_name(video_id, seg, 'block_0000.json')}")
    r.raise_for_status()
    count = r.json()["block_count"]

    async def one(i: int) -> BlockEnvelope:
        idx = f"block_{i:04d}"
        mb, bb = await asyncio.gather(
            client.get(f"{base}/{object_name(video_id, seg, idx + '.json')}"),
            client.get(f"{base}/{object_name(video_id, seg, idx + '.bin')}"),
        )
        mb.raise_for_status()
        bb.raise_for_status()
        m = mb.json()
        return BlockEnvelope(
            video_id=m["video_id"], segment_name=m["segment_name"], segment_index=m["segment_index"],
            block_index=m["block_index"], block_count=m["block_count"],
            segment_byte_length=m["segment_byte_length"], payload=bb.content,
        )

    return list(await asyncio.gather(*(one(i) for i in range(count))))


async def build_segment(data: dict, video_id: str, seg: str) -> bytes:
    node = data["placement"].get(seg)
    if not node:
        raise HTTPException(404, f"segment inconnu: {seg}")
    owner = data["owner_id"]
    try:
        async with httpx.AsyncClient(timeout=30) as client:
            blocks = await fetch_blocks(client, node, owner, video_id, seg)
    except httpx.HTTPError as e:
        raise HTTPException(502, f"Edge Agent {node} injoignable/erreur pour {seg}: {e}")
    blocks.sort(key=lambda b: b.block_index)

    if VERIFY:
        sec, entry = data.get("security"), (data.get("segment_signatures") or {}).get(seg)
        if not sec or not entry:
            raise HTTPException(502, f"{seg}: aucune signature (rejeté)")
        loop = asyncio.get_running_loop()
        t0 = time.time()
        ok, bad = await loop.run_in_executor(pool(), verify_segment, sec, entry, video_id, seg, [b.payload for b in blocks])
        print(f"[verify] {seg}: {'OK' if ok else 'ECHEC'} en {time.time() - t0:.1f}s" + (f" blocs fautifs={bad}" if bad else ""))
        if not ok:
            raise HTTPException(502, f"{seg}: vérification d'intégrité/authenticité ÉCHOUÉE" + (f" (blocs {bad})" if bad else ""))
    return reassemble_segment(blocks)


async def get_segment(data: dict, video_id: str, seg: str) -> bytes:
    key = (video_id, seg)
    if key in _cache:
        _cache.move_to_end(key)
        return _cache[key]
    if key in _inflight:  # une requête du lecteur + un préchargement ne doivent pas doubler le calcul
        return await asyncio.shield(_inflight[key])
    fut = asyncio.ensure_future(build_segment(data, video_id, seg))
    _inflight[key] = fut
    try:
        content = await fut
    finally:
        _inflight.pop(key, None)
    _cache[key] = content
    while len(_cache) > CACHE_SEGMENTS:
        _cache.popitem(last=False)
    return content


async def prefetch(data: dict, video_id: str, seg: str) -> None:
    segs = data.get("segments") or (parse_manifest(data["manifest"])[1] if data.get("manifest") else [])
    if seg not in segs:
        return
    i = segs.index(seg)
    for nxt in segs[i + 1 : i + 1 + PREFETCH]:
        if (video_id, nxt) not in _cache and (video_id, nxt) not in _inflight:
            asyncio.ensure_future(_safe(get_segment(data, video_id, nxt)))


async def _safe(coro) -> None:
    try:
        await coro
    except Exception as e:  # noqa: BLE001 - le préchargement ne doit jamais faire planter l'agent
        print(f"[prefetch] ignoré: {e}")


@app.get("/health")
async def health() -> dict:
    return {"ok": True, "verify": VERIFY, "pinned": bool(PINNED_PK_S), "cached": len(_cache)}


@app.get("/v1/videos/{video_id}/index.m3u8")
async def manifest(video_id: str) -> Response:
    data = await load_placement()
    if data["video_id"] != video_id:
        raise HTTPException(404, "vidéo inconnue")
    check_trust(data)
    if data.get("live"):
        text = build_live_manifest(data)
    elif data.get("manifest"):
        text = data["manifest"]
    else:
        raise HTTPException(404, "pas de manifest")
    return PlainTextResponse(text, media_type="application/vnd.apple.mpegurl", headers={"Cache-Control": "no-cache"})


@app.get("/v1/videos/{video_id}/{segment}")
async def segment(video_id: str, segment: str) -> Response:
    data = await load_placement()
    if data["video_id"] != video_id:
        raise HTTPException(404, "vidéo inconnue")
    check_trust(data)
    content = await get_segment(data, video_id, segment)
    await prefetch(data, video_id, segment)
    return Response(content, media_type="video/mp4")
