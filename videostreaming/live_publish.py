"""Native authenticated live publisher. Owner agent holds keys and Go places bytes.
Works with a real-time file, local RTMP/OBS or an existing atomic HLS directory.
No direct Edge writes, crypto fallback or Python placement.
"""
from __future__ import annotations
import argparse,base64,getpass,hashlib,json,os,re,secrets,subprocess,time,sys
from datetime import datetime
from pathlib import Path
from urllib.parse import urlparse
import httpx

def entries(text):
 if not text.startswith('#EXTM3U'):raise ValueError('HLS manifest required')
 out=[];duration=None;stamp=None;init=None
 for line in text.splitlines():
  if line.startswith('#EXT-X-MAP:'):
   if line!='#EXT-X-MAP:URI="init.mp4"':raise ValueError('local init.mp4 required')
   init='init.mp4'
  elif line.startswith('#EXTINF:'):duration=int(round(float(line[8:].split(',')[0])*1000))
  elif line.startswith('#EXT-X-PROGRAM-DATE-TIME:'):
   value=datetime.fromisoformat(line.split(':',1)[1].replace('Z','+00:00'))
   if value.utcoffset() is None:raise ValueError('UTC-qualified programme time required')
   stamp=int(round(value.timestamp()*1000))
  elif line and not line.startswith('#'):
   if not re.fullmatch(r'seg_[0-9]{5,12}\.m4s',line) or duration is None or stamp is None or not 1<=duration<=60000:raise ValueError('unsafe, untimed or invalid live entry')
   if any(e['name']==line for e in out):raise ValueError('duplicate live entry')
   out.append({'name':line,'duration_ms':duration,'program_unix_ms':stamp});duration=None;stamp=None
 if out and init is None:raise ValueError('missing init')
 return out,'#EXT-X-ENDLIST' in text

def ffmpeg_command(args):
 command=['ffmpeg','-hide_banner','-loglevel','warning','-nostdin','-y']
 if args.file:command+=['-re','-i',str(args.file.resolve())]
 else:
  u=urlparse(args.rtmp_listen)
  if u.scheme!='rtmp' or u.hostname not in {'127.0.0.1','0.0.0.0','localhost'} or not u.port:raise ValueError('local RTMP listener required; publish Docker port on host loopback only')
  command+=['-listen','1','-i',args.rtmp_listen]
 if args.transcode:command+=['-c:v','libx264','-preset','veryfast','-crf','18','-tune','zerolatency','-force_key_frames',f'expr:gte(t,n_forced*{args.segment_seconds})','-c:a','aac']
 else:command+=['-c','copy']
 command+=['-f','hls','-hls_time',str(args.segment_seconds),'-hls_list_size','0','-hls_segment_type','fmp4','-hls_fmp4_init_filename','init.mp4','-hls_flags','independent_segments+program_date_time+temp_file','-hls_segment_filename',str(args.hls_dir/'seg_%05d.m4s'),str(args.hls_dir/'index.m3u8')]
 return command

def publish(args,on_chunk=None,policy_for_sequence=None):
 args.hls_dir=args.hls_dir.resolve();args.hls_dir.mkdir(mode=0o700,parents=True,exist_ok=True)
 journal=args.hls_dir/'publisher-state.json'
 saved=json.loads(journal.read_text()) if journal.exists() else {'live_id':'l-'+secrets.token_hex(16),'create_action':'live-create-'+secrets.token_hex(16),'chunks':[]}
 if args.file or args.rtmp_listen:
  if any((args.hls_dir/name).exists() for name in ('index.m3u8','init.mp4')) or any(args.hls_dir.glob('seg_*.m4s')):raise ValueError('refusing to overwrite existing capture; use a new directory or watch it without an input')
 journal.write_text(json.dumps(saved,indent=2));os.chmod(journal,0o600)
 report={'live_id':saved['live_id'],'measurements':saved['chunks'],'clock_basis':'FFmpeg programme date-time, not camera capture; clocks must agree','physical_lab':'NOT_TESTED'}
 password=(sys.stdin.readline().rstrip('\r\n') if getattr(args,'password_stdin',False) else os.environ.get('CARIBOU_LIVE_PASSWORD')) or getpass.getpass('Mot de passe Java (non enregistré): ')
 process=None;logfile=None;began=time.monotonic();last_status=began;reason='source_finished'
 with httpx.Client(timeout=240,trust_env=False) as c:
  def login():
   r=c.post(args.owner_url+'/owner/login',json={'email':args.email,'password':password});r.raise_for_status();return {'X-CSRF-Token':r.json()['csrf']}
  headers=login();last_login=time.monotonic()
  def send(kind,body):
   nonlocal headers,last_login
   if time.monotonic()-last_login>getattr(args,'reauth_seconds',900):headers=login();last_login=time.monotonic()
   for attempt in range(3):
    try:
     r=c.post(args.owner_url+'/owner/live/'+kind,json={**body,'live_id':saved['live_id']},headers=headers)
     if r.status_code<500:r.raise_for_status();return r.json()['state']
     r.raise_for_status()
    except httpx.HTTPError:
     if attempt==2:raise
     time.sleep(.5*(attempt+1))
  state=send('create',{'title':args.title,'public':args.public,'action_id':saved['create_action']})
  print(json.dumps({'live_id':state['id'],'state':'CREATED','viewer_path':'/live/?id='+state['id']}),flush=True)
  if state['ended']:return report
  try:
   if args.file or args.rtmp_listen:
    logfile=(args.hls_dir/'ffmpeg.log').open('ab');process=subprocess.Popen(ffmpeg_command(args),stdout=subprocess.DEVNULL,stderr=logfile)
   while True:
    if time.monotonic()-began>args.max_seconds:raise TimeoutError('configured live duration limit reached')
    if time.monotonic()-last_status>5:
     state=send('status',{'action_id':'live-status-'+saved['live_id'][2:]});last_status=time.monotonic()
     if state['ended']:reason=state.get('end_reason','publisher_stopped');break
    manifest=args.hls_dir/'index.m3u8'
    if not manifest.exists():
     if process and process.poll() is not None:raise RuntimeError('FFmpeg exited before producing a manifest; see local ffmpeg.log')
     if time.monotonic()-began>args.idle_timeout:raise TimeoutError('input did not start')
     time.sleep(.2);continue
    listed,ended=entries(manifest.read_text());seq=len(state['chunks'])
    if len(listed)-seq>args.max_pending:raise RuntimeError('publisher backlog limit exceeded; no fragment silently dropped')
    if seq<len(listed):
     item=listed[seq];init=(args.hls_dir/'init.mp4').read_bytes();fragment=(args.hls_dir/item['name']).read_bytes();start=time.perf_counter()
     action='live-chunk-'+hashlib.sha256((saved['live_id']+'/'+str(seq)).encode()).hexdigest()[:32]
     body={'action_id':action,'sequence':seq,'duration_ms':item['duration_ms'],'program_unix_ms':item['program_unix_ms'],'init':base64.b64encode(init).decode(),'fragment':base64.b64encode(fragment).decode(),'service_policy':policy_for_sequence(seq) if policy_for_sequence else args.policy}
     state=send('segment',body);entry={'sequence':seq,'source_sha256':hashlib.sha256(init+fragment).hexdigest(),'source_bytes':len(init)+len(fragment),'publish_request_ms':round((time.perf_counter()-start)*1000,3),'programme_age_at_commit_ms':int(time.time()*1000)-item['program_unix_ms'],'pending_observed':len(listed)-seq-1,'video_id':state['chunks'][seq]['video_id'],'owner_publish_ms':state['chunks'][seq]['publish_ms']}
     if len(saved['chunks'])==seq:saved['chunks'].append(entry)
     journal.write_text(json.dumps(saved,indent=2));report['measurements']=saved['chunks'];print(json.dumps(entry),flush=True)
     if on_chunk:on_chunk(state,item,init,fragment,process)
     continue
    if ended:break
    if process and process.poll() is not None:
     if process.returncode!=0:raise RuntimeError('FFmpeg failed; see local ffmpeg.log')
     break
    if time.monotonic()-began>args.max_seconds:raise TimeoutError('configured live duration limit reached')
    time.sleep(.2)
  except KeyboardInterrupt:reason='publisher_stopped'
  except Exception as e:
   reason='publisher_error:'+type(e).__name__;report['error']=str(e);raise
  finally:
   if process and process.poll() is None:
    process.terminate()
    try:process.wait(timeout=10)
    except subprocess.TimeoutExpired:process.kill();process.wait()
   if logfile:logfile.close()
   try:
    if not state['ended']:state=send('end',{'action_id':'live-end-'+hashlib.sha256(saved['live_id'].encode()).hexdigest()[:32],'reason':reason})
   finally:
    report.update(end_reason=reason,wall_seconds=round(time.monotonic()-began,3));(args.hls_dir/'measurements.json').write_text(json.dumps(report,indent=2))
  return report

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--password-stdin',action='store_true',help='read password from stdin, never command arguments');p.add_argument('--owner-url',default='http://127.0.0.1:19591');p.add_argument('--email',required=True);p.add_argument('--title',default='Direct CaribouTV');p.add_argument('--public',action='store_true');p.add_argument('--hls-dir',type=Path,required=True)
 inputs=p.add_mutually_exclusive_group();inputs.add_argument('--file',type=Path);inputs.add_argument('--rtmp-listen');p.add_argument('--transcode',action='store_true');p.add_argument('--segment-seconds',type=int,choices=range(2,13),default=4);p.add_argument('--max-pending',type=int,choices=range(1,33),default=8);p.add_argument('--max-seconds',type=int,default=3600);p.add_argument('--idle-timeout',type=int,default=300);p.add_argument('--budget',type=int,default=100000)
 args=p.parse_args()
 if not 1<=args.max_seconds<=3600 or not 1<=args.idle_timeout<=3600 or args.budget<1:p.error('duration/idle limits must be 1..3600 seconds and budget positive')
 args.policy={'max_rtt_ms':2000,'budget_microcredits':args.budget,'retention_hours':24};publish(args)
if __name__=='__main__':main()
