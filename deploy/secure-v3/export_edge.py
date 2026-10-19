"""Export one PRE-PROVISIONED laboratory Edge; never replace existing bundles.
Contains that node's credentials only. Keep the output private, never in Git.
"""
import argparse,ipaddress,json,os,shutil
from pathlib import Path
from urllib.parse import urlsplit
from broker_contract import connection

def export(node,controller,output,arch,runtime):
 ip=ipaddress.ip_address(controller)
 if ip.version!=4 or not ip.is_private or ip.is_loopback:raise ValueError('controller private LAN IPv4 required')
 cfg=json.loads((runtime/'config.json').read_text(encoding='utf-8'));spec=next((n for n in cfg['nodes'] if n['id']==node),None)
 if not spec:raise ValueError('Node is not provisioned. Start the PC using topology.lab.example.json first.')
 if node in {'A1','B1'}:raise ValueError('Use a new hardware identity (PI1/JETSON1), preserving the active PC nodes and their stored files.')
 actors=['edge-'+node,'broker-'+node,'reconstructor-'+node,'storage-issuer-'+node]
 for actor in actors:
  for name in ['ca.crt','tls.crt','tls.key']:
   if not (runtime/'pki'/actor/name).is_file():raise ValueError('Missing provisioned node certificate: '+actor)
 if output.exists():raise ValueError('Output exists; refusing to overwrite any bundle or credential')
 output.mkdir(parents=True);root=output/'.runtime';root.mkdir(mode=0o700)
 for actor in actors:shutil.copytree(runtime/'pki'/actor,root/'pki'/actor)
 shutil.copytree(runtime/'identities'/('edge-'+node),root/'identities'/('edge-'+node))
 (root/'broker').mkdir();shutil.copy2(runtime/'broker'/'acl',root/'broker'/'acl')
 for name in ['tenant-'+node,'lan-'+node]:(root/name).mkdir()
 (root/'config.json').write_text(json.dumps(cfg,indent=2),encoding='utf-8')
 here=Path(__file__).resolve().parent
 for name in ['lan.py','broker_contract.py','lan-requirements.txt','broker-entrypoint.sh','provision-tenant.ps1']:shutil.copy2(here/name,output/name)
 base='user mosquitto\nlistener 8883 0.0.0.0\nallow_anonymous false\nrequire_certificate true\nuse_identity_as_username true\ncafile /tmp/secure/ca.crt\ncertfile /tmp/secure/tls.crt\nkeyfile /tmp/secure/tls.key\nacl_file /tmp/secure/acl\npersistence true\npersistence_location /mosquitto/data/\nmax_packet_size 1048576\nlog_dest stdout\n'
 (output/'broker.conf').write_text(base+'\n'.join(connection(cfg,node,node+'-to-hub','broker',19683))+'\n',encoding='ascii',newline='\n')
 tls={'TLS_CA_FILE':'/run/tls/ca.crt','TLS_CERT_FILE':'/run/tls/tls.crt','TLS_KEY_FILE':'/run/tls/tls.key'}
 seeds=['broker','central','web','auth','is-a','is-b','zone-a','zone-b']
 hosts={name:str(ip) for name in seeds};project='caribou-edge-'+node.lower();control='caribou/secure-v3-control:lab-'+arch;security='caribou/secure-v3-security:lab-'+arch
 origins=','.join(n['endpoint'] for n in cfg['nodes']);zone_urls='https://zone-a:19691,https://zone-b:19692'
 edge_env={**tls,'CONTROLLERS_CONFIG':'/config/config.json','SECRETS_DIR':'/run/lab-secrets','MQTT_BROKER':'tls://broker-'+node+':8883','MQTT_PROTOCOL':'5','MQTT_JSON_ENVELOPE':'1','EDGE_ID':node,'EDGE_ZONE':spec['zone'],'EDGE_CONTROL_ENABLED':'1','EDGE_MODE':'real-http','EDGE_BACKEND_URL':'http://127.0.0.1:8080','EDGE_RECONSTRUCTOR_URL':'https://reconstructor-'+node+':8443','EDGE_LEDGER_PATH':'/data/control/edge-ledger.json','STORAGE_ROOT':'/data/files','HTTP_AUTH_TOKEN_FILE':'/run/lab-secrets/edge-'+node+'.http-token','HTTP_ADDR':'127.0.0.1:8088','CONTROLLER_HTTP_ADDR':'127.0.0.1:8088','TLS_ALLOWED_PEERS':'web,edge-*,zone-*,reconstructor-*,admin','STORAGE_BACKENDS_FILE':'/run/tenant-backends/tenants.json','DISCOVERY_ZONE_URLS':zone_urls}
 worker_env={**tls,'SECURITY_ROLE':'worker','DELEGATE_ID':'reconstructor-'+node,'WEB_URL':'https://web:19689','AUTH_URL':'https://auth:19583','TRUSTED_IS_URLS':json.dumps({'A':'https://is-a:19581','B':'https://is-b:19582'}),'ALLOWED_EDGE_ORIGINS':origins}
 services={
 'broker':{'image':'eclipse-mosquitto:2.0.22','entrypoint':['/bin/sh','/entrypoint.sh'],'environment':{'BRIDGE_NODE_ID':node},'volumes':['./broker-entrypoint.sh:/entrypoint.sh:ro','./broker.conf:/config/broker.conf:ro','./.runtime/broker:/provision:ro','./.runtime/pki/broker-'+node+':/run/tls:ro','./.runtime/pki/edge-'+node+':/run/bridge-tls:ro','./.runtime/lan-'+node+':/discovery:ro','broker-data:/mosquitto/data'],'ports':['8883:8883'],'networks':{'default':{'aliases':['broker-'+node]}}},
 'edge':{'image':control,'command':['tls-run','edge-agent'],'environment':edge_env,'volumes':['./.runtime/config.json:/config/config.json:ro','./.runtime/identities/edge-'+node+':/run/lab-secrets:ro','./.runtime/pki/edge-'+node+':/run/tls:ro','./.runtime/tenant-'+node+':/run/tenant-backends:ro','edge-data:/data'],'ports':['8443:8443'],'networks':{'default':{'aliases':[urlsplit(spec['endpoint']).hostname]}}},
 'reconstructor':{'image':security,'environment':worker_env,'volumes':['./.runtime/pki/reconstructor-'+node+':/run/tls:ro','worker-data:/data'],'ports':['9443:8443'],'networks':{'default':{'aliases':['reconstructor-'+node]}}}}
 for name,value in services.items():value.update(restart='unless-stopped',extra_hosts=hosts,init=True)
 config={'name':project,'services':services,'volumes':{k:{} for k in ['broker-data','edge-data','worker-data']}}
 (output/'compose.json').write_text(json.dumps(config,indent=2),encoding='utf-8')
 (output/'BUNDLE-INFO.json').write_text(json.dumps({'node':node,'project':project,'arch':arch,'controller_ip':str(ip),'exported_roles':actors,'excluded':['project root CA private key','zone authority keys','owner wallet keys','operator token'],'physical_hardware':'NOT_TESTED'},indent=2),encoding='utf-8')
 return {'bundle':str(output),'node':node,'arch':arch,'project':project,'secrets':'private output only; not printed'}

if __name__=='__main__':
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--node',required=True);p.add_argument('--controller-ip',required=True);p.add_argument('--output',type=Path,required=True);p.add_argument('--arch',choices=['amd64','arm64'],default='arm64');p.add_argument('--runtime',type=Path,default=Path(__file__).resolve().parent/'.runtime');a=p.parse_args();print(json.dumps(export(a.node,a.controller_ip,a.output.resolve(),a.arch,a.runtime.resolve()),indent=2))
