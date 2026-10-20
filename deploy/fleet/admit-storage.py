"""Controller-side explicit admission, preserving all existing nodes and data.
Run once as the Docker administrator. Never deploys to the remote machine.
"""
import argparse
import json
from pathlib import Path
import re
import shutil
import sys
import time

from secure_client import command, read, write


def admitted_config(cfg, node, zone, maximum):
 if not re.fullmatch('[A-Z][A-Z0-9]{2,31}',node):raise ValueError('new node id: 3..32 uppercase letters/digits')
 if zone not in cfg['zones']:raise ValueError('unknown zone')
 if not 1024*1024 <= maximum <= 1024**4:raise ValueError('maximum quota: 1 MiB..1 TiB')
 if any(n['id']==node for n in cfg['nodes']):raise ValueError('existing node: use its existing bundle; explicit migration required to change admission')
 value=json.loads(json.dumps(cfg))
 value['discovery_enabled']=True
 value['nodes'].append({'id':node,'zone':zone,'quota_bytes':maximum,'endpoint':'https://edge-'+node+':8443','allowed_zones':list(cfg['zones']),'mode':'real-http','microcredits_per_gib_hour':1000000})
 return value


def main():
 p=argparse.ArgumentParser(description=__doc__)
 p.add_argument('--runtime',type=Path,required=True);p.add_argument('--compose',type=Path,required=True)
 p.add_argument('--override',action='append',type=Path,default=[])
 p.add_argument('--control-image',required=True);p.add_argument('--node',required=True);p.add_argument('--zone',required=True)
 p.add_argument('--maximum-gib',type=float,required=True);p.add_argument('--apply',action='store_true')
 p.add_argument('--additional',action='append',default=[],metavar='NODE:ZONE:MAX_GIB',help='batch explicit admissions in one controller reload')
 a=p.parse_args();runtime=a.runtime.resolve();base=a.compose.resolve();cfg=read(runtime/'config.json')
 maximum=int(a.maximum_gib*2**30);value=admitted_config(cfg,a.node,a.zone,maximum)
 for extra in a.additional:
  parts=extra.split(':')
  if len(parts)!=3:raise ValueError('additional node format: NODE:ZONE:MAX_GIB')
  value=admitted_config(value,parts[0],parts[1],int(float(parts[2])*2**30))
 additions=value['nodes'][len(cfg['nodes']):]
 if not a.apply:
  print(json.dumps({'mode':'PLAN_ONLY','new_nodes':additions,'existing_nodes_preserved':[n['id'] for n in cfg['nodes']]}));return
 for path in [base,*a.override]:
  if not path.is_file():raise ValueError('missing compose overlay: '+str(path))
 backup=runtime/('config-before-'+str(time.time_ns())+'.json');shutil.copy2(runtime/'config.json',backup)
 candidate=runtime/'topology.admission.json';write(candidate,value)
 command(['docker','run','--rm','--network','none','--user','0:0','--mount',f'type=bind,source={candidate},target=/input/topology.json,readonly','--mount',f'type=bind,source={runtime},target=/output',a.control_image,'lab-admin','-mode','real-http','-secure-v3'],timeout=180)
 services=read(base)['services'];origins=','.join(n['endpoint'] for n in value['nodes'])
 changed={name:{'environment':{'ALLOWED_EDGE_ORIGINS':origins}} for name,spec in services.items() if 'ALLOWED_EDGE_ORIGINS' in spec.get('environment',{})}
 overlay=runtime/'client.storage.json';write(overlay,{'services':changed})
 command_base=['docker','compose','--project-directory',str(base.parent),'-f',str(base)]
 for path in [*a.override,overlay]:command_base+=['-f',str(path.resolve())]
 # Reload config and explicit endpoint allowlists; all volumes/certificates stay.
 names=[n for n in ['central','zone-a','zone-b',*changed] if n in services and not n.startswith('test-')]
 # Config files do not change the Compose service hash. Recreate explicitly:
 # Go reads topology at startup, and bind mounts may still reference old inodes.
 command(command_base+['up','-d','--no-deps','--force-recreate','--wait','--wait-timeout','180',*names],timeout=240)
 # Brokers copy ACLs on startup, so existing brokers must refresh their ACLs.
 brokers=[n for n in ['broker','broker-a','broker-b'] if n in services]
 command(command_base+['up','-d','--no-deps','--force-recreate','--wait',*brokers],timeout=180)
 print(json.dumps({'admitted_nodes':additions,'backup':str(backup),'remote_machine_modified':False,'discovery_and_real_storage_test':'PENDING'}))


if __name__=='__main__':
 try:main()
 except (ValueError,OSError) as e:print(str(e),file=sys.stderr);sys.exit(1)
