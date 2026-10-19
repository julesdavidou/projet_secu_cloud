"""Vérifie, pour un segment live donné, la signature SIS/IBS en récupérant
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

from crypto_serialize import deserialize_g2, deserialize_sigma
from IBSSignature import IBSSignature
from sendBlocks import object_name
from SISIntegrity import SecurityParams, SISIntegrity, SISParams


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


def verify_segment(state: dict, segment_name: str) -> None:
    node_url = state["placement"].get(segment_name)
    if not node_url:
        raise SystemExit(f"segment {segment_name} absent du placement")

    sec = state["security"]
    sec_params = SecurityParams(**sec["sec_params"])
    sis_params = SISParams(**sec["sis_params"])
    sis = SISIntegrity(sec_params)
    A = sis.reconstruct_A(sis_params)

    print(f"récupération des blocs de {segment_name} depuis {node_url}...")
    payloads = fetch_segment_payloads(node_url, state["owner_id"], state["video_id"], segment_name)

    X_prime = sis.build_data_matrix(payloads)
    V_prime = sis.compute_V(A, X_prime)

    PK_s = deserialize_g2(sec["PK_s"])
    sigma = deserialize_sigma(state["segment_signatures"][segment_name]["sigma"])

    ibs = IBSSignature()
    ok = ibs.verify_signature(sec["owner_id"], None, PK_s, sigma, V_prime, sis_params)
    print(f"vérification (authenticité + intégrité) : {'OK' if ok else 'ÉCHEC'}")

    if not ok:
        print("diagnostic bloc par bloc :")
        V = state["segment_signatures"][segment_name]["V"]
        bad = 0
        for i, (payload, v_expected) in enumerate(zip(payloads, V)):
            if not sis.verify_block(payload, A, v_expected):
                print(f"  bloc {i}: ALTÉRÉ/CORROMPU")
                bad += 1
        print(f"{bad} bloc(s) en défaut sur {len(payloads)}")


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

    verify_segment(state, args.segment)


if __name__ == "__main__":
    main()
