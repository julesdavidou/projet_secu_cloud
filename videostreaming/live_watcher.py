"""Ingestion live : surveille un dossier HLS alimenté EN CONTINU par ffmpeg
(OBS -> RTMP -> ffmpeg -> segments .m4s), et pour chaque segment nouvellement
FINALISÉ (plus en cours d'écriture), lance le pipeline complet : blocs ->
signature SIS/IBS -> placement -> envoi à l'Edge Agent choisi -> mise à jour
du manifest live (placement.json réécrit de façon atomique à chaque segment).

Détection de segment finalisé : on ne lit JAMAIS un segment dont le nom
vient d'apparaître dans le dossier — on attend qu'il soit RÉFÉRENCÉ dans
index.m3u8, ce qui n'arrive, avec ffmpeg, qu'une fois le fichier fermé.

Clés IBS et matrice SIS (A, paramA) générées UNE SEULE FOIS au démarrage du
watcher et réutilisées pour tous les segments du live — c'est ce qui permet
à un vérifieur de faire confiance à owner_id/PK_s pour toute la session,
sans re-négocier à chaque segment.

Lancement (après avoir démarré l'ingestion ffmpeg, voir live_ingest.sh) :
    py live_watcher.py --hls-dir ./live_out --out-dir ./blocks_out \
        --video-id live01 --owner-id quentin \
        --nodes http://edge-agent-1:8080,http://edge-agent-2:8080
"""
from __future__ import annotations

import argparse
import json
import time
from pathlib import Path

import httpx

from blocks import BLOCK_SIZE, segment_to_blocks
from hls_utils import parse_manifest
from segment_signer import SegmentSigner
from placement import choose_nodes_for_segments
from sendBlocks import object_name

def load_state(state_path: Path, video_id: str, owner_id: str) -> dict:
    if state_path.is_file():
        return json.loads(state_path.read_text())
    return {
        "video_id": video_id,
        "owner_id": owner_id,
        "live": True,
        "target_duration": 4,
        "init_segment": None,
        "segments": [],
        "placement": {},
        "security": None,           # rempli une fois au 1er segment (sis_params, PK_s)
        "segment_signatures": {},   # segment_name -> {"V": [...], "sigma": [w1, [x,y]]}
    }


def save_state_atomic(state_path: Path, state: dict) -> None:
    tmp = state_path.with_suffix(".tmp")
    tmp.write_text(json.dumps(state, indent=2))
    tmp.replace(state_path)  # écriture atomique : le gateway ne voit jamais un fichier à moitié écrit


def send_one_segment(
    hls_dir: Path, segment_name: str, video_id: str, owner_id: str,
    node_urls: list[str], signer: SegmentSigner,
) -> tuple[str, dict]:
    """Découpe, signe, place et envoie un seul segment.
    Retourne (nœud choisi, signature {"V":..., "sigma":...})."""
    data = (hls_dir / segment_name).read_bytes()
    env_blocks = segment_to_blocks(video_id, segment_name, 0, data, block_size=BLOCK_SIZE)
    payloads = [b.payload for b in env_blocks]

    signature = signer.sign_segment(video_id, segment_name, payloads, include_v=True)
    node_url = choose_nodes_for_segments(video_id, [segment_name], node_urls, strategy="round_robin")[segment_name]

    with httpx.Client(timeout=30) as client:
        for env in env_blocks:
            meta = {
                "video_id": env.video_id, "segment_name": env.segment_name,
                "segment_index": env.segment_index, "block_index": env.block_index,
                "block_count": env.block_count, "segment_byte_length": env.segment_byte_length,
                "block_size": len(env.payload),
            }
            idx = f"block_{env.block_index:04d}"
            bin_obj = object_name(video_id, segment_name, f"{idx}.bin")
            meta_obj = object_name(video_id, segment_name, f"{idx}.json")
            client.put(f"{node_url}/v1/storage/{owner_id}/{bin_obj}", content=env.payload).raise_for_status()
            client.put(f"{node_url}/v1/storage/{owner_id}/{meta_obj}", content=json.dumps(meta)).raise_for_status()

    return node_url, signature


def watch(
    hls_dir: Path, state_path: Path, video_id: str, owner_id: str,
    node_urls: list[str], poll_interval: float,
) -> None:
    manifest_path = hls_dir / "index.m3u8"
    state = load_state(state_path, video_id, owner_id)
    known = set(state["segments"])

    print("génération des clés IBS et de la matrice SIS (une fois pour tout le live)...")
    signer = SegmentSigner(owner_id)
    if state["security"] is None:
        state["security"] = signer.security_header()
        save_state_atomic(state_path, state)

    print(f"surveillance de {manifest_path} (connus: {len(known)} segment(s))")

    while True:
        if not manifest_path.is_file():
            time.sleep(poll_interval)
            continue

        text = manifest_path.read_text()
        init_segment, segments, target_duration = parse_manifest(text)
        state["target_duration"] = target_duration

        if init_segment and not state.get("init_segment"):
            print(f" [+] segment d'init détecté: {init_segment}")
            node_url, signature = send_one_segment(hls_dir, init_segment, video_id, owner_id, node_urls, signer)
            state["init_segment"] = init_segment
            state["placement"][init_segment] = node_url
            state["segment_signatures"][init_segment] = signature
            save_state_atomic(state_path, state)

        new_segments = [s for s in segments if s not in known]
        for seg in new_segments:
            print(f" [+] nouveau segment finalisé: {seg}")
            node_url, signature = send_one_segment(hls_dir, seg, video_id, owner_id, node_urls, signer)
            state["segments"].append(seg)
            state["placement"][seg] = node_url
            state["segment_signatures"][seg] = signature
            known.add(seg)
            save_state_atomic(state_path, state)
            print(f"     -> {node_url}, signé ({len(state['segments'])} segment(s) au total)")

        time.sleep(poll_interval)


def main() -> None:
    p = argparse.ArgumentParser(description="Watcher d'ingestion live signée : HLS local -> blocs -> SIS/IBS -> Edge Agents")
    p.add_argument("--hls-dir", type=Path, required=True, help="dossier où ffmpeg écrit index.m3u8 + segments")
    p.add_argument("--out-dir", type=Path, default=Path("./blocks_out"), help="dossier où écrire placement.json")
    p.add_argument("--video-id", required=True)
    p.add_argument("--owner-id", required=True)
    p.add_argument("--nodes", required=True, help="URLs des Edge Agents séparées par des virgules")
    p.add_argument("--poll-interval", type=float, default=1.0)
    args = p.parse_args()

    args.out_dir.mkdir(parents=True, exist_ok=True)
    state_path = args.out_dir / "placement.json"
    node_urls = [n.strip() for n in args.nodes.split(",")]

    try:
        watch(args.hls_dir, state_path, args.video_id, args.owner_id, node_urls, args.poll_interval)
    except KeyboardInterrupt:
        print("\narrêt du watcher")


if __name__ == "__main__":
    main()
