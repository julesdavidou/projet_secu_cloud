"""Actual Java auth -> local owner -> authority -> Go placement -> Edge SIS/IBS.
Uses fixed demo owner slots; never replaces an existing wallet or deletes data.
"""
import hashlib,json,os,struct,subprocess,tempfile,time
from pathlib import Path
import httpx
from secure_services import ssl_client
BASE='https://web:8443';OWNER='http://127.0.0.1:9300'
slot=os.environ.get('SMOKE_OWNER_SLOT','a')
assert slot in ('a','b')
email='secure-owner-'+slot+'@example.invalid';password='OnlyForLocalTest!2026'
with httpx.Client(verify=ssl_client(),timeout=240,trust_env=False) as c:
 r=c.post(OWNER+'/owner/login',json={'email':email,'password':password});assert r.status_code==200,(r.status_code,r.text);local=r.json();csrf=local['csrf'];owner=local['owner_id']
 r=c.post(BASE+'/api/auth/login',data={'email':email,'password':password});assert r.json()['client_id']==owner
 print('PASS Java identity reused by local owner and TLS gateway',flush=True)
 with tempfile.TemporaryDirectory() as d:
  source=Path(d)/'source.mp4'
  subprocess.run(['ffmpeg','-v','error','-f','lavfi','-i','testsrc2=size=320x180:rate=24','-f','lavfi','-i','sine=frequency='+('440' if slot=='a' else '880')+':sample_rate=44100','-t','6','-c:v','libx264','-c:a','aac',str(source)],check=True)
  # Valid MP4 free atom: force variable original blocks across zones regardless
  # of encoder compression. These are real stored, hashed and reconstructed bytes.
  padding=bytes(range(251))*5000
  with source.open('ab') as f:f.write(struct.pack('>I4s',len(padding)+8,b'free')+padding)
  data=source.read_bytes();expected=hashlib.sha256(data).hexdigest();start=time.perf_counter()
  r=c.post(OWNER+'/owner/publish?title=VOD-SIS-IBS',content=data,headers={'Content-Type':'video/mp4','X-CSRF-Token':csrf});assert r.status_code==201,(r.status_code,r.text)
  published=r.json();video=published['id'];assert published['integrity'].startswith('SIS-HESS');publish_time=time.perf_counter()-start
  print('PASS local owner signs real source/HLS blocks; authorities, Go placement and real Edge storage READY',flush=True)
  r=c.post(BASE+'/api/videos?title=unsigned',content=data,headers={'Content-Type':'video/mp4'});assert r.status_code==428
  start=time.perf_counter();r=c.get(BASE+'/api/videos/'+video+'/playback');assert r.status_code==200,(r.status_code,r.text);playback=r.json();cold=time.perf_counter()-start
  assert playback['integrity'].startswith('SIS-HESS'),playback
  raw=c.get(BASE+playback['url']);assert raw.status_code==200 and raw.content==data
  start=time.perf_counter();r=c.get(BASE+'/api/videos/'+video+'/playback');assert r.status_code==200;warm=time.perf_counter()-start
  original=c.get(BASE+'/api/videos/'+video+'/original');assert original.status_code==200 and original.content==data,(original.status_code,original.text[:100] if original.status_code!=200 else 'mismatch')
  print('PASS real Edge SIS/IBS reconstruction, byte-identical original, MP4 playback and unsigned-upload refusal',flush=True)
  part=c.get(BASE+playback['url'],headers={'Range':'bytes=0-127'});assert part.status_code==206 and part.content==data[:128]
  result={'owner':owner,'video_id':video,'source_sha256':expected,'source_size':len(data),'publish_seconds':round(publish_time,3),'cold_playback_seconds':round(cold,3),'warm_playback_seconds':round(warm,3),'integrity':published['integrity'],'physical_lab':'NOT_TESTED'}
  Path('/data/test-results').mkdir(exist_ok=True);Path('/data/test-results/latest.json').write_text(json.dumps(result,indent=2));print(json.dumps(result,indent=2),flush=True)
