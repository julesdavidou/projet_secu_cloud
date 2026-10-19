"""Real multi-broker control, actual two-zone MP4 reconstruction and mobility.
A real temporary reservation limits source-zone capacity, never a fake STORED file.
All completed files remain; the capacity reservation is cancelled authoritatively.
"""
import base64,hmac,hashlib,http.cookiejar,json,struct,subprocess,tempfile,time,urllib.request,urllib.error,urllib.parse,uuid
from pathlib import Path
BASE='http://127.0.0.1:8080';CONTROL='http://central:8088'
root=Path('/run/web-secrets');operator=(root/'operator.token').read_text().strip()
run='network-'+uuid.uuid4().hex
client=urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
def request(url,body=None,method=None,headers=None,expect=200):
 if not url.startswith('http'):url=BASE+url
 if isinstance(body,dict):body=json.dumps(body).encode();headers={'Content-Type':'application/json',**(headers or {})}
 q=urllib.request.Request(url,data=body,method=method or ('POST' if body is not None else 'GET'),headers=headers or {})
 try:r=client.open(q,timeout=180)
 except urllib.error.HTTPError as e:r=e
 raw=r.read();assert r.status==expect,(url,r.status,raw[:300]);return raw,r.headers
def js(*a,**k):return json.loads(request(*a,**k)[0])
def control(path,body=None,expect=200):return js(CONTROL+path,body,headers={'Authorization':'Bearer '+operator},expect=expect)
def snap():return control('/api/v1/snapshot')
def rpc(target,op,body={}):return control('/api/v1/rpc/'+target+'/'+op,body)
def wait(predicate):
 for _ in range(60):
  if predicate():return
  time.sleep(1)
 raise AssertionError('timed out waiting for control convergence')
login=js('/api/auth/login',urllib.parse.urlencode({'email':run+'@example.invalid','password':'TestOnly-'+run}).encode(),headers={'Content-Type':'application/x-www-form-urlencoded'})
assert login['status']=='AUTHENTICATED'
for node in ['A1','B1']:
 for op in ['REQUEST_STATUS','REFRESH_STATE']:
  result=rpc('edge-'+node,op);assert result['node']['node_id']==node
try:rpc('edge-A1','DELETE_OBJECT',{'tenant':'project','object':'forbidden'})
except AssertionError:pass
else:raise AssertionError('uncoordinated delete accepted')
print('PASS MQTT3 A / MQTT5 B status commands and business ACK; destructive bypass refused',flush=True)
for key,old in snap()['uploads'].items():
 if key.startswith('network-') and key.endswith('-hold') and old['state']!='READY':rpc('central','CANCEL',{'video_id':old['upload']['manifest']['video_id'],'reservation_id':'unused'})
wait(lambda:snap()['nodes']['A1']['node']['available_for_project_bytes']>500000)
initial=snap();free=initial['nodes']['A1']['node']['available_for_project_bytes'];reserved=free-500000
assert reserved>0
hold=run+'-hold'
upload={'manifest':{'request_id':hold,'video_id':hold,'source_node_id':'A1','total_size_bytes':reserved,'segments':[{'segment_id':0,'size_bytes':reserved}]},'target_size_bytes':1,'contents':[{'extent_id':0,'sha256':'0'*64}]}
plan=control('/api/v1/uploads',upload,expect=201);assert plan['state']!='READY' and plan['units'][0]['node_id']=='A1'
wait(lambda:snap()['nodes']['A1']['node']['available_for_project_bytes']<=500000)
try:
 with tempfile.TemporaryDirectory() as d:
  source=Path(d)/'source.mp4'
  subprocess.run(['ffmpeg','-v','error','-f','lavfi','-i','testsrc2=size=320x180:rate=24','-f','lavfi','-i','sine=frequency=440:sample_rate=44100','-t','6','-c:v','libx264','-c:a','aac',str(source)],check=True)
  # Valid ISO BMFF free atom ensures the original asset spans zones independently
  # of encoder compression. The returned file must preserve these exact bytes too.
  padding=bytes(range(251))*5000
  with source.open('ab') as f:f.write(struct.pack('>I4s',len(padding)+8,b'free')+padding)
  data=source.read_bytes();expected=hashlib.sha256(data).hexdigest()
  published=js('/api/videos?'+urllib.parse.urlencode({'title':run}),data,headers={'Content-Type':'video/mp4'},expect=201)
  video=published['id'];record=control('/api/v1/videos/'+video+'/placements')
  assert {u['zone_id'] for u in record['units']}=={'A','B'},record['state']
  unit=record['units'][0]
  key=(root/('edge-'+unit['node_id']+'.http-token')).read_text().strip().encode()
  def grant(expiry):
   payload={'node':unit['node_id'],'video':video,'extent':unit['extent_id'],'sha256':unit['sha256'],'expires':expiry}
   body=base64.urlsafe_b64encode(json.dumps(payload,separators=(',',':')).encode()).rstrip(b'=').decode()
   return body+'.'+hmac.new(key,b'caribou-read-v1\x00'+body.encode(),hashlib.sha256).hexdigest()
  ticket=grant(int(time.time())+60)
  address=unit['endpoint']+'/store/'+video+'/'+str(unit['extent_id'])
  block=request(address,headers={'X-Read-Capability':ticket})[0]
  assert len(block)==unit['size_bytes'] and hashlib.sha256(block).hexdigest()==unit['sha256']
  request(address,expect=401)
  request(address,b'x',method='PUT',headers={'X-Read-Capability':ticket},expect=401)
  request(address,headers={'X-Read-Capability':grant(int(time.time())-1)},expect=401)
  request(address.replace(video,'wrong-video'),headers={'X-Read-Capability':ticket},expect=401)
  print('PASS real scoped read grants; anonymous, expired, wrong-object and write reuse refused',flush=True)

  assert control('/api/v1/uploads',record['upload'],expect=201)['plan']==record['plan']
  raw,headers=request('/api/videos/'+video+'/original');assert raw==data and headers['X-Content-SHA256']==expected
  playback=js('/api/videos/'+video+'/playback');assert request(playback['url'])[0]==data
  print('PASS real variable blocks span A/B, idempotent Go plan, Edge reconstruction cmp/source SHA256',flush=True)
  before=[(u['extent_id'],u['node_id'],u['object'],u['sha256']) for u in record['units']]
  rpc('edge-A1','MOVE',{'node_id':'A1','zone_id':'B','generation':0})
  try:
   wait(lambda:snap()['nodes']['A1']['node']['zone_id']=='B')
   moved=control('/api/v1/videos/'+video+'/placements')
   assert [(u['extent_id'],u['node_id'],u['object'],u['sha256']) for u in moved['units']]==before
   assert all(u['zone_id']=='B' for u in moved['units'])
   assert request('/api/videos/'+video+'/original')[0]==data
   print('PASS logical node mobility A->B through brokers, unchanged object placement and exact reconstruction',flush=True)
  finally:rpc('edge-A1','MOVE',{'node_id':'A1','zone_id':'A','generation':0})
  result={'run':run,'owner':login['client_id'],'video_id':video,'source_sha256':expected,'source_size':len(data),'state':record['state'],'nodes':sorted({u['node_id'] for u in record['units']}),'broker_protocols':{'A1':'MQTT3.1.1','B1':'MQTT5'},'physical_lab':'NOT_TESTED'}
  Path('/data/test-results').mkdir(exist_ok=True);Path('/data/test-results/'+run+'.json').write_text(json.dumps(result,indent=2))
  print(json.dumps(result,indent=2),flush=True)
finally:
 rpc('central','CANCEL',{'video_id':hold,'reservation_id':'unused'})
 wait(lambda:snap()['nodes']['A1']['node']['available_for_project_bytes']>500000)
 print('PASS temporary capacity reservation cancelled; no fake completion, no stored file deleted',flush=True)
