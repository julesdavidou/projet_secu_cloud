"""Temporary capacity reservation only: never writes or acknowledges fake files.
Used by smoke.ps1 to force actual new video blocks onto both zones without
changing the scheduler or provisioned quotas. Cancellation is run in finally.
"""
import json,os,secrets,ssl,time
from pathlib import Path
import httpx

ctx=ssl.create_default_context(cafile='/pki/ca.crt');ctx.load_cert_chain('/pki/tls.crt','/pki/tls.key')
headers={'Authorization':'Bearer '+Path('/operator/operator.token').read_text().strip()}
path=Path('/fixture/hold-'+os.environ['HOLD_SLOT']+'.json')
with httpx.Client(verify=ctx,headers=headers,timeout=60,trust_env=False) as c:
 def request(route,body=None):
  r=c.get('https://central:8443'+route) if body is None else c.post('https://central:8443'+route,json=body)
  r.raise_for_status();return r.json()
 def cancel(hold):
  request('/api/v1/rpc/central/CANCEL',{'video_id':hold,'reservation_id':'unused'})
 if os.environ['HOLD_ACTION']=='reserve':
  if path.exists():cancel(json.loads(path.read_text())['video_id'])
  time.sleep(3)
  free=request('/api/v1/snapshot')['nodes']['A1']['node']['available_for_project_bytes'];reserved=free-500000
  assert reserved>0,'source capacity too small for this fixture'
  hold='secure-test-'+secrets.token_hex(8)+'-hold'
  # Persist the ID before network submission so a failed invocation is recoverable.
  path.write_text(json.dumps({'video_id':hold,'bytes':reserved,'stored':False}))
  upload={'manifest':{'request_id':hold,'video_id':hold,'source_node_id':'A1','total_size_bytes':reserved,'segments':[{'segment_id':0,'size_bytes':reserved}]},'target_size_bytes':1,'contents':[{'extent_id':0,'sha256':'0'*64}]}
  plan=request('/api/v1/uploads',upload);assert plan['state']!='READY' and all(u['node_id']=='A1' for u in plan['units'])
  print('PASS temporary capacity reservation, NO fake stored bytes',flush=True)
 else:
  cancel(json.loads(path.read_text())['video_id'])
  print('PASS temporary reservation cancelled through authoritative catalogue',flush=True)
