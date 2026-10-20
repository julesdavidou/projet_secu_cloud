"""Real-time RTMP source -> signed fragments -> Go placement -> real Edge reads.
Synthetic picture/sound fixture, actual FFmpeg TCP RTMP/HLS/crypto/storage.
"""
import base64,hashlib,json,os,secrets,subprocess,tempfile,threading,time
from pathlib import Path
from types import SimpleNamespace
from concurrent.futures import ThreadPoolExecutor
import httpx
from secure_services import ssl_client
from live_publish import publish
from live_quality import quality

WEB='https://web:8443';OWNER='http://127.0.0.1:9300';password='OnlyForLocalTest!2026';os.environ['CARIBOU_LIVE_PASSWORD']=password
with tempfile.TemporaryDirectory(prefix='real-live-') as folder:
 folder=Path(folder);source=folder/'input.mp4';capture=folder/'capture'
 subprocess.run(['ffmpeg','-v','error','-f','lavfi','-i','testsrc2=size=320x180:rate=24','-f','lavfi','-i','sine=frequency=660:sample_rate=44100','-t','32','-c:v','libx264','-preset','veryfast','-g','48','-pix_fmt','yuv420p','-c:a','aac',str(source)],check=True)
 url='rtmp://127.0.0.1:1935/live/stream';emitter=[];emitter_error=[]
 def emit():
  try:
   for _ in range(600):
    if any(':078F ' in line and ' 0A ' in line for line in Path('/proc/net/tcp').read_text().splitlines()):break
    time.sleep(.1)
   else:raise RuntimeError('RTMP listener did not start')
   with (folder/'emitter.log').open('wb') as f:
    p=subprocess.Popen(['ffmpeg','-v','error','-re','-i',str(source),'-c','copy','-f','flv',url],stderr=f);emitter.append(p);assert p.wait(timeout=90)==0,'real RTMP emitter failed'
    print('SOURCE_FINISHED '+json.dumps({'source_end_unix_ms':int(time.time()*1000)}),flush=True)
  except Exception as e:emitter_error.append(str(e))
 thread=threading.Thread(target=emit,daemon=True)
 reports=[];seen=set();first_while_live=False
 with httpx.Client(verify=ssl_client(),timeout=240,trust_env=False) as viewer,httpx.Client(verify=ssl_client(),timeout=240,trust_env=False) as other,httpx.Client(timeout=240,trust_env=False) as owner:
  for client,slot in [(viewer,'a'),(other,'b')]:r=client.post(WEB+'/api/auth/login',data={'email':'secure-owner-'+slot+'@example.invalid','password':password});r.raise_for_status()
  r=owner.post(OWNER+'/owner/login',json={'email':'secure-owner-a@example.invalid','password':password});r.raise_for_status();headers={'X-CSRF-Token':r.json()['csrf']}
  def policy(seq):return {'max_rtt_ms':2000,'budget_microcredits':100000,'retention_hours':24,'max_microcredits_per_gib_hour':1000000 if seq%2==0 else 600000}
  def check(state,item,init,fragment,process):
   global first_while_live
   seq=len(state['chunks'])-1;live=state['id'];child=state['chunks'][seq]['video_id'];started=time.perf_counter()
   if seq==0:first_while_live=bool(emitter and emitter[0].poll() is None)
   r=viewer.get(WEB+'/api/live/'+live+'/index.m3u8');assert r.status_code==200 and (seq!=0 or '#EXT-X-ENDLIST' not in r.text)
   assert other.get(WEB+'/api/live/'+live).status_code==404
   prefix=WEB+'/api/live/'+live+'/segments/'+str(seq)
   a=viewer.get(prefix+'/init.mp4');b=viewer.get(prefix+'/segment.m4s');assert a.status_code==b.status_code==200,(a.status_code,b.status_code,a.text[:100] if a.status_code!=200 else '')
   assert a.content==init and b.content==fragment,'not real identical Edge reconstruction'
   clip=folder/('read-'+str(seq)+'.mp4');clip.write_bytes(a.content+b.content)
   subprocess.run(['ffmpeg','-v','error','-i',str(clip),'-f','null','-'],check=True,stdout=subprocess.DEVNULL,stderr=subprocess.PIPE)
   r=viewer.get(WEB+'/api/videos/'+child+'/placement');r.raise_for_status();zones={u['zone_id'] for u in r.json()['units']};seen.update(zones)
   # Retry exact first publication: same bytes, same signed operation/head.
   body={'live_id':live,'action_id':'live-chunk-'+hashlib.sha256((live+'/'+str(seq)).encode()).hexdigest()[:32],'sequence':seq,'duration_ms':item['duration_ms'],'program_unix_ms':item['program_unix_ms'],'init':base64.b64encode(init).decode(),'fragment':base64.b64encode(fragment).decode(),'service_policy':policy(seq)}
   if seq==0:
    replay=owner.post(OWNER+'/owner/live/segment',json=body,headers=headers);assert replay.status_code==200 and replay.json()['state']['chunks'][0]['video_id']==child,(replay.status_code,replay.text)
    bad={**body,'sequence':1000,'action_id':'live-gap-'+secrets.token_hex(16)};r=owner.post(OWNER+'/owner/live/segment',json=bad,headers=headers);assert r.status_code==422
   reports.append({'sequence':seq,'video_id':child,'bytes':len(init)+len(fragment),'sha256':hashlib.sha256(init+fragment).hexdigest(),'verified_read_decode_ms':round((time.perf_counter()-started)*1000,3),'zones':sorted(zones)})
  args=SimpleNamespace(hls_dir=capture,owner_url=OWNER,email='secure-owner-a@example.invalid',title='Live integration '+secrets.token_hex(8),public=False,file=None,rtmp_listen=url,transcode=False,segment_seconds=4,max_pending=16,max_seconds=300,idle_timeout=60,reauth_seconds=5,policy=policy(0))
  thread.start()
  try:
   with ThreadPoolExecutor(max_workers=2) as reads:
    pending=[]
    result=publish(args,on_chunk=lambda *values:pending.append(reads.submit(check,*values)),policy_for_sequence=policy)
    for task in pending:task.result()
  finally:
   if emitter and emitter[0].poll() is None:emitter[0].terminate();emitter[0].wait(timeout=10)
   thread.join(timeout=10)
  assert not emitter_error,emitter_error
  assert first_while_live,'first verified fragment arrived only after the RTMP source ended'
  assert len(reports)>=4 and seen=={'A','B'},(len(reports),seen)
  assembled=folder/'full-read.mp4';combined=(capture/'init.mp4').read_bytes()
  for entry in sorted(reports,key=lambda e:e['sequence']):
   raw=(folder/('read-'+str(entry['sequence'])+'.mp4')).read_bytes();combined+=raw[len((capture/'init.mp4').read_bytes()):]
  assembled.write_bytes(combined);reference_quality=quality(source,assembled)
  assert reference_quality['reference_frames']==reference_quality['reconstructed_frames'] and reference_quality['ssim']>.9999,reference_quality
  result['reference_quality']=reference_quality
  live=result['live_id'];state=viewer.get(WEB+'/api/live/'+live).json()['state'];assert state['ended'];assert '#EXT-X-ENDLIST' in viewer.get(WEB+'/api/live/'+live+'/index.m3u8').text
  for kind,expected in [('share',200),('unshare',404)]:
   r=owner.post(OWNER+'/owner/live/'+kind,json={'live_id':live,'action_id':'live-'+kind+'-'+secrets.token_hex(16)},headers=headers);assert r.status_code==200,(r.status_code,r.text)
   assert other.get(WEB+'/api/live/'+live+'/segments/0/segment.m4s').status_code==expected
  r=owner.post(OWNER+'/owner/live/segment',json={'live_id':live,'action_id':'live-after-end-'+secrets.token_hex(16),'sequence':len(state['chunks']),'duration_ms':4000,'program_unix_ms':int(time.time()*1000),'init':base64.b64encode((capture/'init.mp4').read_bytes()).decode(),'fragment':base64.b64encode((capture/'seg_00000.m4s').read_bytes()).decode()},headers=headers);assert r.status_code==422
  result.update(reauth_interval_seconds_for_test=5,passed=True,rtmp='real FFmpeg TCP in real time',verified_before_source_end=first_while_live,source='synthetic test pattern',storage='actual isolated Edge files',zones=sorted(seen),verified_fragments=reports,access_revoked=True,ordered_gap_refused=True,append_after_end_refused=True)
  Path('/data/test-results').mkdir(exist_ok=True);Path('/data/test-results/live.json').write_text(json.dumps(result,indent=2));print('LIVE_TEST_PASS '+json.dumps({'live_id':live,'fragments':len(reports),'zones':sorted(seen)}),flush=True)
