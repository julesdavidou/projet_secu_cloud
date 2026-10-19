"""Utilitaires HLS partagés (parsing de index.m3u8, génération du manifest live)."""
from __future__ import annotations

import re

SEGMENT_RE = re.compile(r'^(?!#)(\S+\.m4s)\s*$', re.MULTILINE)
MAP_RE = re.compile(r'#EXT-X-MAP:URI="([^"]+)"')
TARGETDURATION_RE = re.compile(r'#EXT-X-TARGETDURATION:(\d+)')


def parse_manifest(text: str) -> tuple[str | None, list[str], int]:
    """Retourne (init_segment, segments dans l'ordre, target_duration)."""
    init_match = MAP_RE.search(text)
    td_match = TARGETDURATION_RE.search(text)
    return (
        init_match.group(1) if init_match else None,
        SEGMENT_RE.findall(text),
        int(td_match.group(1)) if td_match else 4,
    )


def build_live_manifest(data: dict) -> str:
    """Manifest live généré depuis l'état (placement.json) : pas d'#EXT-X-ENDLIST,
    donc le lecteur le re-télécharge en boucle."""
    td = data.get("target_duration", 4)
    lines = ["#EXTM3U", "#EXT-X-VERSION:7", f"#EXT-X-TARGETDURATION:{td}", "#EXT-X-MEDIA-SEQUENCE:0"]
    if data.get("init_segment"):
        lines.append(f'#EXT-X-MAP:URI="{data["init_segment"]}"')
    for seg in data.get("segments", []):
        lines.append(f"#EXTINF:{td},")
        lines.append(seg)
    return "\n".join(lines) + "\n"
