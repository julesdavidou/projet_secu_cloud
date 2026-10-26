"""Owner live adapter. Reuses V3 signing/upload, never chooses a storage node.
RTMP/HLS production lives in the native publisher; this service owns the key.
"""
import base64,hashlib,json,os,re,secrets,struct,time
from pathlib import Path
from blocks import segment_to_blocks
from keyed_lock import serialized
from secure_sis import aggregate_shares,sign_operation,canonical

LIVE=re.compile(r'^l-[a-f0-9]{32}$');ACTION=re.compile(r'^[A-Za-z0-9][A-Za-z0-9_.-]{19,127}$')

def boxes(raw):
 out=[];pos=0
 while pos<len(raw):
  if len(raw)-pos<8 or len(out)>1024:raise ValueError('invalid MP4 boxes')
  size,kind=struct.unpack_from('>I4s',raw,pos)
  if size<8 or pos+size>len(raw):raise ValueError('invalid MP4 box size')
  out.append(kind);pos+=size
 return out

def fragment_package(raw,output,video,duration_ms,block_size=None):
 if not 1<=duration_ms<=60000:raise ValueError('invalid live duration')
 block_size=int(os.environ.get('LIVE_BLOCK_SIZE','1048576')) if block_size is None else block_size
 if type(block_size)is not int or not 1<=block_size<=1024*1024:raise ValueError('invalid live block size')
 blocks=segment_to_blocks(video,'original.mp4',0,raw,block_size);files=[]
 asset={'name':'original.mp4','size_bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest(),'block_size':block_size,'blocks':[]}
 for b in blocks:
  value=raw[b.block_index*block_size:(b.block_index+1)*block_size];name=f'block-{b.block_index:05d}.bin';(output/name).write_bytes(value)
  asset['blocks'].append({'index':b.block_index,'extent_id':b.block_index,'size_bytes':len(value),'sha256':hashlib.sha256(value).hexdigest()});files.append(name)
 return {'files':files,'assets':[asset],'native_source':True,'playlist':'','duration':duration_ms/1000}

def state_for(s,record,live):
 with s.client() as c:r=c.get(os.environ['WEB_URL']+'/api/live/'+live,headers={'Cookie':record['cookie']});r.raise_for_status();return r.json()['state']

def proof_for(s,record,state,kind,nonce,data,new_id=''):
 wallet=s.enroll_wallet(record,os.environ.get('OWNER_INITIAL_ZONE','A'));key=aggregate_shares(list(wallet['shares'].values()))
 op={'owner_id':record['owner'],'video_id':state['id'],'kind':kind,'nonce':nonce,'expected_revision':state['revision'],'previous':state['commit'],'initial_public':state['public'],'live':data}
 if new_id:op['new_video_id']=new_id
 proof=sign_operation(key,op);proof['zones']=wallet['zones'];return proof

def submit(s,record,proof):
 with s.client(240) as c:r=c.post(os.environ['WEB_URL']+'/api/live/operations',headers={'Cookie':record['cookie']},json=proof);r.raise_for_status();return r.json()

def owner_live(h,path,record,s):
 body=json.loads(h.read());live=body.get('live_id','');action=body.get('action_id','')
 if not isinstance(live,str) or not LIVE.fullmatch(live) or not isinstance(action,str) or not ACTION.fullmatch(action):raise ValueError('stable live/action identity required')
 with serialized(('live',record['owner'],live)):
  folder=s.DATA/'live'/live;folder.mkdir(parents=True,exist_ok=True);fingerprint=hashlib.sha256(canonical({'path':path,'body':body})).hexdigest();target=folder/(action+'.json')
  old=json.loads(target.read_text()) if target.exists() else None
  if old and old['input_sha256']!=fingerprint:raise ValueError('live action identity conflict')
  if old and old.get('proof'):
   submit(s,record,old['proof']);return h.reply(200,{'state':state_for(s,record,live),'replayed':True})
  kind=path.removeprefix('/owner/live/')
  if kind=='create':
   state={'id':live,'owner_id':record['owner'],'revision':0,'commit':'','public':body.get('public',False)}
   if type(state['public'])is not bool or not isinstance(body.get('title'),str) or not 1<=len(body['title'])<=200:raise ValueError('live title/privacy required')
   proof=proof_for(s,record,state,'live_create',action,{'title':body['title']})
  else:
   state=state_for(s,record,live)
   if state['owner_id']!=record['owner']:raise PermissionError('another live owner')
   if kind=='status':return h.reply(200,{'state':state})
   if kind=='segment':
    seq=body.get('sequence');duration=body.get('duration_ms');program=body.get('program_unix_ms')
    if type(seq)is not int or not 0<=seq<1024 or type(duration)is not int or not 1<=duration<=60000 or type(program)is not int or program<1:raise ValueError('live sequence/timeline bounds')
    init=base64.b64decode(body['init'],validate=True);fragment=base64.b64decode(body['fragment'],validate=True)
    if not 8<len(init)<=1024*1024 or not 8<len(fragment)<=16*1024*1024:raise ValueError('live payload bounds')
    if boxes(init)!=[b'ftyp',b'moov'] or b'moof' not in boxes(fragment) or b'mdat' not in boxes(fragment):raise ValueError('finalized fMP4 required')
    raw=init+fragment;video='v-'+hashlib.sha256((live+'/'+str(seq)+'/').encode()+raw).hexdigest()[:32]
    if seq<len(state['chunks']):
     if state['chunks'][seq]['video_id']!=video or state['chunks'][seq]['duration_ms']!=duration or state['chunks'][seq]['program_unix_ms']!=program:raise ValueError('live sequence content/timeline conflict')
     return h.reply(200,{'state':state,'replayed':True})
    if state['ended'] or seq!=len(state['chunks']):raise ValueError('live gap or ended')
    if state['chunks'] and state['chunks'][0]['init_sha256']!=hashlib.sha256(init).hexdigest():raise ValueError('live codec/init changed; create a new session')
    policy=body.get('service_policy',{'max_rtt_ms':2000,'budget_microcredits':100000,'retention_hours':24})
    archive=s.DATA/'outbox'/(video+'.tar');start=time.perf_counter()
    if archive.exists():s.deliver_publication(record,archive,video)
    else:s.publish(record,raw,{'root_id':[live],'operation_kind':['live_segment-'+str(seq)],'title':['Live '+state['title']],'service_policy':[json.dumps(policy)]},video=video,live_duration_ms=duration)
    data={'duration_ms':duration,'program_unix_ms':program,'init_bytes':len(init),'init_sha256':hashlib.sha256(init).hexdigest(),'publish_ms':max(1,int((time.perf_counter()-start)*1000))}
    proof=proof_for(s,record,state,'live_append',action,data,video)
   elif kind in {'end','share','unshare'}:
    data={'reason':str(body.get('reason','publisher stopped'))[:200]} if kind=='end' else {}
    proof=proof_for(s,record,state,'live_'+kind,action,data)
   else:raise ValueError('unknown live owner operation')
  s.atomic(target,{'input_sha256':fingerprint,'proof':proof});result=submit(s,record,proof)
  return h.reply(200,{'state':result,'replayed':False})
