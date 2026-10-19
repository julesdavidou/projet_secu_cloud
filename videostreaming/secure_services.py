"""Small persistent zone authorities and local owner companion (secure profile).
Java remains the only account/session service. No private key is sent to Go,
MQTT or storage. Public trust is obtained from configured mTLS authorities.
"""
from __future__ import annotations
import base64,hashlib,http.cookies,http.server,json,os,re,secrets,sqlite3,ssl,subprocess,tarfile,tempfile,threading,time
from pathlib import Path
from urllib.parse import urlsplit,parse_qs
import httpx
from secure_sis import *
from blocks import pad_block

DATA=Path(os.environ.get('SECURITY_DATA','/data'));DATA.mkdir(exist_ok=True,parents=True)
ROLE=os.environ.get('SECURITY_ROLE','authority');ZONE=os.environ.get('ZONE_ID','A')
LOCK=threading.RLock();SESSIONS={};MAX_BODY=64*1024*1024

def ssl_client():
 context=ssl.create_default_context(cafile=os.environ['TLS_CA_FILE']);context.minimum_version=ssl.TLSVersion.TLSv1_3
 context.load_cert_chain(os.environ['TLS_CERT_FILE'],os.environ['TLS_KEY_FILE']);return context

def client(timeout=180):return httpx.Client(verify=ssl_client(),timeout=timeout,trust_env=False,follow_redirects=False)
def atomic(path,value):
 path=Path(path);path.parent.mkdir(parents=True,exist_ok=True);temp=path.with_suffix('.next')
 with temp.open('w') as f:os.chmod(temp,0o600);json.dump(value,f,sort_keys=True,separators=(',',':'));f.flush();os.fsync(f.fileno())
 os.replace(temp,path)
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
  if not verify_asset(proof,owner,video,asset,blocks,keys,body.get('revision',1),body.get('previous',''),body.get('public',False)):raise ValueError('SIS/IBS verification failed')
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
    if path=='/v3/verify/package':
     if peer!='web':raise PermissionError('web verifier client certificate required')
     return self.reply(200,verify_package(json.loads(self.read())))
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
  except PermissionError as e:self.reply(403,{'error':str(e)})
  except (ValueError,KeyError,TypeError,OverflowError,json.JSONDecodeError):self.reply(422,{'error':'invalid request or signature'})
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
  if path=='/owner/zone':
   body=json.loads(self.read());wallet=enroll_wallet(record,body['zone_id']);return self.reply(200,{'owner_id':wallet['owner_id'],'zones':wallet['zones']})
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

def publish(record,raw,query):
 wallet=enroll_wallet(record,os.environ.get('OWNER_INITIAL_ZONE','A'));private=aggregate_shares(list(wallet['shares'].values()));video='v-'+secrets.token_hex(16)
 with tempfile.TemporaryDirectory(dir=DATA,prefix='publication-') as folder:
  folder=Path(folder);source=folder/'source.mp4';source.write_bytes(raw);output=folder/'hls'
  env={**os.environ,'VIDEO_BLOCK_PIPELINE':'1','VIDEO_ID':video,'VIDEO_OWNER_ID':record['owner']}
  subprocess.run(['python','/app/video/segment_upload.py',str(source),str(output)],env=env,check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE,timeout=120)
  pack=json.loads((output/'package.json').read_text());proofs={};public=query.get('public',['false'])[0]=='true'
  for asset in pack['assets']:
   payloads=[pad_block((output/pack['files'][b['extent_id']]).read_bytes(),asset['block_size']) for b in asset['blocks']]
   proofs[asset['name']]=sign_asset(private,record['owner'],video,asset,payloads,wallet['zones'],public=public)
  manifest={**pack,'version':3,'video_id':video,'owner_id':record['owner'],'title':query.get('title',['Publication SIS/IBS'])[0],'category':'projets','public':public,'proofs':proofs,'revision':1,'previous':''}
  (output/'signed-manifest.json').write_bytes(canonical(manifest));archive=folder/'signed.tar'
  with tarfile.open(archive,'w') as tar:
   tar.add(output/'signed-manifest.json',arcname='signed-manifest.json')
   for name in pack['files']:tar.add(output/name,arcname=name)
  with client(240) as c:r=c.post(os.environ.get('WEB_URL','https://web:8443')+'/api/videos/signed',headers={'Cookie':record['cookie'],'Content-Type':'application/x-tar'},content=archive.read_bytes())
  r.raise_for_status();return r.json()

OWNER_HTML="""<!doctype html><html lang=fr><meta charset=utf-8><title>CaribouTV — agent propriétaire</title><body><h1>Publication protégée SIS/IBS</h1><p>Les clés privées restent dans cet agent local. Les vidéos publiées se consultent dans CaribouTV avec le même compte.</p><form id=login><input id=email type=email placeholder=Email required><input id=password type=password placeholder='Mot de passe' required><button>Connexion</button></form><form id=upload hidden><input id=title placeholder=Titre required><input id=file type=file accept=video/mp4 required><button>Signer et publier</button></form><pre id=status></pre><script>const login=document.getElementById('login'),upload=document.getElementById('upload'),email=document.getElementById('email'),password=document.getElementById('password'),titleInput=document.getElementById('title'),fileInput=document.getElementById('file'),statusEl=document.getElementById('status');let csrf='';login.onsubmit=async e=>{e.preventDefault();let r=await fetch('/owner/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({email:email.value,password:password.value})});let d=await r.json();statusEl.textContent=JSON.stringify(d,null,2);if(r.ok){csrf=d.csrf;upload.hidden=false;password.value=''}};upload.onsubmit=async e=>{e.preventDefault();statusEl.textContent='Signature, vérification et stockage en cours…';let r=await fetch('/owner/publish?title='+encodeURIComponent(titleInput.value),{method:'POST',headers:{'Content-Type':'video/mp4','X-CSRF-Token':csrf},body:fileInput.files[0]});statusEl.textContent=JSON.stringify(await r.json(),null,2)}</script></body></html>"""

def main():
 if ROLE=='authority':authority_key()
 context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);context.minimum_version=ssl.TLSVersion.TLSv1_3;context.load_cert_chain(os.environ['TLS_CERT_FILE'],os.environ['TLS_KEY_FILE']);context.load_verify_locations(os.environ['TLS_CA_FILE']);context.verify_mode=ssl.CERT_REQUIRED
 server=http.server.ThreadingHTTPServer(('0.0.0.0',8443),Handler);server.socket=context.wrap_socket(server.socket,server_side=True)
 if ROLE=='owner':
  local=http.server.ThreadingHTTPServer(('0.0.0.0',9300),Handler);threading.Thread(target=local.serve_forever,daemon=True).start()
 print('security service ready:',ROLE,ZONE,flush=True);server.serve_forever()
if __name__=='__main__':main()
