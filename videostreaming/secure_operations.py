"""IBS signed operations and scoped worker delegation, backed by Central SQLite.
The owner key stays in its local agent; a worker receives only S+y*H1(mandate).
Content revisions are immutable, so failed CAS never overwrites existing bytes.
"""
from __future__ import annotations
import base64,hashlib,json,os,re,secrets,time
from pathlib import Path
from secure_sis import *
from keyed_lock import serialized

KINDS={'replace','insert_blocks','delete_blocks','modify_blocks','share','unshare','delete'}
ID=re.compile(r'^[A-Za-z0-9][A-Za-z0-9_.-]{19,127}$')

def root_state(s,record,root):
 with s.client() as c:r=c.get(os.environ['WEB_URL']+'/api/videos/'+root+'/operations',headers={'Cookie':record['cookie']});r.raise_for_status();return r.json()

def signed(s,key,operation,zones,certificate=None):
 proof=sign_operation(key,operation);proof['zones']=zones
 if certificate:proof['delegation']=certificate
 return proof

def operation(state,kind,nonce,new_id='',grant_id='',certificate=None):
 value={'owner_id':state['owner_id'],'video_id':state['root_id'],'kind':kind,'nonce':nonce,'expected_revision':state['revision'],'previous':state['commit'],'initial_public':state['public']}
 if new_id:value['new_video_id']=new_id
 if grant_id:value['grant_id']=grant_id
 if certificate:value['certificate']=certificate
 return value

def submit(s,record,proof,delegated=False):
 url='/api/v3/delegated/operations' if delegated else '/api/videos/'+proof['operation']['video_id']+'/operations'
 with s.client() as c:r=c.post(os.environ['WEB_URL']+url,json=proof,headers={'Cookie':record.get('cookie','')});r.raise_for_status();return r.json()

def patch_blocks(raw,block_size,kind,edits):
 if type(block_size)is not int or not 1<=block_size<=1024*1024 or not isinstance(edits,list) or not 1<=len(edits)<=64:raise ValueError('invalid block edits')
 blocks=[raw[i:i+block_size] for i in range(0,len(raw),block_size)];out=[];cursor=0
 for edit in edits:
  if not isinstance(edit,dict) or set(edit)!={'index','delete_count','blocks'}:raise ValueError('invalid edit schema')
  index=edit['index'];count=edit['delete_count'];inserted=edit['blocks']
  if type(index)is not int or type(count)is not int or not cursor<=index<=len(blocks) or not 0<=count<=len(blocks)-index or not isinstance(inserted,list) or len(inserted)>64:raise ValueError('unordered, overlapping or excessive block edit')
  if kind=='insert_blocks' and (count or not inserted) or kind=='delete_blocks' and (not count or inserted) or kind=='modify_blocks' and (not count or not inserted):raise ValueError('edit differs from operation kind')
  decoded=[base64.b64decode(v,validate=True) for v in inserted]
  if any(not 1<=len(b)<=block_size for b in decoded):raise ValueError('invalid variable block size')
  out+=blocks[cursor:index]+decoded;cursor=index+count
 out+=blocks[cursor:];value=b''.join(out)
 if not 0<len(value)<=16*1024*1024:raise ValueError('edited source size out of bounds')
 return value

def source_for(s,cookie,root,kind,body,delegated=False,grant_id=''):
 if kind=='replace':
  raw=base64.b64decode(body['source'],validate=True)
  if not 0<len(raw)<=16*1024*1024:raise ValueError('replacement source out of bounds')
  return raw
 route='/api/v3/delegated/original/' if delegated else '/api/videos/'
 url=route+root+('' if delegated else '/original')
 with s.client() as c:r=c.get(os.environ['WEB_URL']+url,headers={'Cookie':cookie,'X-Delegation-ID':grant_id});r.raise_for_status()
 return patch_blocks(r.content,int(r.headers['X-Block-Size']),kind,body['edits'])

def owner_operation(h,path,record,s):
 body=json.loads(h.read());nonce=body.get('action_id','')
 if not isinstance(nonce,str) or not ID.fullmatch(nonce):raise ValueError('stable action_id required')
 with serialized(('owner-action',record['owner'],nonce)):
  return _owner_operation(h,path,record,s,body)

def _owner_operation(h,path,record,s,body):
 root=body.get('video_id','')
 if not re.fullmatch(r'v-[a-f0-9]{32}',root):raise ValueError('invalid video identity')
 wallet=s.enroll_wallet(record,os.environ.get('OWNER_INITIAL_ZONE','A'));key=aggregate_shares(list(wallet['shares'].values()));state=root_state(s,record,root)
 if state['owner_id']!=record['owner'] or state['root_id']!=root :raise PermissionError('owner root unavailable')
 folder=s.DATA/'operations';folder.mkdir(exist_ok=True)
 nonce=body['action_id']
 if not isinstance(nonce,str) or not ID.fullmatch(nonce):raise ValueError('stable action_id required')
 target=folder/(nonce+'.json');fingerprint=hashlib.sha256(canonical(body)).hexdigest()
 if target.exists():
  old=json.loads(target.read_text())
  if old['input_sha256']!=fingerprint:raise ValueError('action identity conflict')
  result=None
  if old.get('content_video_id'):
   archive=s.DATA/'outbox'/(old['content_video_id']+'.tar')
   if not archive.exists():
    if state['revision']!=old['proof']['operation']['expected_revision']:raise ValueError('source changed before action preparation')
    raw=source_for(s,record['cookie'],root,body['kind'],body)
    s.publish(record,raw,{'root_id':[root],'operation_kind':[body['kind']],'title':[body.get('title','Version signée')],'service_policy':[json.dumps(body.get('service_policy',{'max_rtt_ms':2000,'budget_microcredits':100000,'retention_hours':24}))]},video=old['content_video_id'])
   s.deliver_publication(record,archive,old['content_video_id'])
   result=submit(s,record,old['proof'])
  if result is None:result=submit(s,record,old['proof'])
  if old.get('worker_url'):
   with s.client() as c:r=c.post(old['worker_url']+'/v3/delegations/install',headers={'Cookie':record['cookie']},json={'key':old['key'],'certificate':old['certificate']});r.raise_for_status()
   return h.reply(200,{'state':result,'grant':old['grant'],'installed':True,'action_id':nonce})
  return h.reply(200,result)
 if state['deleted']:raise PermissionError('media deleted')
 if path=='/owner/delegate':
  workers=s.configured_workers();worker=body.get('delegate_id','reconstructor-A1')
  if worker not in workers:raise ValueError('unprovisioned delegate')
  ttl=body.get('ttl_seconds',300)
  if type(ttl)is not int or not 10<=ttl<=3600:raise ValueError('delegation lifetime out of bounds')
  now=int(time.time());mandate={'owner_id':record['owner'],'video_id':root,'delegate_id':worker,'operations':body.get('operations',['replace']),'not_before':now-1,'expires':now+ttl-1,'max_revision':state['revision']+16,'max_bytes':16*1024*1024,'max_budget_microcredits':body.get('max_budget_microcredits',100000),'nonce':nonce}
  delegated_key,certificate=delegate(key,mandate);certificate['zones']=wallet['zones'];grant=delegation_info(certificate,record['owner'],aggregate_public(list(s.trusted_keys(wallet['zones']).values())))
  proof=signed(s,key,operation(state,'delegate',nonce,certificate=certificate),wallet['zones'])
  # Durable local subkey outbox allows explicit reinstall after transport loss.
  item={'input_sha256':fingerprint,'proof':proof,'key':delegated_key,'certificate':certificate,'worker_url':workers[worker],'grant':grant}
  s.atomic(target,item);result=submit(s,record,proof)
  with s.client() as c:r=c.post(workers[worker]+'/v3/delegations/install',headers={'Cookie':record['cookie']},json={'key':delegated_key,'certificate':certificate});r.raise_for_status()
  return h.reply(200,{'state':result,'grant':grant,'installed':True,'action_id':nonce})
 kind=body.get('kind')
 if kind not in KINDS|{'revoke'}:raise ValueError('unsupported owner operation')
 new_id=''
 if kind in {'replace','insert_blocks','delete_blocks','modify_blocks'}:
  raw=source_for(s,record['cookie'],root,kind,body);new_id='v-'+secrets.token_hex(16)
  proof=signed(s,key,operation(state,kind,nonce,new_id),wallet['zones']);s.atomic(target,{'input_sha256':fingerprint,'proof':proof,'content_video_id':new_id})
  s.publish(record,raw,{'root_id':[root],'operation_kind':[kind],'title':[body.get('title','Version signée')],'service_policy':[json.dumps(body.get('service_policy',{'max_rtt_ms':2000,'budget_microcredits':100000,'retention_hours':24}))]},video=new_id)
 else:
  proof=signed(s,key,operation(state,kind,nonce,new_id,body.get('grant_id','')),wallet['zones']);s.atomic(target,{'input_sha256':fingerprint,'proof':proof})
 return h.reply(200,submit(s,record,proof))

def worker_operation(h,path,s):
 if not h.peer().startswith('owner-'):raise PermissionError('local owner agent certificate required')
 owner=s.check_session(h.headers.get('Cookie',''));body=json.loads(h.read())
 # Hash the exact install body to serialize duplicate installs; apply retries
 # share a key even if a conflicting body is submitted with the same nonce.
 action=hashlib.sha256(canonical(body)).hexdigest() if path=='/v3/delegations/install' else body.get('action_id','')
 if not isinstance(action,str) or not ID.fullmatch(action):raise ValueError('stable action_id required')
 with serialized(('worker-action',owner,action)):
  return _worker_operation(h,path,s,owner,body)

def _worker_operation(h,path,s,owner,body):
 folder=s.DATA/'delegations';folder.mkdir(exist_ok=True)
 if path=='/v3/delegations/install':
  certificate=body['certificate'];key=body['key'];public=aggregate_public(list(s.trusted_keys(certificate['zones']).values()));grant=delegation_info(certificate,owner,public)
  if grant['delegate_id']!=os.environ['DELEGATE_ID'] or not grant['not_before']<=int(time.time())<grant['expires']:raise PermissionError('delegate identity or lifetime mismatch')
  test={'check':'delegation-subkey-possession'};params={'I0':17,'a':23,'C0':7}
  if not verify_value(owner,public,test,[],params,sign_value(key,test,[],params),certificate['operation']):raise ValueError('delegated subkey mismatch')
  if len(list(folder.glob('*.json')))>=1024 and not (folder/(grant['grant_id']+'.json')).exists():raise ValueError('delegation journal full')
  s.atomic(folder/(grant['grant_id']+'.json'),{'key':key,'certificate':certificate});return h.reply(200,{'installed':True,'grant_id':grant['grant_id']})
 if path!='/v3/delegations/apply':raise ValueError('unknown delegated operation')
 grant_id=body['grant_id'];action=body['action_id'];kind=body['kind']
 if not re.fullmatch(r'[a-f0-9]{64}',grant_id) or not ID.fullmatch(action) or kind not in KINDS:raise ValueError('invalid delegated action')
 item=json.loads((folder/(grant_id+'.json')).read_text());certificate=item['certificate'];root=certificate['operation']['mandate']['video_id']
 if certificate['operation']['owner_id']!=owner:raise PermissionError('another owner delegation')
 with s.client() as c:
  r=c.get(os.environ['WEB_URL']+'/api/v3/delegated/grants/'+grant_id);r.raise_for_status();grant=r.json()
  if grant['owner_id']!=owner or kind not in grant['operations']:raise PermissionError('operation outside delegation')
  r=c.get(os.environ['WEB_URL']+'/api/v3/delegated/state/'+root,headers={'X-Delegation-ID':grant_id});r.raise_for_status();state=r.json()
 if state['deleted']:raise PermissionError('media deleted')
 jobs=s.DATA/'delegated-actions';jobs.mkdir(exist_ok=True);target=jobs/(action+'.json');fingerprint=hashlib.sha256(canonical(body)).hexdigest()
 record={'owner':owner,'cookie':'','delegated':True};content_kind=kind in {'replace','insert_blocks','delete_blocks','modify_blocks'}
 if target.exists():
  job=json.loads(target.read_text())
  if job['input_sha256']!=fingerprint or job['grant_id']!=grant_id:raise ValueError('action identity conflict')
 else:
  if len(list(jobs.glob('*.json')))>=1024:raise ValueError('delegated action journal full')
  video='v-'+secrets.token_hex(16) if content_kind else ''
  proof=signed(s,item['key'],operation(state,kind,action,video),certificate['zones'],certificate)
  job={'input_sha256':fingerprint,'grant_id':grant_id,'video_id':video,'proof':proof};s.atomic(target,job)
 if content_kind:
  archive=s.DATA/'outbox'/(job['video_id']+'.tar')
  if archive.exists():s.deliver_publication(record,archive,job['video_id'])
  else:
   if state['revision']!=job['proof']['operation']['expected_revision']:raise ValueError('source changed before action preparation')
   raw=source_for(s,'',root,kind,body,True,grant_id)
   if len(raw)>grant['max_bytes']:raise ValueError('delegated size exceeded')
   policy=body.get('service_policy',{'max_rtt_ms':2000,'budget_microcredits':grant['max_budget_microcredits'],'retention_hours':24})
   s.publish(record,raw,{'root_id':[root],'operation_kind':[kind],'title':[body.get('title','Version déléguée')],'service_policy':[json.dumps(policy)]},signer={'key':item['key'],'zones':certificate['zones'],'delegation':certificate},video=job['video_id'])
 result=submit(s,record,job['proof'],True);return h.reply(200,{'state':result,'action_id':action,'delegated':True})
