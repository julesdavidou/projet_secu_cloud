"""Verify exported OCI/Docker image configuration and actual ELF binaries; never executes ARM."""
import argparse
import hashlib
import io
import json
import struct
import tarfile

parser = argparse.ArgumentParser()
parser.add_argument("archive")
parser.add_argument("--expect", choices=["amd64", "arm64"], required=True)
args = parser.parse_args()
names = {"central-controller", "serveur-zone", "edge-bridge", "edge-agent", "lab-admin"}
platforms = set()
with tarfile.open(args.archive) as archive:
    def blob(digest):
        return archive.extractfile("blobs/" + digest.replace(":", "/")).read()

    def manifests(entry):
        obj = json.loads(blob(entry["digest"]))
        if "manifests" in obj:
            for child in obj["manifests"]:
                yield from manifests(child)
        else:
            yield entry["digest"], obj

    for entry in json.load(archive.extractfile("index.json"))["manifests"]:
        for manifest_id, manifest in manifests(entry):
            config = json.loads(blob(manifest["config"]["digest"]))
            arch = config.get("architecture")
            if arch not in ("amd64", "arm64"):
                continue
            assert config.get("os") == "linux"
            platforms.add(arch)
            found = {}
            for layer in manifest["layers"]:
                with tarfile.open(fileobj=io.BytesIO(blob(layer["digest"])), mode="r:*") as files:
                    for item in files:
                        path = item.name.removeprefix("./")
                        if path.startswith("usr/local/bin/") and path.split("/")[-1] in names:
                            raw = files.extractfile(item).read()
                            assert raw[:4] == b"\x7fELF"
                            machine = struct.unpack("<H" if raw[5] == 1 else ">H", raw[18:20])[0]
                            assert machine == {"amd64": 62, "arm64": 183}[arch]
                            found[path.split("/")[-1]] = {"elf_machine": machine, "sha256": hashlib.sha256(raw).hexdigest()}
            assert set(found) == names, (arch, found)
            print(json.dumps({"result": "PASS", "platform": "linux/" + arch, "manifest": manifest_id, "config": manifest["config"]["digest"], "binaries": found}, sort_keys=True))
assert platforms == {args.expect}, platforms
