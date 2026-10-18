import sys
from pathlib import Path
import ffmpeg


def segment_video(input_file, output_dir, segment_time=4):
    """Segmente une vidéo en utilisant ffmpeg et génère un fichier index.m3u8 dans output_dir."""
    input_path = Path(input_file).resolve()
    out_dir = Path(output_dir).resolve()
    out_dir.mkdir(parents=True, exist_ok=True)

    # Définition des chemins complets vers le dossier de sortie
    init_filename = str(out_dir / "init.mp4")
    segment_filename = str(out_dir / "seg_%05d.m4s")
    playlist_path = str(out_dir / "index.m3u8")

    (
        ffmpeg.input(str(input_path))
        .output(
            playlist_path,
            vcodec="libx264",
            preset="medium",
            crf=21,
            g=96,
            keyint_min=96,
            sc_threshold=0,
            acodec="aac",
            audio_bitrate=128000,
            f="hls",
            hls_time=segment_time,
            hls_playlist_type="vod",
            hls_segment_type="fmp4",
            hls_fmp4_init_filename=init_filename,
            hls_segment_filename=segment_filename,
        )
        .overwrite_output()
        .run()
    )


def main():
    args = sys.argv[1:]
    if not args:
        print(
            "Usage: python segmentation_video.py <input_file> <output_dir> [segment_time]"
        )
        return
    input_file = Path(args[0])
    output_dir = Path(args[1])
    segment_time = int(args[2]) if len(args) > 2 else 4
    segment_video(input_file, output_dir, segment_time)


if __name__ == "__main__":
    main()