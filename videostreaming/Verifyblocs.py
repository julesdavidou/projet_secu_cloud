import argparse
import json
import random
from pathlib import Path

from blocks import BlockEnvelope
from SISIntegrity import SecurityParams, SISIntegrity, SISParams
from IBSSignature import IBSSignature

def load_blocks_segment(dir):
    """Charge tous les blocs d'un segment depuis son dossier (un bloc = .bin + .json)."""
    envelopes = []
    for meta_path in sorted(dir.glob("block_*.json")):
        meta = json.loads(meta_path.read_text(encoding="utf-8"))
        bin_path = meta_path.with_suffix(".bin")
        if not bin_path.is_file():
            raise ValueError(f"fichier binaire manquant pour {meta_path.name} (attendu: {bin_path.name})")
        payload = bin_path.read_bytes()
        if len(payload) != meta["block_size"]:
            raise ValueError(
                f"{bin_path.name}: taille {len(payload)} o, attendu {meta['block_size']} o (fichier tronqué/corrompu ?)"
            )
        envelopes.append(
            BlockEnvelope(
                video_id=meta["video_id"],
                segment_name=meta["segment_name"],
                segment_index=meta["segment_index"],
                block_index=meta["block_index"],
                block_count=meta["block_count"],
                segment_byte_length=meta["segment_byte_length"],
                payload=payload,
            )
        )
    return envelopes

def load_all_blocks(dir):
    """Charge tous les blocs de tous les segments depuis le dossier."""
    all_envelopes = []
    for seg_dir in sorted(p for p in dir.iterdir() if p.is_dir()):
        envelopes = load_blocks_segment(seg_dir)
        all_envelopes.extend(envelopes)
    return all_envelopes

def make_sis_params(q):
    """Crée un objet SISParams avec les paramètres de sécurité spécifiés."""
    return SISParams(
        I0= random.randrange(1,q),
        a= random.randrange(1,q),
        C0= random.randrange(1,q),
    )

def verify_blocks(all_blocks,package):
    sis = SISIntegrity(package["sec_params"])
    sis_params = package["sis_params"]
    A = sis.reconstruct_A(sis_params)

    payloads = [b.payload for b in all_blocks]
    X_prime = sis.build_data_matrix(payloads)
    V_prime = sis.compute_V(A,X_prime)

    ibs = IBSSignature()
    global_ok = ibs.verify_signature(package["owner_id"],package.get("Pk_v"), package["Pk_s"],package["sigma"], V_prime, sis_params)

    if "V" not in package:
        return

    print("diagnostic bloc par bloc :")
    bad = 0
    for b, v_expected in zip(all_blocks, package["V"]):
        ok = sis.verify_block(b.payload, A, v_expected)
        if not ok:
            print("ALTÉRÉ/CORROMPU")
            bad += 1
    print(f"{bad} bloc(s) en défaut sur {len(all_blocks)}") 

def main():
    parser = argparse.ArgumentParser(description="Test de signature et vérification de blocs")
    parser.add_argument("blocks_dir", type=Path, help="dossier contenant les blocs à vérifier")
    parser.add_argument("package_file", type=Path, help="fichier JSON contenant le package de signature")
    args = parser.parse_args()

    all_blocks = []
    for seg_dir in sorted(p for p in args.blocks_dir.iterdir() if p.is_dir()):
        envelopes = load_blocks_segment(seg_dir)
        all_blocks.extend(envelopes)

    package = json.loads(args.package_file.read_text(encoding="utf-8"))
    verify_blocks(all_blocks, package)