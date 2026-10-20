"""Adapter: reuse colleague segmentation; never perform placement here."""
import json
import os
import hashlib
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
package = {"files": files, "playlist": "\n".join(lines) + "\n", "duration": duration}
if os.environ.get("VIDEO_BLOCK_PIPELINE") == "1":
    from blocks import segment_to_blocks
    block_size = int(os.environ.get("VIDEO_BLOCK_SIZE", "125000"))
    if not 1 <= block_size <= 1024*1024:raise ValueError("invalid block size")
    probe_streams=json.loads(subprocess.check_output(["ffprobe","-v","error","-show_entries","stream=codec_type,codec_name","-of","json",str(source)]))["streams"]
    native=any(p["codec_type"]=="video" and p["codec_name"]=="h264" for p in probe_streams) and all(p["codec_type"] not in {"video","audio"} or p["codec_name"] in {"h264","aac"} for p in probe_streams)
    assets=[];stored=[]
    video_id=os.environ["VIDEO_ID"]
    for segment_index,name in enumerate(["original.mp4"]+files):
        data=source.read_bytes() if name=="original.mp4" else (output/name).read_bytes()
        asset={"name":name,"size_bytes":len(data),"sha256":hashlib.sha256(data).hexdigest(),"block_size":block_size,"blocks":[]}
        for b in segment_to_blocks(video_id,name,segment_index,data,block_size):
            raw=b.payload[:min(block_size,len(data)-b.block_index*block_size)]
            file_name=f"block-{len(stored):05d}.bin"
            (output/file_name).write_bytes(raw)
            asset["blocks"].append({"index":b.block_index,"extent_id":len(stored),"size_bytes":len(raw),"sha256":hashlib.sha256(raw).hexdigest()})
            stored.append(file_name)
        assets.append(asset)
    package.update({"version":2,"files":stored,"assets":assets,"native_source":native})
(output / "package.json").write_text(json.dumps(package), encoding="utf-8")
