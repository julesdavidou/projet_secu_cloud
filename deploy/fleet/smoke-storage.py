"""Real per-owner backend check, separate from Go placement and SIS/IBS.
Creates only random probe objects, restarts only this bundle's tenant processes,
reads the same bytes, then removes only its own probe objects. No Docker socket
is mounted into any container. Run before announcing a new Edge to participants.
"""
import argparse
import json
from pathlib import Path
import uuid

from secure_client import command, read

PROBE=r'''
import hashlib,json,os,sys
from pathlib import Path
import httpx
from secure_services import ssl_client
registry=json.loads(Path('/registry/tenants.json').read_text())
phase,probe=sys.argv[1:];result=[]
with httpx.Client(verify=ssl_client(),trust_env=False,timeout=30) as c:
 for i,(owner,spec) in enumerate(sorted(registry.items())):
  data=(bytes(range(251))*300)[:31 if i==0 else 65537]
  digest=hashlib.sha256(data).hexdigest();url=spec['endpoint']
  headers={'Authorization':'Bearer '+spec['token']}
  path='/v1/storage/'+owner+'/'+probe
  if phase=='write':
   r=c.put(url+'/v1/verified/'+owner+'/'+probe,content=data,headers={**headers,'X-Content-SHA256':'0'*64});assert r.status_code==422,(r.status_code,r.text)
   r=c.put(url+'/v1/verified/'+owner+'/'+probe,content=data,headers={**headers,'X-Content-SHA256':digest});assert r.status_code==201,(r.status_code,r.text)
  r=c.get(url+path,headers=headers);assert r.status_code==200 and r.content==data,(r.status_code,'bytes differ')
  assert c.get(url+path).status_code==401
  for other in registry:
   if other!=owner:assert c.get(url+'/v1/storage/'+other+'/'+probe,headers=headers).status_code in (400,403)
  if phase=='cleanup':
   r=c.delete(url+path,headers=headers);assert r.status_code==204,(r.status_code,r.text)
   assert c.get(url+path,headers=headers).status_code==404
  result.append({'owner':owner,'bytes':len(data),'sha256':digest})
print(json.dumps({'phase':phase,'objects':result,'actual_volume_files':True,'sha256_not_sis_ibs':True}))
'''


def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--bundle',type=Path,required=True)
 p.add_argument('--allow-test-restart',action='store_true',required=True);p.add_argument('--output',type=Path,required=True)
 a=p.parse_args();folder=a.bundle.resolve();profile=read(folder/'profile.json')
 if 'storage' not in profile['roles']:raise ValueError('storage bundle required')
 node=profile['node_id'];project='caribou-client-'+profile['client_id'];runtime=folder/'.runtime'
 registry=read(runtime/('tenant-'+node)/'tenants.json')
 if sorted(registry)!=sorted(profile['owners']):raise ValueError('tenant registry differs from admitted test owners')
 containers=[]
 for owner in profile['owners']:
  ids=command(['docker','ps','-q','--filter','label=caribou.project='+project,'--filter','label=caribou.node='+node,'--filter','label=caribou.owner='+owner]).split()
  if len(ids)!=1:raise ValueError('one running, explicitly owned tenant process required')
  containers+=ids
 probe='onboarding-probe-'+uuid.uuid4().hex
 a.output.write_text(json.dumps({'node':node,'probe':probe,'state':'STARTED','failure_policy':'preserve probes for explicit diagnosis'}),encoding='utf-8')
 args=['docker','run','--rm','-i','--network',project+'_default','--cap-drop','ALL','--security-opt','no-new-privileges:true',
       '--mount',f'type=bind,source={runtime}/pki/edge-{node},target=/run/tls,readonly',
       '--mount',f'type=bind,source={runtime}/tenant-{node},target=/registry,readonly',
       '--env','TLS_CA_FILE=/run/tls/ca.crt','--env','TLS_CERT_FILE=/run/tls/tls.crt','--env','TLS_KEY_FILE=/run/tls/tls.key',
       profile['images']['security']['reference'],'python','-']
 results=[]
 results.append(json.loads(command(args+['write',probe],input=PROBE,timeout=120)))
 command(['docker','restart',*containers],timeout=60)
 # Docker restart returns before the listeners/health checks converge.
 import time
 for _ in range(40):
  if all(command(['docker','inspect','--format','{{.State.Health.Status}}',cid])=='healthy' for cid in containers):break
  time.sleep(1)
 else:raise ValueError('test tenant restart did not become healthy; probe objects preserved for diagnosis')
 results.append(json.loads(command(args+['read',probe],input=PROBE,timeout=120)))
 results.append(json.loads(command(args+['cleanup',probe],input=PROBE,timeout=120)))
 result={'node':node,'phases':results,'real_storage':'PASS','restart_persistence':'PASS','isolation_and_corruption_refusal':'PASS','Go_placement_and_SIS_IBS':'NOT_EXERCISED_BY_THIS_BACKEND_CHECK','second_physical_pc':'NOT_TESTED'}
 a.output.write_text(json.dumps(result,indent=2),encoding='utf-8');print(json.dumps(result,indent=2))


if __name__=='__main__':main()
