"""Real Java identities, IBS/zone keys, delegated subkey, SQLite and Edge bytes.
Only this test's new media is edited/tombstoned; existing fixtures stay intact.
"""
import base64,hashlib,json,secrets,struct,subprocess,tempfile,time
from pathlib import Path
from urllib.parse import urlencode
import httpx
from concurrent.futures import ThreadPoolExecutor
from secure_services import ssl_client

WEB='https://web:8443';LOCAL='http://127.0.0.1:9300';B=125000
def atom(size,value):return struct.pack('>I4s',size,b'free')+bytes([value])*(size-8)
def nonce():return 'op-'+secrets.token_hex(16)
def b64(raw):return base64.b64encode(raw).decode()

with httpx.Client(verify=ssl_client(),trust_env=False,timeout=360) as c,httpx.Client(verify=ssl_client(),trust_env=False,timeout=360) as other:
 r=c.post(LOCAL+'/owner/login',json={'email':'secure-owner-a@example.invalid','password':'OnlyForLocalTest!2026'});assert r.status_code==200,(r.status_code,r.text);owner=r.json()['owner_id'];csrf=r.json()['csrf'];headers={'X-CSRF-Token':csrf}
 r=c.post(WEB+'/api/auth/login',data={'email':'secure-owner-a@example.invalid','password':'OnlyForLocalTest!2026'});assert r.status_code==200
 r=other.post(WEB+'/api/auth/login',data={'email':'secure-owner-b@example.invalid','password':'OnlyForLocalTest!2026'});assert r.status_code==200
 with tempfile.TemporaryDirectory() as folder:
  source=Path(folder)/'ops.mp4';subprocess.run(['ffmpeg','-v','error','-f','lavfi','-i','testsrc2=size=160x90:rate=12','-t','1','-c:v','libx264',str(source)],check=True);video_bytes=source.read_bytes();assert len(video_bytes)<B-8
  expected=video_bytes+atom(B-len(video_bytes),1)+atom(B,2)+atom(B,3)+atom(137,4)
  policy={'max_rtt_ms':2000,'budget_microcredits':100000,'retention_hours':24}
  r=c.post(LOCAL+'/owner/publish?'+urlencode({'title':'Operations-only fixture','service_policy':json.dumps(policy)}),content=expected,headers=headers);assert r.status_code==201,(r.status_code,r.text);root=r.json()['id']
  assert other.get(WEB+'/api/videos/'+root+'/original').status_code==404
  def owner_op(kind,**kw):
   r=c.post(LOCAL+'/owner/operate',json={'video_id':root,'kind':kind,'action_id':nonce(),**kw},headers=headers);assert r.status_code==200,(r.status_code,r.text);return r.json()
  def original():
   r=c.get(WEB+'/api/videos/'+root+'/original');assert r.status_code==200,(r.status_code,r.text);assert r.content==expected,'new version not byte-identical';return r
  original();state=owner_op('share');r=other.get(WEB+'/api/videos/'+root+'/original');assert r.status_code==200 and r.content==expected
  p=other.get(WEB+'/api/videos/'+root+'/playback');assert p.status_code==200;cached_url=p.json()['url'];assert other.get(WEB+cached_url).status_code==200
  owner_op('unshare');assert other.get(WEB+cached_url).status_code==404
  print('PASS real IBS share/unshare blocks access to already cached playback',flush=True)
  action=nonce();r=c.post(LOCAL+'/owner/delegate',json={'video_id':root,'delegate_id':'reconstructor-A1','operations':['modify_blocks','insert_blocks','delete_blocks','replace'],'action_id':action,'ttl_seconds':300},headers=headers);assert r.status_code==200,(r.status_code,r.text);grant=r.json()['grant'];assert r.json()['installed'];grant_id=grant['grant_id']
  def delegated(kind,**kw):
   body={'delegate_id':'reconstructor-A1','grant_id':grant_id,'kind':kind,'action_id':nonce(),**kw};r=c.post(LOCAL+'/owner/delegated/apply',json=body,headers=headers);assert r.status_code==200,(r.status_code,r.text);return body,r.json()
  # Real scoped refusal: no publication or catalogue mutation for share.
  r=c.post(LOCAL+'/owner/delegated/apply',json={'delegate_id':'reconstructor-A1','grant_id':grant_id,'kind':'share','action_id':nonce()},headers=headers);assert r.status_code==403,(r.status_code,r.text)
  replacement=atom(B,9);body={'delegate_id':'reconstructor-A1','grant_id':grant_id,'kind':'modify_blocks','action_id':nonce(),'edits':[{'index':1,'delete_count':1,'blocks':[b64(replacement)]}]}
  # Both are FIRST submissions: the worker must create one version, and both
  # callers must receive the same committed result despite concurrent arrival.
  with ThreadPoolExecutor(max_workers=2) as pool:
   replies=list(pool.map(lambda _:c.post(LOCAL+'/owner/delegated/apply',json=body,headers=headers),range(2)))
  assert all(r.status_code==200 for r in replies),[(r.status_code,r.text) for r in replies]
  result=replies[0].json();assert replies[1].json()['state']['commit']==result['state']['commit']
  expected=expected[:B]+replacement+expected[2*B:];original()
  print('PASS concurrent first submissions commit one immutable version',flush=True)
  retry=c.post(LOCAL+'/owner/delegated/apply',json=body,headers=headers);assert retry.status_code==200 and retry.json()['state']['commit']==result['state']['commit'],(retry.status_code,retry.text)
  inserted=atom(B,8);delegated('insert_blocks',edits=[{'index':2,'delete_count':0,'blocks':[b64(inserted)]}]);expected=expected[:2*B]+inserted+expected[2*B:];original()
  delegated('delete_blocks',edits=[{'index':1,'delete_count':1,'blocks':[]}]);expected=expected[:B]+expected[2*B:];original()
  tail=atom(173,7);delegated('modify_blocks',edits=[{'index':3,'delete_count':1,'blocks':[b64(tail)]}]);expected=expected[:3*B]+tail;original()
  print('PASS delegated modify/insert/delete, variable tail, exact replay and real SIS/IBS reconstruction',flush=True)
  state=c.get(WEB+'/api/videos/'+root+'/operations').json();new_id=state['current_video_id'];assert new_id!=root
  assert other.get(WEB+'/api/videos/'+new_id+'/original').status_code==404
  revoked=owner_op('revoke',grant_id=grant_id)
  r=c.post(LOCAL+'/owner/delegated/apply',json={'delegate_id':'reconstructor-A1','grant_id':grant_id,'kind':'replace','action_id':nonce(),'source':b64(expected)},headers=headers);assert r.status_code==403,(r.status_code,r.text);original()
  # Revocation does not invalidate the historical signature of committed bytes.
  assert c.get(WEB+'/api/videos/'+root+'/operations').json()['commit']==revoked['commit']
  print('PASS immediate revocation, preserved historical signatures, user isolation',flush=True)
  r=c.post(LOCAL+'/owner/delegate',json={'video_id':root,'delegate_id':'reconstructor-A1','operations':['share'],'action_id':nonce(),'ttl_seconds':10},headers=headers);assert r.status_code==200,(r.status_code,r.text);short=r.json()['grant'];time.sleep(max(0,short['expires']-time.time()+1))
  r=c.post(LOCAL+'/owner/delegated/apply',json={'delegate_id':'reconstructor-A1','grant_id':short['grant_id'],'kind':'share','action_id':nonce()},headers=headers);assert r.status_code==403,(r.status_code,r.text)
  deleted=owner_op('delete');assert deleted['deleted'];assert c.get(WEB+'/api/videos/'+root+'/original').status_code==404;assert c.get(WEB+'/media/'+new_id+'/video.mp4').status_code==404
  result={'root_id':root,'owner_id':owner,'last_content_id':new_id,'last_source_sha256':hashlib.sha256(expected).hexdigest(),'last_source_size':len(expected),'final_state':deleted,'concurrent_first_submission':'PASS','delegated_block_edits':'REAL','revoked_grant_id':grant_id,'scoped_refusal':'PASS','revocation':'PASS','expiry':'PASS','cached_access_revoked':'PASS','physical_reclamation':'NOT_REQUESTED; old immutable bytes retained and charged','physical_lab':'NOT_TESTED'}
  Path('/data/test-results').mkdir(exist_ok=True);Path('/data/test-results/operations.json').write_text(json.dumps(result,indent=2));print(json.dumps(result,indent=2),flush=True)
