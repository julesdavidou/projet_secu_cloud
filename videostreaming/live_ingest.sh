#!/usr/bin/env bash
# Ingestion RTMP (OBS) -> segmentation HLS live en continu.
#
# Démarrez CE script en premier, puis lancez le stream dans OBS, puis
# démarrez live_watcher.py.
#
# Usage :
#   ./live_ingest.sh ./live_out 1935
set -euo pipefail

OUT_DIR="${1:-./live_out}"
PORT="${2:-1935}"

mkdir -p "$OUT_DIR"

echo "écoute RTMP sur le port $PORT, segments écrits dans $OUT_DIR"
echo "dans OBS : Paramètres > Stream > Service = Personnalisé..."
echo "  Serveur : rtmp://<IP_DE_CETTE_MACHINE>:$PORT/live"
echo "  Clé de stream : stream   (ou ce que vous voulez, ignoré ici)"
echo

ffmpeg -listen 1 -i "rtmp://0.0.0.0:${PORT}/live/stream" \
  -c:v libx264 -preset veryfast -tune zerolatency \
  -c:a aac \
  -f hls -hls_time 4 -hls_list_size 0 \
  -hls_flags append_list+independent_segments \
  -hls_segment_type fmp4 \
  -hls_fmp4_init_filename "init.mp4" \
  -hls_segment_filename "${OUT_DIR}/seg_%05d.m4s" \
  "${OUT_DIR}/index.m3u8"
