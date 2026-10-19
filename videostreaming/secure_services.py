"""Small persistent zone authorities and local owner companion (secure profile).
Java remains the only account/session service. No private key is sent to Go,
MQTT or storage. Public trust is obtained from configured mTLS authorities.
"""
from __future__ import annotations
import sys
import base64,hashlib,http.cookies,http.server,json,os,re,secrets,sqlite3,ssl,subprocess,tarfile,tempfile,threading,time
from pathlib import Path
from urllib.parse import urlsplit,parse_qs
import httpx
from secure_sis import *
from blocks import pad_block
from keyed_lock import serialized

DATA=Path(os.environ.get('SECURITY_DATA','/data'))
ROLE=os.environ.get('SECURITY_ROLE','authority');ZONE=os.environ.get('ZONE_ID','A')
LOCK=threading.RLock();SESSIONS={};MAX_BODY=64*1024*1024

class PublicationError(Exception):
 def __init__(self,status,value):self.status=status;self.value=value

def deliver_publication(record,archive,video):
 with serialized(('publication',record['owner'],video)):
  return _deliver_publication(record,archive,video)

def _deliver_publication(record,archive,video):
 # Retransmit the exact signed bytes and video ID, never sign another media on
 # an uncertain completion. Permanent refusals are returned to the owner.
 result=None
 for attempt in range(3):
  try:
   with client(240) as c:
    response=c.post(os.environ.get('WEB_URL','https://web:8443')+('/api/v3/delegated/videos' if record.get('delegated') else '/api/videos/signed'),headers={'Cookie':record.get('cookie',''),'Content-Type':'application/x-tar'},content=archive.read_bytes())
   if response.status_code==201:
    result=response.json();break
   try:value=response.json()
   except ValueError:value={}
   safe={k:value[k] for k in ('trace_id','stage','retryable') if k in value}
   error=PublicationError(503 if response.status_code>=500 else response.status_code,{'error':'publication refused','video_id':video,'upstream_status':response.status_code,**safe})
   if response.status_code not in (502,503,504) or not value.get('retryable',True):raise error
  except httpx.HTTPError as e:
   error=PublicationError(503,{'error':'publication transport unavailable','video_id':video,'stage':'transport','retryable':True,'category':type(e).__name__})
  if attempt==2:raise error
  time.sleep(.3*(attempt+1))
 atomic(DATA/'outbox'/ (video+'.json'),{'owner_id':record['owner'],'video_id':video,'state':'READY'})
 return result

def ssl_client():
 context=ssl.create_default_context(cafile=os.environ['TLS_CA_FILE']);context.minimum_version=ssl.TLSVersion.TLSv1_3
 context.load_cert_chain(os.environ['TLS_CERT_FILE'],os.environ['TLS_KEY_FILE']);return context

class DirectoryTransport(httpx.BaseTransport):
 def __init__(self):self.inner=httpx.HTTPTransport(verify=ssl_client(),trust_env=False)
 def close(self):self.inner.close()
 def handle_request(self,request):
  # Only configured, mutually authenticated zone directories can change dial
  # addresses. TLS verifies the original DNS identity at the new address.
  for origin in os.environ.get('DISCOVERY_ZONE_URLS','').split(','):
   if not origin.startswith('https://'):continue
   try:
    q=httpx.Request('GET',origin+'/api/v1/discovery/routes',extensions={'timeout':{'connect':2,'read':2,'write':2,'pool':2}});r=self.inner.handle_request(q)
    try:
     raw=b''
     for part in r.iter_bytes():
      raw+=part
      if len(raw)>65536:raise ValueError('directory too large')
     routes=json.loads(raw) if r.status_code==200 else {}
    finally:r.close()
    original=request.url;port=original.port or 443;route=routes.get(original.host+':'+str(port))
    if not route:continue
    from datetime import datetime,timezone
    import ipaddress
    remaining=(datetime.fromisoformat(route['expires_at'].replace('Z','+00:00'))-datetime.now(timezone.utc)).total_seconds();address=urlsplit('https://'+route['address']);ip=ipaddress.ip_address(address.hostname)
    if not 0<remaining<=31 or not ip.is_private or ip.version!=4 or not address.port:continue
    request.headers['Host']=original.netloc.decode();request.extensions['sni_hostname']=original.host;request.url=original.copy_with(host=str(ip),port=address.port);break
   except (httpx.HTTPError,ValueError,KeyError):continue
  return self.inner.handle_request(request)

def client(timeout=180):return httpx.Client(transport=DirectoryTransport(),timeout=timeout,trust_env=False,follow_redirects=False)

def configured_workers():
 workers=json.loads(os.environ.get('DELEGATE_WORKERS','{}'))
 path=Path(os.environ.get('CONTROLLERS_CONFIG','/config/config.json'))
 if path.exists():
  for node in json.loads(path.read_text())['nodes']:workers.setdefault('reconstructor-'+node['id'],'https://reconstructor-'+node['id']+':8443')
 return workers

def atomic(path,value):
 path=Path(path);path.parent.mkdir(parents=True,exist_ok=True)
 fd,name=tempfile.mkstemp(prefix=path.name+'.',suffix='.next',dir=path.parent)
 try:
  with os.fdopen(fd,'w',encoding='utf-8') as f:os.chmod(name,0o600);json.dump(value,f,sort_keys=True,separators=(',',':'));f.flush();os.fsync(f.fileno())
  os.replace(name,path)
 finally:
  Path(name).unlink(missing_ok=True)

def db():
 conn=sqlite3.connect(DATA/'authority.db',timeout=10);conn.execute('PRAGMA journal_mode=WAL');conn.execute('CREATE TABLE IF NOT EXISTS issued(owner TEXT NOT NULL,epoch INTEGER NOT NULL,created INTEGER NOT NULL,PRIMARY KEY(owner,epoch))');return conn

def authority_key():
 path=DATA/'zone-key.json'
 with LOCK:
  if path.exists():
   value=json.loads(path.read_text());master=int(value['secret']);public=value['public']
   if value['zone_id']!=ZONE or public!=encode_point(multiply(G2,master),2):raise ValueError('invalid persisted zone key')
   return master,public
  master,public=zone_keys();atomic(path,{'suite':SUITE,'zone_id':ZONE,'epoch':1,'secret':str(master),'public':public});return master,public

def check_session(cookie):
 if not isinstance(cookie,str) or not 1<len(cookie)<512:raise PermissionError('Java session required')
 with client(10) as c:r=c.get(os.environ.get('AUTH_URL','https://auth:8443')+'/api/auth/session',headers={'Cookie':cookie})
 if r.status_code!=200:raise PermissionError('Java session invalid')
 value=r.json();owner=value.get('client_id','')
 if value.get('status')!='AUTHENTICATED' or not re.fullmatch(r'client-[A-Za-z0-9-]{1,100}',owner):raise PermissionError('invalid Java identity')
 return owner

def trusted_keys(zones):
 configured=json.loads(os.environ.get('TRUSTED_IS_URLS','{}'));trusted={}
 with client(10) as c:
  for z in zones:
   key=(z['zone_id'],z['epoch'])
   if key in trusted or z['epoch']!=1 or z['zone_id'] not in configured:raise ValueError('unknown/duplicate authority epoch')
   r=c.get(configured[z['zone_id']]+'/v3/authority/info');r.raise_for_status();info=r.json()
   if info['suite']!=SUITE or info['zone_id']!=z['zone_id'] or info['epoch']!=z['epoch']:raise ValueError('authority identity mismatch')
   decode_point(info['public_key'],2);trusted[key]=info['public_key']
 return trusted

def verify_package(body):
 owner=body['owner_id'];video=body['video_id'];assets=body['assets'];proofs=body['proofs'];payloads=body['payloads']
 if not 1<=len(assets)<=128 or len(payloads)>512:raise ValueError('package bounds')
 for asset in assets:
  blocks=[]
  for b in asset['blocks']:
   raw=base64.b64decode(payloads[str(b['extent_id'])],validate=True)
   if len(raw)!=b['size_bytes'] or hashlib.sha256(raw).hexdigest()!=b['sha256']:raise ValueError('package block mismatch')
   blocks.append(pad_block(raw,asset['block_size']))
  proof=proofs[asset['name']];keys=trusted_keys(proof['zones'])
  if not verify_asset(proof,owner,video,asset,blocks,keys,body.get('revision',1),body.get('previous',''),body.get('public',False),body.get('root_id',''),body.get('operation_kind','')):raise ValueError('SIS/IBS verification failed')
 return {'verified':True,'suite':SUITE,'owner_id':owner,'video_id':video}

class Handler(http.server.BaseHTTPRequestHandler):
 protocol_version='HTTP/1.1'
 def log_message(self,*args):pass # never log cookies, shares or submitted credentials
 def reply(self,status,value,headers=None):
  raw=value if isinstance(value,bytes) else canonical(value);self.send_response(status)
  self.send_header('Content-Type','text/html; charset=utf-8' if isinstance(value,bytes) else 'application/json');self.send_header('Content-Length',str(len(raw)));self.send_header('Cache-Control','no-store');self.send_header('X-Content-Type-Options','nosniff')
  for k,v in (headers or {}).items():self.send_header(k,v)
  self.end_headers();self.wfile.write(raw)
 def read(self):
  length=int(self.headers.get('Content-Length','0'))
  if not 0<length<=MAX_BODY:raise ValueError('request length out of bounds')
  raw=self.rfile.read(length)
  if len(raw)!=length:raise ValueError('truncated request')
  return raw
 def peer(self):
  if not isinstance(self.connection,ssl.SSLSocket):return ''
  cert=self.connection.getpeercert()
  return next((v for group in cert.get('subject',()) for k,v in group if k=='commonName'),'')
 def do_GET(self):
  try:
   path=urlsplit(self.path).path
   if path=='/health':return self.reply(200,{'ready':True,'role':ROLE,'suite':SUITE})
   if ROLE=='authority' and path=='/v3/authority/info':
    _,public=authority_key();return self.reply(200,{'suite':SUITE,'zone_id':ZONE,'epoch':1,'public_key':public})
   if ROLE=='owner' and path=='/':return self.reply(200,OWNER_HTML.encode())
   self.reply(404,{'error':'unknown route'})
  except Exception:self.reply(503,{'error':'security service unavailable'})
 def do_POST(self):
  try:
   path=urlsplit(self.path).path
   if ROLE=='authority':
    peer=self.peer()
    if path=='/v3/authority/enroll':
     if not peer.startswith('owner-'):raise PermissionError('owner agent certificate required')
     body=json.loads(self.read());owner=check_session(self.headers.get('Cookie',''));master,public=authority_key()
     if body.get('zone_id')!=ZONE:raise ValueError('wrong authority zone')
     with LOCK,db() as conn:
      conn.execute('INSERT OR IGNORE INTO issued VALUES(?,?,?)',(owner,1,int(time.time())))
     return self.reply(200,{'suite':SUITE,'owner_id':owner,'zone_id':ZONE,'epoch':1,'public_key':public,'share':issue_share(master,owner)})
    if path=='/v3/verify/operation':
     if peer!='central':raise PermissionError('central verifier client certificate required')
     proof=json.loads(self.read());public=aggregate_public(list(trusted_keys(proof['zones']).values()));return self.reply(200,verify_signed_operation(proof,public))
    if path=='/v3/verify/package':
     if peer!='web':raise PermissionError('web verifier client certificate required')
     return self.reply(200,verify_package(json.loads(self.read())))
   if ROLE=='worker' and path.startswith('/v3/delegations/'):
    from secure_operations import worker_operation
    return worker_operation(self,path,sys.modules[__name__])
   if ROLE=='worker' and path=='/v3/reconstruct':
    if not self.peer().startswith('edge-'):raise PermissionError('Edge identity required')
    import asyncio
    from reconstruction_contract import reconstruct_bytes
    job=json.loads(self.read())
    if job.get('version')!=3:raise ValueError('secure reconstruction contract required')
    data=asyncio.run(reconstruct_bytes(job,set(os.environ['ALLOWED_EDGE_ORIGINS'].split(','))))
    self.send_response(200);self.send_header('Content-Type','application/octet-stream');self.send_header('Content-Length',str(len(data)));self.send_header('X-Integrity',SUITE);self.end_headers();self.wfile.write(data);return
   if ROLE=='owner':return self.owner_post(path)
   self.reply(404,{'error':'unknown route'})
  except PublicationError as e:self.reply(e.status,e.value)
  except PermissionError as e:self.reply(403,{'error':str(e)})
  except (ValueError,KeyError,TypeError,OverflowError,json.JSONDecodeError):self.reply(422,{'error':'invalid request or signature'})
  except httpx.HTTPStatusError as e:
   # Report the failed contract, never its body, headers, cookie or key shares.
   self.reply(e.response.status_code if 400<=e.response.status_code<500 else 502,{'error':'upstream operation refused','upstream_status':e.response.status_code,'upstream_path':e.request.url.path})
  except (httpx.HTTPError,subprocess.SubprocessError,OSError):self.reply(502,{'error':'required authority, catalogue or media operation unavailable'})
  except Exception:self.reply(500,{'error':'security operation failed'})
 def owner_post(self,path):
  origin=self.headers.get('Origin','');allowed=os.environ.get('OWNER_BROWSER_ORIGIN','http://localhost:19591')
  if origin and origin!=allowed:raise PermissionError('owner portal origin refused')
  if path=='/owner/login':
   body=json.loads(self.read())
   with client(15) as c:
    r=c.post(os.environ.get('AUTH_URL','https://auth:8443')+'/api/auth/login',data={'email':body.get('email',''),'password':body.get('password','')})
   value=r.json()
   if value.get('status')!='AUTHENTICATED':raise PermissionError('Java login refused')
   owner=value['client_id'];cookie='session='+r.cookies.get('session','')
   with LOCK:
    wallet_path=DATA/'owner-wallet.json'
    if wallet_path.exists() and json.loads(wallet_path.read_text())['owner_id']!=owner:raise PermissionError('this private agent belongs to another owner')
    session=secrets.token_urlsafe(32);csrf=secrets.token_urlsafe(24);SESSIONS[session]={'owner':owner,'cookie':cookie,'csrf':csrf,'expires':time.time()+1800}
   return self.reply(200,{'owner_id':owner,'csrf':csrf},{'Set-Cookie':'owner_session='+session+'; HttpOnly; SameSite=Strict; Path=/owner'})
  cookie=http.cookies.SimpleCookie(self.headers.get('Cookie',''));session=cookie.get('owner_session');record=SESSIONS.get(session.value if session else '')
  if not record or record['expires']<time.time() or self.headers.get('X-CSRF-Token')!=record['csrf']:raise PermissionError('local owner session and action token required')
  if check_session(record['cookie'])!=record['owner']:raise PermissionError('owner mismatch')
  if path=='/owner/delegated/apply':
   body=json.loads(self.read());workers=configured_workers();worker=body.pop('delegate_id','reconstructor-A1')
   if worker not in workers:raise ValueError('unknown delegate')
   with client(360) as c:r=c.post(workers[worker]+'/v3/delegations/apply',headers={'Cookie':record['cookie']},json=body);r.raise_for_status()
   return self.reply(200,r.json())
  if path in ('/owner/operate','/owner/delegate'):
   from secure_operations import owner_operation
   return owner_operation(self,path,record,sys.modules[__name__])
  if path=='/owner/zone':
   body=json.loads(self.read());wallet=enroll_wallet(record,body['zone_id']);return self.reply(200,{'owner_id':wallet['owner_id'],'zones':wallet['zones']})
  if path=='/owner/retry':
   video=json.loads(self.read()).get('video_id','')
   if not re.fullmatch(r'v-[a-f0-9]{32}',video):raise ValueError('invalid publication identity')
   folder=DATA/'outbox';state=json.loads((folder/(video+'.json')).read_text())
   if state['owner_id']!=record['owner']:raise PermissionError('wrong outbox owner')
   return self.reply(201,deliver_publication(record,folder/(video+'.tar'),video))
  if path=='/owner/publish':
   raw=self.read()
   if len(raw)>16*1024*1024:raise ValueError('source exceeds secure demo limit')
   q=parse_qs(urlsplit(self.path).query);result=publish(record,raw,q)
   return self.reply(201,result)
  self.reply(404,{'error':'unknown owner operation'})

def enroll_wallet(record,zone):
 configured=json.loads(os.environ['TRUSTED_IS_URLS'])
 if zone not in configured:raise ValueError('unknown zone')
 with LOCK:
  path=DATA/'owner-wallet.json';wallet=json.loads(path.read_text()) if path.exists() else {'owner_id':record['owner'],'zones':[],'shares':{}}
  if wallet['owner_id']!=record['owner']:raise PermissionError('owner wallet mismatch')
  if zone in wallet['shares']:return wallet
  with client(15) as c:r=c.post(configured[zone]+'/v3/authority/enroll',headers={'Cookie':record['cookie']},json={'zone_id':zone});r.raise_for_status();share=r.json()
  if share['owner_id']!=record['owner'] or share['zone_id']!=zone:raise ValueError('authority receipt mismatch')
  decode_point(share['share']);decode_point(share['public_key'],2)
  wallet['zones'].append({'zone_id':zone,'epoch':share['epoch']});wallet['shares'][zone]=share['share'];atomic(path,wallet);return wallet

def publish(record,raw,query,signer=None,video=None):
 if signer:private=signer['key'];zones=signer['zones'];certificate=signer['delegation']
 else:
  wallet=enroll_wallet(record,os.environ.get('OWNER_INITIAL_ZONE','A'));private=aggregate_shares(list(wallet['shares'].values()));zones=wallet['zones'];certificate=None
 video=video or 'v-'+secrets.token_hex(16);root=query.get('root_id',[''])[0];kind=query.get('operation_kind',[''])[0]
 with tempfile.TemporaryDirectory(dir=DATA,prefix='publication-') as folder:
  folder=Path(folder);source=folder/'source.mp4';source.write_bytes(raw);output=folder/'hls'
  env={**os.environ,'VIDEO_BLOCK_PIPELINE':'1','VIDEO_ID':video,'VIDEO_OWNER_ID':record['owner']}
  subprocess.run(['python','/app/video/segment_upload.py',str(source),str(output)],env=env,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,timeout=120)
  pack=json.loads((output/'package.json').read_text());proofs={};public=query.get('public',['false'])[0]=='true'
  for asset in pack['assets']:
   payloads=[pad_block((output/pack['files'][b['extent_id']]).read_bytes(),asset['block_size']) for b in asset['blocks']]
   proofs[asset['name']]=sign_asset(private,record['owner'],video,asset,payloads,zones,public=public,root_id=root,operation_kind=kind,delegation=certificate)
  policy=json.loads(query['service_policy'][0]) if 'service_policy' in query else None
  manifest={**pack,'version':3,'video_id':video,'owner_id':record['owner'],'title':query.get('title',['Publication SIS/IBS'])[0],'category':'projets','public':public,'proofs':proofs,'revision':1,'previous':''}
  if root:manifest.update(root_id=root,operation_kind=kind)
  if policy is not None:manifest['service_policy']=policy
  (output/'signed-manifest.json').write_bytes(canonical(manifest));archive=folder/'signed.tar'
  with tarfile.open(archive,'w') as tar:
   tar.add(output/'signed-manifest.json',arcname='signed-manifest.json')
   for name in pack['files']:tar.add(output/name,arcname=name)
  outbox=DATA/'outbox';outbox.mkdir(mode=0o700,exist_ok=True)
  target=outbox/(video+'.tar');temporary=outbox/(video+'.part')
  with temporary.open('wb') as f:os.chmod(temporary,0o600);f.write(archive.read_bytes());f.flush();os.fsync(f.fileno())
  os.replace(temporary,target);atomic(outbox/(video+'.json'),{'owner_id':record['owner'],'video_id':video,'state':'PENDING'})
  if query.get('prepare',['false'])[0]=='true':
   with client(240) as c:r=c.post(os.environ.get('WEB_URL','https://web:8443')+'/api/videos/signed/quote',headers={'Cookie':record['cookie'],'Content-Type':'application/x-tar'},content=target.read_bytes())
   r.raise_for_status();return r.json()
  return deliver_publication(record,target,video)

OWNER_HTML="""<!doctype html><html lang=fr><meta charset=utf-8><title>CaribouTV — agent propriétaire</title><body>
<h1>Publication protégée SIS/IBS</h1><p>Les clés privées restent dans cet agent local. Les fichiers conservent leur qualité d'origine. Prix en crédits de démonstration ; aucune facturation.</p>
<form id=login><input id=email type=email placeholder=Email required><input id=password type=password placeholder='Mot de passe' required><button>Connexion</button></form>
<form id=upload hidden><p><input id=title placeholder=Titre required><input id=file type=file accept=video/mp4 required></p>
<label>RTT maximal zone vers Edge (ms) <input id=rtt type=number min=1 max=60000 value=2000 required></label>
<label>Budget (microcrédits) <input id=budget type=number min=0 value=100000 required></label>
<label>Durée du devis (heures) <input id=hours type=number min=1 max=8760 value=24 required></label>
<button>Préparer et calculer le devis</button></form>
<p>La sonde réseau ne garantit pas la latence vidéo. Le devis ne réserve aucune capacité et sera revérifié à la publication. La durée sert au calcul du prix : aucune suppression automatique des médias.</p>
<form id=retry hidden><input id=video placeholder='Identifiant v-…' required><button>Publier ou reprendre le même paquet</button></form><section id=operations hidden><h2>Opérations signées</h2>
<p>Les versions et leurs fichiers sont conservés. Supprimer retire la vidéo du catalogue visible ; cela ne libère pas son stockage.</p>
<input id=root placeholder='Vidéo racine v-…'><select id=kind><option>share</option><option>unshare</option><option>delete</option><option>revoke</option></select><input id=grant placeholder='Identifiant de délégation'><button type=button id=operate>Signer cette opération</button>
<p><select id=delegate><option>reconstructor-A1</option><option>reconstructor-B1</option></select><button type=button id=delegateButton>Déléguer les mises à jour pour 5 minutes</button></p>
<p><button type=button id=delegatedReplace>Remplacer via cette délégation avec le fichier sélectionné</button></p></section><pre id=status></pre>
<script>
const el=id=>document.getElementById(id);let csrf='';
async function show(r){const d=await r.json();el('status').textContent=JSON.stringify(d,null,2);if(d.video_id)el('video').value=d.video_id;if(d.id)el('root').value=d.id;if(d.grant)el('grant').value=d.grant.grant_id;return d}
el('login').onsubmit=async e=>{e.preventDefault();let r=await fetch('/owner/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({email:el('email').value,password:el('password').value})});let d=await show(r);if(r.ok){csrf=d.csrf;el('upload').hidden=false;el('retry').hidden=false;el('password').value='';el('operations').hidden=false}};
el('upload').onsubmit=async e=>{e.preventDefault();el('status').textContent='Signature et devis en cours…';const policy={max_rtt_ms:Number(el('rtt').value),budget_microcredits:Number(el('budget').value),retention_hours:Number(el('hours').value)};await show(await fetch('/owner/publish?prepare=true&title='+encodeURIComponent(el('title').value)+'&service_policy='+encodeURIComponent(JSON.stringify(policy)),{method:'POST',headers:{'Content-Type':'video/mp4','X-CSRF-Token':csrf},body:el('file').files[0]}))};
el('retry').onsubmit=async e=>{e.preventDefault();await show(await fetch('/owner/retry',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify({video_id:el('video').value})}))};
async function action(path,value){return show(await fetch(path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify({...value,action_id:'op-'+crypto.randomUUID()})}))}
el('operate').onclick=()=>action('/owner/operate',{video_id:el('root').value,kind:el('kind').value,grant_id:el('grant').value});
el('delegatedReplace').onclick=async()=>{const file=el('file').files[0];if(!file){el('status').textContent='Sélectionnez un fichier vidéo.';return}const source=await new Promise((resolve,reject)=>{const reader=new FileReader();reader.onload=()=>resolve(reader.result.split(',')[1]);reader.onerror=reject;reader.readAsDataURL(file)});await action('/owner/delegated/apply',{grant_id:el('grant').value,delegate_id:el('delegate').value,kind:'replace',source})};
el('delegateButton').onclick=()=>action('/owner/delegate',{video_id:el('root').value,delegate_id:el('delegate').value,operations:['replace','insert_blocks','delete_blocks','modify_blocks'],ttl_seconds:300});
</script></body></html>"""

def main():
 DATA.mkdir(exist_ok=True,parents=True)
 if ROLE=='authority':authority_key()
 context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);context.minimum_version=ssl.TLSVersion.TLSv1_3;context.load_cert_chain(os.environ['TLS_CERT_FILE'],os.environ['TLS_KEY_FILE']);context.load_verify_locations(os.environ['TLS_CA_FILE']);context.verify_mode=ssl.CERT_REQUIRED
 server=http.server.ThreadingHTTPServer(('0.0.0.0',8443),Handler);server.socket=context.wrap_socket(server.socket,server_side=True)
 if ROLE=='owner':
  local=http.server.ThreadingHTTPServer(('0.0.0.0',9300),Handler);threading.Thread(target=local.serve_forever,daemon=True).start()
 print('security service ready:',ROLE,ZONE,flush=True);server.serve_forever()
if __name__=='__main__':main()
