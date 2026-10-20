"""Individual secure laptop bundles. Reuses V3 Edge export, signing and Fleet image resolution.
No SSH deployment, discovery-based admission, new placement, or permanent Docker provisioner.
"""
from __future__ import annotations
import argparse,copy,getpass,hashlib,ipaddress,json,os,re,shutil,ssl,subprocess,sys
from pathlib import Path
HERE=Path(__file__).resolve().parent
ROOT=HERE.parents[1]

def read(path):return json.loads(Path(path).read_text(encoding='utf-8-sig'))
def write(path,value):Path(path).write_text(json.dumps(value,indent=2),encoding='utf-8')
def command(args,**kw):
 p=subprocess.run([str(a) for a in args],capture_output=True,text=True,encoding='utf-8',errors='replace',**kw)
 if p.returncode:raise ValueError('Command failed: '+str(args[0])+' '+str(args[1])+'; '+(p.stderr or p.stdout)[-1200:])
 return p.stdout.strip()
def image_info(ref):return json.loads(command(['docker','image','inspect',ref]))[0]
def private_ip(value):
 ip=ipaddress.ip_address(value)
 if ip.version!=4 or not ip.is_private or ip.is_loopback or ip.is_link_local:raise ValueError('controller private LAN IPv4 required')
 return str(ip)
def roles(value):
 result=sorted(set(x.strip() for x in value.lower().split(',')))
 if not result or set(result)-{'viewer','publisher','storage'}:raise ValueError('roles: viewer,publisher,storage')
 if 'publisher' in result and 'viewer' not in result:result.append('viewer')
 return sorted(result)
def client_actors(client,selected):
 if not re.fullmatch('[a-z][a-z0-9-]{2,31}',client):raise ValueError('client id: 3..32 lowercase letters/digits/hyphens')
 return (['viewer-lab-'+client] if 'viewer' in selected else [])+(['owner-lab-'+client] if 'publisher' in selected else [])
def quota_bytes(gib,maximum):
 try:amount=float(gib)
 except (ValueError,TypeError):raise ValueError('invalid quota GiB') from None
 if not 0<amount<=maximum/2**30:raise ValueError('quota exceeds admitted maximum')
 result=int(amount*2**30)
 if result<1024*1024:raise ValueError('minimum quota is 1 MiB')
 return result
def make_bundle(runtime,out,client,selected,controller,images,owners,node=None,offset=0,save_images=False,client_offset=0):
 runtime=Path(runtime).resolve();out=Path(out).resolve();controller=private_ip(controller);actors=client_actors(client,selected);cfg=read(runtime/'config.json')
 if not 0<=client_offset<=30000:raise ValueError('client port offset outside 0..30000')
 view_port,owner_port,rtmp_port=18790+client_offset,18791+client_offset,22935+client_offset
 if out.exists():raise ValueError('output exists; never overwrite an identity bundle')
 if any(not re.fullmatch(r'client-[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}',o) for o in owners):raise ValueError('real stable Java owner ids required')
 spec=None
 if 'storage' in selected:
  spec=next((n for n in cfg['nodes'] if n['id']==node),None)
  if spec is None:raise ValueError('storage node must first be admitted in controller topology')
  if not owners:raise ValueError('approve at least one real owner for storage; no fake STORED or unprovisioned storage')
 for actor in actors:
  if not all((runtime/'pki'/actor/f).is_file() for f in ['ca.crt','tls.crt','tls.key']):raise ValueError('provision transport client first: '+actor)
 if spec:
  sys.path.insert(0,str(HERE.parent/'secure-v3'))
  from export_edge import export
  export(node,controller,out,'amd64',runtime)
  compose=read(out/'compose.json')
  compose['name']='caribou-client-'+client
  for name,host_port,target_port in [('edge',8443,8443),('broker',8883,8883),('reconstructor',9443,8443)]:
   compose['services'][name]['ports']=[f'{host_port+client_offset}:{target_port}']
  # Existing export uses V3 port names; offset is for isolated tests only.
  if offset:
   for service in compose['services'].values():
    for key,val in service.get('environment',{}).items():
     if isinstance(val,str):
      for port in [19683,19688,19689,19691,19692,19581,19582,19583]:val=val.replace(':'+str(port),':'+str(port+offset))
      service['environment'][key]=val
   broker=out/'broker.conf';text=broker.read_text();broker.write_text(text.replace(':19683',':'+str(19683+offset)),encoding='ascii')
  compose['services']['edge']['image']=images['control']['reference'];compose['services']['reconstructor']['image']=images['security']['reference'];compose['services']['broker']['image']=images['broker']['reference']
 else:
  out.mkdir(parents=True);(out/'.runtime').mkdir();compose={'name':'caribou-client-'+client,'services':{},'volumes':{}}
 for actor in actors:shutil.copytree(runtime/'pki'/actor,out/'.runtime'/'pki'/actor)
 tls=lambda actor:{'TLS_CA_FILE':'/run/tls/ca.crt','TLS_CERT_FILE':'/run/tls/tls.crt','TLS_KEY_FILE':'/run/tls/tls.key'}
 if 'viewer' in selected:
  actor='viewer-lab-'+client
  compose['services']['portal']={'image':images['control']['reference'],'command':['client-portal'],'environment':{**tls(actor),'GATEWAY_ADDRESS':controller+':'+str(19693+offset)},'volumes':['./.runtime/pki/'+actor+':/run/tls:ro'],'ports':[f'127.0.0.1:{view_port}:8080'],'healthcheck':{'test':['CMD','wget','-q','-O','-','http://127.0.0.1:8080/ready'],'interval':'3s','timeout':'5s','retries':40}}
 if 'publisher' in selected:
  actor='owner-lab-'+client;origins=','.join(n['endpoint'] for n in cfg['nodes']);workers={('reconstructor-'+n['id']):'https://reconstructor-'+n['id']+':8443' for n in cfg['nodes']}
  # Discovery supplies the actual worker routes; these are stable TLS identities.
  compose['services']['owner']={'image':images['security']['reference'],'environment':{**tls(actor),'SECURITY_ROLE':'owner','AUTH_URL':'https://auth:'+str(19583+offset),'WEB_URL':'https://web:'+str(19689+offset),'TRUSTED_IS_URLS':json.dumps({'A':'https://is-a:'+str(19581+offset),'B':'https://is-b:'+str(19582+offset)}),'ALLOWED_EDGE_ORIGINS':origins,'OWNER_INITIAL_ZONE':'A','OWNER_BROWSER_ORIGIN':f'http://localhost:{owner_port}','DELEGATE_WORKERS':json.dumps(workers),'DISCOVERY_ZONE_URLS':'https://zone-a:'+str(19691+offset)+',https://zone-b:'+str(19692+offset)},'volumes':['./.runtime/pki/'+actor+':/run/tls:ro','owner-data:/data'],'ports':[f'127.0.0.1:{owner_port}:9300',f'127.0.0.1:{rtmp_port}:1935']}
  compose['volumes']['owner-data']={}
 for value in compose['services'].values():
  value.update(restart='unless-stopped',init=True,labels={'caribou.client':client,'caribou.profile':'secure-v3'},extra_hosts={h:controller for h in ['central','web','auth','is-a','is-b','zone-a','zone-b','broker']})
  value.setdefault('cap_drop',['ALL']);value.setdefault('security_opt',['no-new-privileges:true'])
 if spec:
  # Existing Mosquitto entrypoint copies private certificates, changes their
  # ownership/mode, then Mosquitto drops to its own UID/GID. Preserve only the
  # capabilities necessary for those operations, never a Docker engine mount.
  compose['services']['broker']['cap_add']=['CHOWN','DAC_OVERRIDE','FOWNER','SETUID','SETGID']
 write(out/'compose.json',compose)
 ca=next((runtime/'pki'/a/'ca.crt' for a in actors),runtime/'pki'/('edge-'+node)/'ca.crt' if node else runtime/'pki'/'admin'/'ca.crt')
 ca_digest=hashlib.sha256(ssl.PEM_cert_to_DER_cert(ca.read_text())).hexdigest()
 profile={'schema':1,'client_id':client,'roles':selected,'controller_ip':controller,'port_offset':offset,'node_id':node,'max_quota_bytes':spec['quota_bytes'] if spec else 0,'owners':owners,'images':images,'ca_sha256_der':ca_digest,'storage_limit':'logical project quota including reserved/stored blocks, not a filesystem disk reservation','viewer_url':f'http://localhost:{view_port}/','owner_url':f'http://localhost:{owner_port}/','rtmp_url':f'rtmp://127.0.0.1:{rtmp_port}/live','storage_ports':{'http':8443+client_offset,'broker':8883+client_offset,'worker':9443+client_offset}}
 for source,dest in [('windows.ps1','start.ps1'),('client-publish.ps1','publish.ps1'),('secure_client.py','secure_client.py'),('registry.py','registry.py'),('fleet.py','fleet.py')]:shutil.copy2(HERE/source,out/dest)
 if save_images:command(['docker','image','save','-o',str(out/'images.tar'),*[x['reference'] for x in images.values()]],timeout=1200)
 write(out/'profile.json',profile)
 return {'bundle':str(out),'client':client,'roles':selected,'root_private_key_exported':False}

def install(folder,quota=None,registry_user=None):
 folder=Path(folder).resolve();profile=read(folder/'profile.json');runtime=folder/'.runtime';runtime.mkdir(exist_ok=True)
 if profile.get('schema')!=1:raise ValueError('unsupported bundle schema')
 from fleet import acquire_lock,pull_local,validate_image,compose_version
 lock=acquire_lock(runtime)
 try:
  compose_version(command(['docker','compose','version','--short']))
  if command(['docker','info','--format','{{.OSType}}/{{.Architecture}}']) not in ['linux/x86_64','linux/amd64']:raise ValueError('Windows pack requires Docker Desktop in Linux AMD64 mode')
  selected=list(profile['roles'])
  if 'storage' in selected and quota is not None and float(quota)==0:selected.remove('storage')
  if not selected:raise ValueError('at least one role must remain enabled')
  limit=quota_bytes(quota,profile['max_quota_bytes']) if 'storage' in selected else 0
  state={'roles':selected,'quota_bytes':limit,'client_id':profile['client_id']}
  old=runtime/'installation.json'
  if old.exists() and read(old)!=state:raise ValueError('existing installation differs; explicit migration required, no data deletion')
  plans=profile['images']
  if (folder/'images.tar').exists():command(['docker','image','load','-i',str(folder/'images.tar')],timeout=1200)
  registry=None
  for plan in plans.values():
   if plan['reference'].startswith('ghcr.io/'):
    from registry import Registry
    if registry is None:registry=Registry(registry_user or os.environ.get('CARIBOU_GHCR_USERNAME',''),os.environ.get('CARIBOU_GHCR_TOKEN') or getpass.getpass('GHCR read:packages token (not saved): '))
    pull_local(plan,registry,runtime)
   elif plan.get('public_registry'):
    command(['docker','pull','--platform','linux/amd64',plan['reference']],timeout=600)
    info=image_info(plan['reference'])
    if info.get('Os')!='linux' or info.get('Architecture')!='amd64':raise ValueError('public broker image platform mismatch')
   else:
    info=image_info(plan['reference'])
    if info['Id']!=plan['image_id']:raise ValueError('local image does not match the admitted bundle; load its images archive')
  compose=read(folder/'compose.json');project='caribou-client-'+profile['client_id']
  if compose['name']!=project:raise ValueError('bundle project mismatch')
  if 'storage' not in selected:
   for name in ['edge','broker','reconstructor']:compose['services'].pop(name,None)
  for cid in command(['docker','ps','-aq','--filter','label=com.docker.compose.project='+project]).split():
   label=command(['docker','inspect','--format','{{index .Config.Labels "caribou.client"}}',cid])
   if label!=profile['client_id']:raise ValueError('refusing to replace an unrelated container')
  # Freeze admitted identity/quota before any data-bearing container starts.
  write(old,state)
  write(folder/'compose.json',compose)
  if 'storage' in selected:
   cfg=read(runtime/'config.json');node=profile['node_id'];spec=next(n for n in cfg['nodes'] if n['id']==node);spec['quota_bytes']=limit;write(runtime/'config.json',cfg)
   command(['docker','compose','--project-directory',str(folder),'-f',str(folder/'compose.json'),'up','-d','broker','reconstructor'],timeout=180)
   # Provision only explicitly approved users; existing V3 command validates mounts/labels.
   for owner in profile['owners']:
    command(['pwsh','-NoProfile','-File',str(folder/'provision-tenant.ps1'),'-Owner',owner,'-Node',node,'-Project',project,'-RuntimeDirectory',str(runtime),'-ControlImage',plans['control']['reference'],'-SecurityImage',plans['security']['reference']],cwd=folder,timeout=240)
  command(['docker','compose','--project-directory',str(folder),'-f',str(folder/'compose.json'),'up','-d','--wait','--wait-timeout','180'],timeout=240)
  write(old,state)
  return {'client':profile['client_id'],'roles':selected,'quota_bytes':limit,'viewer':profile['viewer_url'] if 'viewer' in selected else None,'remote_hardware_tested':False}
 finally:lock.close()

def main():
 p=argparse.ArgumentParser(description=__doc__);sub=p.add_subparsers(dest='action',required=True)
 b=sub.add_parser('bundle');b.add_argument('--runtime',type=Path,default=ROOT/'deploy/secure-v3/.runtime');b.add_argument('--output',type=Path,required=True);b.add_argument('--client',required=True);b.add_argument('--roles',default='viewer');b.add_argument('--controller-ip',required=True);b.add_argument('--node');b.add_argument('--owners',default='');b.add_argument('--control-image',default='caribou/secure-v3-control:dev');b.add_argument('--security-image',default='caribou/secure-v3-security:dev');b.add_argument('--save-images',action='store_true');b.add_argument('--port-offset',type=int,default=0);b.add_argument('--client-port-offset',type=int,default=0);b.add_argument('--image-commit');b.add_argument('--repository',default='julesdavidou/projet_secu_cloud');b.add_argument('--registry-user')
 i=sub.add_parser('install');i.add_argument('--bundle',type=Path,required=True);i.add_argument('--quota-gib');i.add_argument('--registry-user')
 a=p.parse_args()
 if a.action=='bundle':
  selected=roles(a.roles);components={'control':a.control_image}
  if set(selected)&{'publisher','storage'}:components['security']=a.security_image
  if a.image_commit:
   from registry import Registry
   registry=Registry(a.registry_user or os.environ.get('CARIBOU_GHCR_USERNAME',''),os.environ.get('CARIBOU_GHCR_TOKEN') or getpass.getpass('GHCR read:packages token (not saved): '))
   images={k:registry.resolve(a.repository,'control' if k=='control' else 'secure-security',a.image_commit,'linux/amd64',require_client_portal=k=='control') for k in components}
   if a.save_images:raise ValueError('save-images requires local image mode; registry bundles pull immutable digests')
  else:
   images={k:{'reference':image_info(v)['Id'],'image_id':image_info(v)['Id'],'platform':'linux/amd64'} for k,v in components.items()}
   if image_info(a.control_image).get('Config',{}).get('Labels',{}).get('io.caribou.client-portal')!='1':raise ValueError('control image lacks the secure client portal capability; build this branch first')
  if 'storage' in selected:
   broker=image_info('eclipse-mosquitto:2.0.22')
   images['broker']={'reference':broker['Id'],'image_id':broker['Id'],'platform':'linux/amd64'}
   if a.image_commit:
    digest=next((r for r in broker.get('RepoDigests',[]) if r.startswith('eclipse-mosquitto@sha256:') or r.startswith('eclipse-mosquitto:') or r.startswith('docker.io/library/eclipse-mosquitto@sha256:')),None)
    if not digest or '@sha256:' not in digest:raise ValueError('public Mosquitto image requires an immutable registry digest')
    images['broker']={'reference':digest,'platform':'linux/amd64','public_registry':True}
  result=make_bundle(a.runtime,a.output,a.client,selected,a.controller_ip,images,[x for x in a.owners.split(',') if x],a.node,a.port_offset,a.save_images,a.client_port_offset)
 else:result=install(a.bundle,a.quota_gib,a.registry_user)
 print(json.dumps(result,indent=2))
if __name__=='__main__':
 try:main()
 except (ValueError,OSError,subprocess.TimeoutExpired) as e:print(str(e),file=sys.stderr);sys.exit(1)
