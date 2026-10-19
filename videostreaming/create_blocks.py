from __future__ import annotations

import argparse
import json
from pathlib import Path

from blocks import segment_to_blocks


def process_folder(input_dir: Path, output_dir: Path, video_id: str, block_size: int) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)

    # Récupérer tous les fichiers segments du dossier (exclut index.m3u8 si présent)
    files = [f for f in input_dir.iterdir() if f.is_file() and f.name != "index.m3u8"]
    if not files:
        print(f"Aucun segment trouvé dans {input_dir}")
        return

    print(f"Découpage en blocs de {block_size} octets pour {len(files)} fichier(s) depuis {input_dir}...")

    for seg_idx, file_path in enumerate(sorted(files)):
        data = file_path.read_bytes()
        
        # Découpage du segment en blocs de taille `block_size`
        blocks = segment_to_blocks(
            video_id=video_id,
            segment_name=file_path.name,
            segment_index=seg_idx,
            data=data,
            block_size=block_size,
        )

        # Création d'un sous-dossier par segment
        seg_out_dir = output_dir / file_path.name
        seg_out_dir.mkdir(exist_ok=True)

        for b in blocks:
            # Sauvegarde du contenu binaire fixe du bloc
            block_file = seg_out_dir / f"block_{b.block_index:04d}.bin"
            block_file.write_bytes(b.payload)

            # Sauvegarde des métadonnées associées
            meta_file = seg_out_dir / f"block_{b.block_index:04d}.json"
            meta_file.write_text(json.dumps(b.to_wire_dict(), indent=2), encoding="utf-8")

        print(f"  {file_path.name} ({len(data)} octets) -> {len(blocks)} bloc(s) de {block_size} octets")

    print(f"\nBlocs générés avec succès dans : {output_dir.resolve()}")


def main() -> None:
    p = argparse.ArgumentParser(description="Découper les segments vidéo en blocs de taille fixe pour la sécurité")
    p.add_argument("--data-dir", "-d", type=Path, required=True, help="Dossier contenant les segments (ex: ./data/467930416251)")
    p.add_argument("--block-size", "-b", type=int, required=True, help="Taille fixe obligatoire des blocs en octets (ex: 512, 1024, 2048)")
    p.add_argument("--out-dir", "-o", type=Path, default=Path("./blocks_out"), help="Dossier de sortie (défaut: ./blocks_out)")
    p.add_argument("--video-id", "-v", default="testvideo", help="Identifiant de la vidéo")
    
    args = p.parse_args()

    if args.block_size <= 0:
        p.error("La taille du bloc (--block-size) doit être un entier supérieur à 0.")

    process_folder(args.data_dir, args.out_dir, args.video_id, args.block_size)


if __name__ == "__main__":
    main()