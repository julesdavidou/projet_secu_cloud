import argparse
import json
import random
from pathlib import Path
import py_ecc.fields.field_elements as fe


def iterative_pow(self, other: int):
    if other < 0:
        return (1 / self) ** -other
    result = type(self).one()
    base = self
    exp = int(other)
    while exp > 0:
        if exp % 2 == 1:
            result = result * base
        base = base * base
        exp //= 2
    return result


# Application du patch sur toutes les classes définies dans field_elements
for attr_name in dir(fe):
    attr = getattr(fe, attr_name)
    if isinstance(attr, type) and hasattr(attr, "__pow__"):
        attr.__pow__ = iterative_pow

import segmentation_video
import create_blocks
import reform_segments
import SignBlocks
import Verifyblocs
import sendBlocks
import generate_distributed_m3u8

def main():
    parser = argparse.ArgumentParser(description="Test de sécurité du système de blocs.")
    parser.add_argument("--video", required=True, help="Chemin vers la vidéo à traiter.")
    parser.add_argument("--blocks-dir", required=True, help="Dossier où les blocs seront créés.")
    parser.add_argument("--segments-dir", required=True, help="Dossier où les segments seront reconstitués.")
    parser.add_argument("--owner-id", required=True, help="Identifiant du propriétaire pour la signature.")
    args = parser.parse_args()

    video_path = Path(args.video)
    blocks_dir = Path(args.blocks_dir)
    segments_dir = Path(args.segments_dir)
    owner_id = args.owner_id

    hls_dir = video_path.parent / f"hls_{video_path.stem}"

    # Étape 1 : Segmentation de la vidéo
    print("Segmentation de la vidéo...")
    segmentation_video.segment_video(video_path, hls_dir, 4)

    # Étape 2 : Création des blocs
    print("Création des blocs...")
    create_blocks.process_folder(hls_dir, blocks_dir, video_id=video_path.stem, block_size=125000)

    # Étape 3 : Signature des blocs
    print("Signature des blocs...")
    all_blocks = SignBlocks.load_all_blocks(blocks_dir)
    package = SignBlocks.sign_blocks(all_blocks, owner_id)

    # Étape 4 : Envoi des blocs entre les utilisateurs
    NODES = [
        "http://192.168.1.196:9001",       # Nœud local sur l'émetteur
        "http://192.168.1.187:9001",    # Nœud distant sur le récepteur
    ]
    sendBlocks.send_blocks_distributed(blocks_dir, NODES, video_id=video_path.stem)

    # Étape 5 : Vérification des blocs
    print("Vérification des blocs...")
    Verifyblocs.verify_blocks(all_blocks, package)

    # Étape 6 : Reconstitution des segments
    print("Reconstitution des segments...")
    reform_segments.reform_folder(blocks_dir, segments_dir)

if __name__ == "__main__":
    main()