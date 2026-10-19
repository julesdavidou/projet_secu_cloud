"""Additional real interzone exercise. Consumes 12 MiB, never deletes files.
Only run against supplied 8 MiB/node demo; skips when capacity insufficient.
"""
import hashlib,json,time,urllib.request,uuid
from pathlib import Path
base="http://central:8088"
token=(Path('/run/web-secrets')/'operator.token').read_text().strip()
def call(path,body=None):
    request=urllib.request.Request(base+path,data=None if body is None else json.dumps(body).encode(),headers={'Authorization':'Bearer '+token,'Content-Type':'application/json'})
    with urllib.request.urlopen(request,timeout=120) as response:return json.load(response)
snap=call('/api/v1/snapshot');size=6*1024*1024
if any(snap['nodes'][n]['node']['available_for_project_bytes']<size for n in ['A1','B1']):
    print('SKIPPED interzone capacity test: less than 6 MiB free per demo node; existing files preserved');raise SystemExit(0)
video='interzone-'+uuid.uuid4().hex
payloads=[bytes([41])*size,bytes([97])*size]
upload={'manifest':{'request_id':video,'video_id':video,'source_node_id':'A1','total_size_bytes':2*size,'segments':[{'segment_id':i,'size_bytes':size} for i in range(2)]},'target_size_bytes':1,'contents':[{'extent_id':i,'sha256':hashlib.sha256(data).hexdigest()} for i,data in enumerate(payloads)]}
plan=call('/api/v1/uploads',upload)
assert plan['state']!='READY','PREPARE incorrectly READY'
assert {u['zone_id'] for u in plan['units']}=={'A','B'},plan
assert call('/api/v1/uploads',upload)['plan']==plan['plan'],'retry changed placement'
print('PASS source-local plus peer OFFER interzone A/B, PREPARE not READY, idempotent plan')
for unit in plan['units']:
    secret=(Path('/run/web-secrets')/('edge-'+unit['node_id']+'.http-token')).read_text().strip()
    headers={'Authorization':'Bearer '+secret,'Content-Type':'application/octet-stream'}
    url=unit['endpoint']+'/store/'+video+'/'+str(unit['extent_id'])
    with urllib.request.urlopen(urllib.request.Request(url,data=payloads[unit['extent_id']],method='PUT',headers=headers),timeout=120) as response:assert response.status in (200,201)
    with urllib.request.urlopen(urllib.request.Request(url,headers=headers),timeout=120) as response:assert hashlib.sha256(response.read()).hexdigest()==unit['sha256']
record=call('/api/v1/videos/'+video+'/placements')
assert record['state']=='READY' and all(u['proof']=='HTTP_SHA256_VERIFIED' for u in record['units']),record
Path('/data/test-results').mkdir(exist_ok=True)
Path('/data/test-results/'+video+'.json').write_text(json.dumps(record,indent=2))
print('PASS real bytes uploaded and HTTP reread SHA256 in both zones; durable READY',video)
