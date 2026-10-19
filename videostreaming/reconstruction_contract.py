"""V2 adapter for the colleague client: Go decides placement, Edge reconstructs.
No public/operator storage credential is accepted or returned in this contract.
Read capabilities expire and authorize GET on one object only; hashes come from the API.
"""
from __future__ import annotations
import asyncio
import hashlib
import hmac
import json
import os
import re
from pathlib import Path
from urllib.parse import urlsplit
import httpx
from fastapi import HTTPException, Request
from fastapi.responses import Response
from blocks import BlockEnvelope, reassemble_segment, pad_block
from segment_signer import verify_segment

MAX_BYTES = 64*1024*1024
MAX_BLOCKS = 512
_gate = asyncio.Semaphore(2)

def integer(value, lower, upper):
    if type(value) is not int or not lower <= value <= upper:
        raise ValueError("invalid integer bounds")
    return value

def digest(value):
    if not isinstance(value, str) or not re.fullmatch(r"[0-9a-f]{64}", value):
        raise ValueError("invalid SHA256")
    return value

def signed_context(job):
    asset = job["asset"]
    return {"version": 2, "owner_id": job["owner_id"], "video_id": job["video_id"],
            "name": asset["name"], "size_bytes": asset["size_bytes"], "sha256": asset["sha256"],
            "block_size": asset["block_size"],
            "blocks": [{"index": b["index"], "size_bytes": b["size_bytes"], "sha256": b["sha256"]}
                       for b in sorted(asset["blocks"], key=lambda b: b["index"])]}

def validate_job(job, allowed):
    if job.get("version") not in {2,3}:
        raise ValueError("unsupported block contract")
    for key in ["video_id", "owner_id"]:
        if not re.fullmatch(r"[A-Za-z0-9_.-]{1,160}", job[key]):
            raise ValueError("invalid identity")
    asset=job["asset"]
    if not re.fullmatch(r"[A-Za-z0-9_.-]{1,160}", asset["name"]) or asset["name"] in {".",".."}:
        raise ValueError("invalid asset name")
    size=integer(asset["size_bytes"],1,MAX_BYTES)
    block_size=integer(asset["block_size"],1,1024*1024)
    digest(asset["sha256"])
    blocks=asset["blocks"]
    count=(size+block_size-1)//block_size
    if not isinstance(blocks,list) or not 1 <= len(blocks) == count <= MAX_BLOCKS:
        raise ValueError("missing or excessive blocks")
    indexes=set();extents=set()
    for b in blocks:
        i=integer(b["index"],0,count-1)
        if i in indexes or b["extent_id"] in extents:raise ValueError("duplicate block")
        indexes.add(i);extents.add(integer(b["extent_id"],0,MAX_BLOCKS-1))
        if integer(b["size_bytes"],1,block_size)!=min(block_size,size-i*block_size):raise ValueError("block length inconsistent with offset")
        digest(b["sha256"])
        endpoint=b["endpoint"]
        u=urlsplit(endpoint)
        if endpoint not in allowed or u.scheme not in {"http","https"} or not u.hostname or u.username or u.path or u.query or u.fragment:raise ValueError("unprovisioned endpoint")
        if not isinstance(b.get("read_capability"),str) or not 1<=len(b["read_capability"])<=2048:raise ValueError("missing read capability")
    return asset

async def reconstruct_bytes(job,allowed,client=None):
    asset=validate_job(job,allowed)
    owned=client is None
    if owned:
        if job.get("version")==3:
            from secure_services import ssl_client
            client=httpx.AsyncClient(timeout=30,follow_redirects=False,trust_env=False,verify=ssl_client())
        else:client=httpx.AsyncClient(timeout=30,follow_redirects=False,trust_env=False)
    limit=asyncio.Semaphore(4)
    try:
        async def one(b):
            async with limit:
                url=f"{b['endpoint']}/store/{job['video_id']}/{b['extent_id']}"
                async with client.stream("GET",url,headers={"X-Read-Capability":b["read_capability"]}) as response:
                    response.raise_for_status()
                    content=bytearray()
                    async for part in response.aiter_bytes():
                        content.extend(part)
                        if len(content)>b["size_bytes"]:raise ValueError("block length exceeded")
                raw=bytes(content)
                if len(raw)!=b["size_bytes"] or hashlib.sha256(raw).hexdigest()!=b["sha256"]:raise ValueError("block size/SHA256 mismatch")
                return BlockEnvelope(job["video_id"],asset["name"],0,b["index"],len(asset["blocks"]),asset["size_bytes"],raw,asset["block_size"])
        blocks=await asyncio.gather(*(one(b) for b in asset["blocks"]),return_exceptions=True)
        for block in blocks:
            if isinstance(block,BaseException):raise block
    finally:
        if owned:await client.aclose()
    raw=reassemble_segment(blocks)
    if hashlib.sha256(raw).hexdigest()!=asset["sha256"]:raise ValueError("asset SHA256 mismatch")
    if job.get("version")==3:
        from secure_sis import verify_asset,SUITE
        from secure_services import trusted_keys
        proof=job.get("security")
        if not proof or proof.get("suite")!=SUITE:raise ValueError("required SIS/IBS proof missing")
        keys=await asyncio.to_thread(trusted_keys,proof["zones"])
        payloads=[pad_block(b.payload,asset["block_size"]) for b in sorted(blocks,key=lambda b:b.block_index)]
        ok=await asyncio.to_thread(verify_asset,proof,job["owner_id"],job["video_id"],asset,payloads,keys,job.get("revision",1),job.get("previous",""),job.get("public",False))
        if not ok:raise ValueError("SIS/IBS verification failed")
        return raw
    sec,entry,anchor=job.get("security"),job.get("signature"),job.get("trust_anchor")
    if sec or entry or anchor:
        if not sec or not entry or not anchor or sec.get("owner_id")!=job["owner_id"] or sec.get("owner_id")!=anchor.get("owner_id") or sec.get("PK_s")!=anchor.get("PK_s"):
            raise ValueError("missing or mismatched independent trust anchor")
        context=signed_context(job)
        # Keep the colleague SIS/IBS calculations; bind all reconstruction metadata.
        payloads=[pad_block(b.payload,asset["block_size"]) for b in sorted(blocks,key=lambda b:b.block_index)]
        ok,_=await asyncio.to_thread(verify_segment,sec,entry,job["video_id"],asset["name"],payloads,context)
        if not ok:raise ValueError("SIS/IBS prototype verification failed")
    return raw

async def reconstruction_endpoint(request:Request):
    path=os.environ.get("WORKER_TOKEN_FILE","")
    try:token=Path(path).read_text().strip()
    except OSError:raise HTTPException(503,"worker authentication not configured")
    supplied=request.headers.get("authorization","")
    if len(token)<24 or not hmac.compare_digest(supplied,"Bearer "+token):raise HTTPException(401,"worker authentication required")
    body=bytearray()
    async for part in request.stream():
        body.extend(part)
        if len(body)>1024*1024:raise HTTPException(413,"reconstruction request too large")
    try:
        job=json.loads(body)
        allowed=set(filter(None,os.environ.get("ALLOWED_EDGE_ORIGINS","").split(",")))
        async with _gate:raw=await reconstruct_bytes(job,allowed)
    except (ValueError,KeyError,TypeError,httpx.HTTPError):raise HTTPException(502,"invalid, missing, corrupt or unauthorized reconstruction blocks")
    return Response(raw,media_type="application/octet-stream",headers={"X-Content-SHA256":hashlib.sha256(raw).hexdigest(),"Cache-Control":"no-store","X-Integrity":"SHA256; SIS/IBS prototype only when explicitly configured"})
