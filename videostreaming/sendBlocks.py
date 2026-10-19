"""Distribution des blocs vers les Edge Agents, via leur vraie API
PUT /v1/storage/{tenant}/{object} (voir README du projet).

TOUS les blocs d'un même segment partent sur le MÊME Edge Agent (choisi via
placement.py, stratégie interchangeable), pour permettre une reconstruction
centralisée à la passerelle VOD (pas besoin de /reconstruct côté Edge Agent,
qui n'existe pas dans l'API documentée).

{tenant} = owner_id (isolation par utilisateur, comme prévu par le README).
{object} = nom plat sans slash : "{video_id}_{segment_name}_{fichier_bloc}"
           HYPOTHÈSE à vérifier : je n'ai pas le code de l'Edge Agent, donc
           je ne sais pas si {object} accepte des "/" internes. Si oui, une
           organisation en "{video_id}/{segment_name}/{fichier}" serait plus
           propre — à ajuster quand vous aurez ce code sous les yeux.

Persiste le placement (+ manifest HLS optionnel) dans un fichier JSON local,
ET tente de le publier en MQTT (retained) si un broker est fourni — les
topics ci-dessous sont PROVISOIRES, à remplacer par la convention réelle du
control plane dès qu'elle sera connue.

Dépendances : pip install httpx paho-mqtt
"""
from __future__ import annotations

import json
from pathlib import Path

import httpx

from placement import choose_nodes_for_segments

try:
    import paho.mqtt.publish as mqtt_publish
    MQTT_AVAILABLE = True
except ImportError:
    MQTT_AVAILABLE = False


def object_name(video_id: str, segment_name: str, file_name: str) -> str:
    """Nom plat pour /v1/storage/{tenant}/{object} (pas de slash supposé)."""
    return f"{video_id}_{segment_name}_{file_name}"


def send_blocks_distributed(
    blocks_dir: Path,
    node_urls: list[str],
    video_id: str,
    owner_id: str,
    strategy: str = "round_robin",
    manifest_path: Path | None = None,  # ex: hls_dir/index.m3u8, pour que la passerelle puisse le servir
    mqtt_broker: str | None = None,       # ex: "192.168.1.1" (broker déjà en place)
    mqtt_port: int = 1883,
    signer=None,                          # SegmentSigner (segment_signer.py) -> VOD signée ; None = non signé
    include_v: bool = False,              # stocke V pour localiser un bloc altéré (volumineux)
) -> dict[str, str]:
    """Envoie les blocs, groupés par segment, vers un seul Edge Agent par segment.

    Retourne le placement {segment_name: node_url}, aussi écrit dans
    blocks_dir/placement.json (+ publié en MQTT si mqtt_broker est fourni).
    """
    seg_dirs = sorted(p for p in blocks_dir.iterdir() if p.is_dir())
    if not seg_dirs:
        print(f"Aucun dossier de segment trouvé dans {blocks_dir}")
        return {}

    segment_names = [d.name for d in seg_dirs]
    placement = choose_nodes_for_segments(video_id, segment_names, node_urls, strategy=strategy)

    signatures: dict[str, dict] = {}
    with httpx.Client(timeout=60) as client:
        for seg_dir in seg_dirs:
            segment_name = seg_dir.name
            target_node = placement[segment_name]
            files = sorted(seg_dir.glob("block_*"))  # .bin ET .json (métadonnées)

            for file_path in files:
                obj = object_name(video_id, segment_name, file_path.name)
                url = f"{target_node}/v1/storage/{owner_id}/{obj}"
                with open(file_path, "rb") as f:
                    resp = client.put(url, content=f.read())
                    resp.raise_for_status()

            if signer is not None:
                payloads = [p.read_bytes() for p in sorted(seg_dir.glob("block_*.bin"))]
                signatures[segment_name] = signer.sign_segment(video_id, segment_name, payloads, include_v=include_v)

            print(f" [+] {segment_name} ({len(files)} fichiers) -> {target_node}" + (" signé" if signer else ""))

    manifest_text = manifest_path.read_text() if manifest_path and manifest_path.is_file() else None

    manifest_data = {
        "video_id": video_id,
        "owner_id": owner_id,
        "placement": placement,
        "manifest": manifest_text,
    }
    if signer is not None:
        manifest_data["security"] = signer.security_header()
        manifest_data["segment_signatures"] = signatures
    out_path = blocks_dir / "placement.json"
    out_path.write_text(json.dumps(manifest_data, indent=2))
    print(f" [+] placement persisté dans {out_path}")

    if mqtt_broker:
        if not MQTT_AVAILABLE:
            print(" [!] paho-mqtt non installé (pip install paho-mqtt) — placement non publié au broker")
        else:
            try:
                mqtt_publish.single(
                    topic=f"video/{video_id}/placement",  # PROVISOIRE : à aligner sur la convention réelle
                    payload=json.dumps(manifest_data),
                    retain=True,
                    hostname=mqtt_broker,
                    port=mqtt_port,
                )
                print(f" [+] placement publié sur MQTT {mqtt_broker}:{mqtt_port} (topic provisoire video/{video_id}/placement)")
            except Exception as e:  # noqa: BLE001
                print(f" [!] publication MQTT échouée ({e}) — placement.json reste la source de vérité")

    return placement
