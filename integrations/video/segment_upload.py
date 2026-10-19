"""Adapter: reuse colleague segmentation; never perform placement here."""
import json
import re
import subprocess
import sys
from pathlib import Path
sys.path.insert(0, "/app/vendor-video")
from segmentation_video import segment_video

source, output = Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve()
segment_video(source, output)
playlist = (output / "index.m3u8").read_text(encoding="utf-8")
files = []
def local_name(value):
    name = Path(value).name
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", name) or name in {".", ".."}:
        raise ValueError("invalid segment name")
    if not (output / name).is_file():
        raise ValueError("missing segment")
    if name not in files:
        files.append(name)
    return name
lines = []
for line in playlist.splitlines():
    if line.startswith("#EXT-X-MAP:"):
        line = re.sub(r'URI="([^"]+)"', lambda m: 'URI="' + local_name(m.group(1)) + '"', line)
    elif line and not line.startswith("#"):
        line = local_name(line)
    lines.append(line)
probe = subprocess.check_output(["ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "json", str(source)])
duration = float(json.loads(probe)["format"]["duration"])
if duration <= 0 or not files or files[0] != "init.mp4":
    raise ValueError("invalid VOD package")
(output / "package.json").write_text(json.dumps({"files": files, "playlist": "\n".join(lines) + "\n", "duration": duration}), encoding="utf-8")
