import argparse
import json
import random
from pathlib import Path
import sys

sys.setrecursionlimit(10000)

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

#Signature
def sign_blocks(all_blocks, owner_id):
    sec_params = SecurityParams()
    sis = SISIntegrity(sec_params)
    sis_params = make_sis_params(sec_params.q)

    A = sis.generate_matrix_A(sis_params)
    payloads = [b.payload for b in all_blocks]
    X = sis.build_data_matrix(payloads)
    V = sis.compute_V(A,X)

    ibs = IBSSignature()
    t_s, PK_s = ibs.setup_zone_keys()
    S_w_share = ibs.issue_secret_share(t_s, owner_id)
    PK_v = None
    S_w = ibs.compute_owner_key(None,S_w_share)

    sigma = ibs.generate_signature(V, sis_params, S_w)

    # Ce qui voyage avec les blocs : sis_params + owner_id, PK_s + sigma (public)
    # V optionnel : inclus seulement si diagnostic bloc-par-bloc voulu

    return {
        "sec_params": sec_params,
        "sis_params": sis_params,
        "owner_id": owner_id,
        "Pk_s": PK_s,
        "Pk_v": PK_v,
        "sigma": sigma,
        "V": V,
    }

def main():
    parser = argparse.ArgumentParser(description="Test de signature et vérification de blocs")
    parser.add_argument("--blocks-dir", type=Path, required=True, help="Dossier contenant les blocs à signer")
    parser.add_argument("--owner-id", type=str, required=True, help="Identifiant du propriétaire pour la signature")
    args = parser.parse_args()

    all_blocks = load_all_blocks(args.blocks_dir)
    package = sign_blocks(all_blocks, args.owner_id)

    # Sauvegarde du package de signature dans un fichier JSON
    package_path = args.blocks_dir / "signature_package.json"
    with package_path.open("w", encoding="utf-8") as f:
        json.dump(package, f, indent=4)
    
    print(f"Signature générée et sauvegardée dans {package_path}")