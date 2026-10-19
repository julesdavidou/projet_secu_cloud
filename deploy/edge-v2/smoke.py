"""Real integration test: Java + Go + MQTT5 + SQLite + actual Edge Agent files.
No fake catalogue, placement algorithm, storage endpoint or video decoder.
The generated test pattern is a synthetic video fixture, stored as real bytes.
Does not remove any persistent file or volume; each run has distinct identities.
"""
import hashlib
import http.cookiejar
import json
import os
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

BASE=os.environ.get("SMOKE_URL","http://127.0.0.1:8080")
run="smoke-"+uuid.uuid4().hex
checks=[]
def client():
    return urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
alice,bob,anonymous=client(),client(),client()
def request(opener,path,method="GET",body=None,headers=None,expected=200):
    if isinstance(body,dict):
        body=json.dumps(body).encode();headers={"Content-Type":"application/json",**(headers or {})}
    req=urllib.request.Request((path if path.startswith("http") else BASE+path),data=body,method=method,headers=headers or {})
    try: response=opener.open(req,timeout=180)
    except urllib.error.HTTPError as error: response=error
    raw=response.read()
    assert response.status==expected,(method,path,response.status,raw[:500])
    return raw,response.headers
def js(*args,**kwargs): return json.loads(request(*args,**kwargs)[0])
def passed(name): checks.append(name);print("PASS "+name,flush=True)
def login(opener,email,password="TestOnly-"+run):
    result=js(opener,"/api/auth/login","POST",urllib.parse.urlencode({"email":email,"password":password}).encode(),{"Content-Type":"application/x-www-form-urlencoded"})
    assert result["status"]=="AUTHENTICATED",result
    return result["client_id"]
request(anonymous,"/ready")
request(anonymous,"/api/videos",expected=401)
owner=login(alice,run+"@example.invalid")
login(bob,"other-"+run+"@example.invalid")
passed("Java login and session-required API")
wrong=client();result=js(wrong,"/api/auth/login","POST",urllib.parse.urlencode({"email":run+"@example.invalid","password":"WrongPassword"}).encode(),{"Content-Type":"application/x-www-form-urlencoded"});assert result["status"]!="AUTHENTICATED";request(wrong,"/api/videos",expected=401)
passed("wrong password denied (colleague HTTP-200 failure contract preserved)")
with tempfile.TemporaryDirectory(prefix="caribou-test-") as temp:
    source=Path(temp)/"input.mp4"
    subprocess.run(["ffmpeg","-v","error","-f","lavfi","-i","testsrc2=size=320x180:rate=24","-f","lavfi","-i","sine=frequency=440:sample_rate=44100","-t","6","-c:v","libx264","-pix_fmt","yuv420p","-c:a","aac",str(source)],check=True)
    published=js(alice,"/api/videos?"+urllib.parse.urlencode({"title":run,"category":"projets"}),"POST",source.read_bytes(),{"Content-Type":"video/mp4"},201)
    video=published["id"]
    assert published["state"]=="READY",published
    passed("colleague HLS segmentation -> Go scheduler -> real Edge upload READY")
    placement=js(alice,"/api/videos/"+video+"/placement")
    assert len(placement["units"])>=3,placement
    assert all(u["state"]=="STORED" and u["proof"]=="HTTP_SHA256_VERIFIED" for u in placement["units"]),placement
    listed=js(alice,"/api/videos");assert video in [v["id"] for v in listed["items"]]
    assert video not in [v["id"] for v in js(bob,"/api/videos")["items"]]
    request(bob,"/api/videos/"+video+"/playback",expected=404)
    request(anonymous,"/media/"+video+"/video.mp4",expected=401)
    passed("SQLite READY catalogue, real SHA256 receipts, private ownership isolation")
    playback=js(alice,"/api/videos/"+video+"/playback")
    raw,headers=request(alice,playback["url"])
    original,headers=request(alice,"/api/videos/"+video+"/original")
    assert original==source.read_bytes(), "source reconstruction is not bit-identical"
    assert raw==original, "native H264/AAC playback does not preserve original MP4"
    assert headers["X-Content-SHA256"]==hashlib.sha256(original).hexdigest()
    assert headers["X-Reconstruction-Node"] in {"A1","B1"}
    request(bob,"/api/videos/"+video+"/original",expected=404)
    passed("original MP4 reconstructed on Edge: exact source bytes/SHA256, private download isolated")
    output=Path(temp)/"read.mp4";output.write_bytes(raw)
    probe=json.loads(subprocess.check_output(["ffprobe","-v","error","-show_entries","format=duration","-of","json",str(output)]))
    assert 5.5<float(probe["format"]["duration"])<6.5,probe
    subprocess.run(["ffmpeg","-v","error","-i",str(output),"-f","null","-"],check=True)
    part,headers=request(alice,playback["url"],headers={"Range":"bytes=0-127"},expected=206)
    assert part==raw[:128] and headers["Content-Range"].startswith("bytes 0-127/"),headers
    passed("Edge block reconstruction, exact native MP4, decode video+audio, HTTP Range206")
    profile=js(alice,"/api/me","PATCH",{"name":"Integration Test","course":"Cloud","promotion":"2026"})
    assert profile["name"]=="Integration Test"
    profile=js(alice,"/api/me/favorites/"+video,"PUT");assert video in profile["favorites"]
    profile=js(alice,"/api/me/progress/"+video,"PUT",{"position":2,"duration":6});assert profile["history"][0]["videoId"]==video
    assert js(bob,"/api/me")["history"]==[]
    assert video not in js(alice,"/api/me/favorites/"+video,"DELETE")["favorites"]
    assert js(alice,"/api/me/history","DELETE")["history"]==[]
    js(alice,"/api/me/favorites/"+video,"PUT")
    js(alice,"/api/me/progress/"+video,"PUT",{"position":2,"duration":6})
    request(alice,"/api/me","PATCH",{"name":"evil"},{"Origin":"https://evil.invalid"},403)
    passed("profiles, favorites, resume, user isolation, cross-origin mutation denial")
    request(alice,"/api/videos?title=invalid","POST",b"not-an-mp4-file",{"Content-Type":"video/mp4"},422)
    passed("invalid media never published READY")
    public=js(alice,"/api/videos?"+urllib.parse.urlencode({"title":"Démo intégrée – fichiers Edge réels","public":"true"}),"POST",source.read_bytes(),{"Content-Type":"video/mp4"},201)
    assert public["id"] in [v["id"] for v in js(bob,"/api/videos")["items"]]
    shared=js(bob,"/api/videos/"+public["id"]+"/playback")
    request(bob,shared["url"],headers={"Range":"bytes=0-127"},expected=206)
    request(anonymous,shared["url"],expected=401)
    passed("explicitly shared video readable by another signed-in account only")
    config=request(anonymous,"/assets/js/config.js")[0];assert b'dataSource:"api"' in config
    request(anonymous,"/");request(anonymous,"/integration/")
    passed("unchanged spectator frontend served with runtime API configuration")
    request(alice,"/api/auth/logout","POST",expected=204);request(alice,"/api/videos",expected=401)
    assert login(alice,run+"@example.invalid")==owner
    passed("logout revokes session, stable client ID on relogin")
    result={"run":run,"video_id":video,"owner":owner,"checks":checks,"placement":placement,"playback_bytes":len(raw),"playback_sha256":hashlib.sha256(raw).hexdigest(),"physical_lab":"NOT_TESTED","SIS_IBS_live":"NOT_INTEGRATED","fixture":"synthetic pattern; actual Edge files and decoder"}
    print(json.dumps(result,indent=2),flush=True)
    target=Path("/data/test-results");target.mkdir(exist_ok=True);(target/(run+".json")).write_text(json.dumps(result,indent=2),encoding="utf-8")
