"""OUTIL HISTORIQUE : format/crypto du prototype antérieur à V3.
Parcours actuel : deploy/live-v4/live-check.py ; voir README_live_streaming.md.

Vérifie, pour un segment live donné, la signature SIS/IBS en récupérant
les blocs DEPUIS L'EDGE AGENT (réseau réel), pas depuis le disque local —
contrairement à security_demo.py qui vérifie en mémoire. C'est le test qui
dit si ce que le lecteur recevra vraiment est authentique et intact.

Usage :
    py live_verify.py --placement ./blocks_out/placement.json --segment seg_00007.m4s
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path

import httpx

from sendBlocks import object_name
from segment_signer import verify_segment


def fetch_segment_payloads(node_url: str, owner_id: str, video_id: str, segment_name: str) -> list[bytes]:
    with httpx.Client(timeout=30) as client:
        first_meta = object_name(video_id, segment_name, "block_0000.json")
        r = client.get(f"{node_url}/v1/storage/{owner_id}/{first_meta}")
        r.raise_for_status()
        block_count = r.json()["block_count"]

        payloads = []
        for i in range(block_count):
            bin_obj = object_name(video_id, segment_name, f"block_{i:04d}.bin")
            r = client.get(f"{node_url}/v1/storage/{owner_id}/{bin_obj}")
            r.raise_for_status()
            payloads.append(r.content)
        return payloads


def verify_one(state: dict, segment_name: str) -> None:
    node_url = state["placement"].get(segment_name)
    if not node_url:
        raise SystemExit(f"segment {segment_name} absent du placement")
    print(f"récupération des blocs de {segment_name} depuis {node_url}...")
    payloads = fetch_segment_payloads(node_url, state["owner_id"], state["video_id"], segment_name)
    ok, bad = verify_segment(state["security"], state["segment_signatures"][segment_name],
                             state["video_id"], segment_name, payloads)
    print(f"vérification (authenticité + intégrité) : {'OK' if ok else 'ÉCHEC'}")
    if not ok:
        print(f"blocs en défaut : {bad}" if bad else "(V non stocké : pas de localisation bloc par bloc)")


def main() -> None:
    p = argparse.ArgumentParser(description="Vérifie un segment live en allant chercher ses blocs sur le réseau")
    p.add_argument("--placement", type=Path, required=True)
    p.add_argument("--segment", required=True)
    args = p.parse_args()

    state = json.loads(args.placement.read_text())
    if not state.get("security"):
        raise SystemExit("aucune signature dans ce placement.json (live non signé ?)")
    if args.segment not in state.get("segment_signatures", {}):
        raise SystemExit(f"pas de signature enregistrée pour {args.segment}")

    verify_one(state, args.segment)


if __name__ == "__main__":
    main()
