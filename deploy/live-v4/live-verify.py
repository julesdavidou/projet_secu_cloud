"""Real Central state and cold Edge reconstruction before/after restart."""
import hashlib,json,os,ssl
from pathlib import Path
import httpx
root=Path('/fixture');fixture=json.loads((root/'live.json').read_text());live=fixture['live_id'];ctx=ssl.create_default_context(cafile='/pki/ca.crt');ctx.load_cert_chain('/pki/tls.crt','/pki/tls.key');auth={'Authorization':'Bearer '+Path('/operator/operator.token').read_text().strip()}
with httpx.Client(verify=ctx,timeout=90,trust_env=False) as c:
 r=c.get('https://central:8443/api/v1/live/'+live,headers=auth);r.raise_for_status();state=r.json();assert state['ended'] and not state['public'];assert len(state['chunks'])==len(fixture['verified_fragments'])
 baseline=root/'live-before-restart.json'
 if os.environ.get('LIVE_VERIFY_PHASE')=='after':assert json.loads(baseline.read_text())==state,'live head/history changed after restart'
 else:baseline.write_text(json.dumps(state,indent=2))
 first=sorted(fixture['verified_fragments'],key=lambda x:x['sequence'])[0]
 r=c.get('https://central:8443/api/v1/videos/'+first['video_id']+'/placements',headers=auth);r.raise_for_status();placement=r.json();assert placement['state']=='READY'
 unit=placement['units'][0];assert unit['state']=='STORED' and unit['proof']=='HTTP_SHA256_VERIFIED'
 target={k:unit[k] for k in ['owner_id','node_id','object','sha256']};target.update(video_id=first['video_id'],source_sha256=first['sha256']);(root/'fault-target.json').write_text(json.dumps(target,indent=2))
 r=c.post('https://web:8443/api/auth/login',data={'email':'secure-owner-a@example.invalid','password':'OnlyForLocalTest!2026'});r.raise_for_status()
 prefix='https://web:8443/api/live/'+live+'/segments/0/'
 a=c.get(prefix+'init.mp4');b=c.get(prefix+'segment.m4s');assert a.status_code==b.status_code==200;assert hashlib.sha256(a.content+b.content).hexdigest()==first['sha256']
 r=c.post('https://web:8443/api/auth/login',data={'email':'secure-owner-b@example.invalid','password':'OnlyForLocalTest!2026'});r.raise_for_status();assert c.get(prefix+'segment.m4s').status_code==404
print('PASS persistent live order/head/end/privacy and cold, identical Edge reconstruction')
