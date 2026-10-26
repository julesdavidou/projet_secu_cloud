"""Native Windows/Linux DNS-SD helper. Discovery hints never grant admission.
Run on the LAN host with Docker Desktop; multicast is not forwarded by its NAT.
No Docker API/socket, project root key, account password or placement algorithm.
"""
from __future__ import annotations
import argparse,ipaddress,json,os,re,socket,ssl,threading,time
from pathlib import Path
from urllib.parse import urlsplit
import httpx
from broker_contract import connection
from zeroconf import Zeroconf,ServiceBrowser,ServiceInfo,IPVersion,InterfaceChoice

SERVICE='_caribou-edge._tcp.local.'
IDENTITY=re.compile(r'^[A-Za-z0-9][A-Za-z0-9_.-]{0,100}$')

def publish_file(path,value):
 """Keep atomic publication despite short Windows/Docker sharing locks."""
 path=Path(path)
 if path.exists() and path.read_text(encoding='ascii')==value:return
 temp=path.with_name(path.name+'.next');temp.write_text(value,encoding='ascii')
 for attempt in range(6):
  try:os.replace(temp,path);return
  except PermissionError:
   if attempt==5:raise
   time.sleep(0.1*(attempt+1))

def private_ip(value):
 ip=ipaddress.ip_address(value)
 if ip.version!=4 or not ip.is_private or ip.is_loopback or ip.is_link_local:raise ValueError('private LAN IPv4 required')
 return str(ip)

def selected_ip(interface):
 if interface!='auto':return private_ip(interface)
 with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as s:
  s.connect(('224.0.0.251',5353));return private_ip(s.getsockname()[0])

def zone_request(c,method,origin,path,zone_ip=None,**kw):
 u=urlsplit(origin)
 if u.scheme!='https' or u.username or u.path or u.query or u.fragment:raise ValueError('canonical HTTPS zone origin required')
 headers={};extensions={};target=origin
 if zone_ip:
  ip=ipaddress.ip_address(zone_ip);target=u._replace(netloc=f'{ip}:{u.port or 443}').geturl();headers['Host']=u.netloc;extensions['sni_hostname']=u.hostname
 return c.request(method,target+path,headers=headers,extensions=extensions,**kw)

def peer_files(output,routes,topology,self_node):
 """Only generate disjoint own-origin bridges to provisioned, admitted peers.
 Mosquitto validates their stable DNS names; IPs are never used as TLS names.
 """
 nodes={n['id'] for n in topology['nodes']}
 if self_node not in nodes:raise ValueError('unprovisioned bridge identity')
 hosts=[];config=[];expiry=[]
 from datetime import datetime,timezone
 for node in sorted(nodes-{self_node}):
  route=routes.get(('broker-'+node+':8883').lower())
  if not route:continue
  until=datetime.fromisoformat(route['expires_at'].replace('Z','+00:00')).timestamp()
  if not time.time()<until<=time.time()+31:continue
  ip,port=route['address'].rsplit(':',1);ip=private_ip(ip);port=int(port)
  if not 1<=port<=65535:raise ValueError('invalid broker port')
  hostname='broker-'+node;hosts.append(ip+' '+hostname);expiry.append(until)
  config +=connection(topology,self_node,'discovered-'+self_node+'-to-'+node,hostname,port)
 folder=Path(output);folder.mkdir(parents=True,exist_ok=True)
 # Keep lease renewal separate: an unchanged peer set must not restart brokers.
 for name,value in [('hosts','\n'.join(hosts)+'\n'),('peers.conf','\n'.join(config)+'\n'),('expires',str(int(min(expiry))) if expiry else '0')]:
  publish_file(folder/name,value)

class Listener:
 def __init__(self):self.lock=threading.Lock();self.values={}
 def update_service(self,zc,type_,name):self.add_service(zc,type_,name)
 def remove_service(self,zc,type_,name):
  with self.lock:self.values.pop(name,None)
 def add_service(self,zc,type_,name):
  info=zc.get_service_info(type_,name,timeout=1500)
  if not info:return
  try:
   node=info.properties[b'node_id'].decode('ascii');broker=int(info.properties.get(b'broker_port',b'0'));broker_ip=info.properties.get(b'broker_ip',b'').decode('ascii');worker_port=int(info.properties.get(b'worker_port',b'0'));worker_ip=info.properties.get(b'worker_ip',b'').decode('ascii')
   if worker_ip:worker_ip=private_ip(worker_ip)
   if not 0<=worker_port<=65535:return
   if broker_ip:broker_ip=private_ip(broker_ip)
   if not IDENTITY.fullmatch(node) or not 0<=broker<=65535:return
   addresses=[private_ip(a) for a in info.parsed_addresses()][:2]
   if not addresses or not 1<=info.port<=65535:return
   with self.lock:
    if name not in self.values and len(self.values)>=64:return
    self.values[name]=(time.monotonic(),[{'node_id':node,'ip':ip,'http_port':info.port,'broker_port':broker,'broker_ip':broker_ip,'worker_port':worker_port,'worker_ip':worker_ip} for ip in addresses])
  except (KeyError,UnicodeError,ValueError):return

def advertise(args):
 if not IDENTITY.fullmatch(args.node):raise ValueError('invalid node identity')
 start=time.monotonic()
 while not args.duration or time.monotonic()-start<args.duration:
  interface=selected_ip(args.interface);address=private_ip(args.address or interface)
  with Zeroconf(interfaces=[interface],ip_version=IPVersion.V4Only) as zc:
   info=ServiceInfo(SERVICE,args.node+'.'+SERVICE,addresses=[socket.inet_aton(address)],port=args.http_port,properties={'node_id':args.node,'broker_port':str(args.broker_port),'broker_ip':private_ip(args.broker_address) if args.broker_address else '','worker_port':str(args.worker_port),'worker_ip':private_ip(args.worker_address) if args.worker_address else ''},server='caribou-'+args.node+'.local.',host_ttl=20,other_ttl=20)
   zc.register_service(info);print('ADVERTISED',args.node,flush=True)
   try:
    while not args.duration or time.monotonic()-start<args.duration:
     time.sleep(2)
     if args.interface=='auto' and selected_ip('auto')!=interface:break
   finally:zc.unregister_service(info)

def browse(args):
 context=ssl.create_default_context(cafile=str(Path(args.tls_dir)/'ca.crt'));context.minimum_version=ssl.TLSVersion.TLSv1_3
 context.load_cert_chain(str(Path(args.tls_dir)/'tls.crt'),str(Path(args.tls_dir)/'tls.key'))
 listener=Listener();start=time.monotonic();next_admit={}
 interfaces=InterfaceChoice.All if args.interface=='auto' else [private_ip(args.interface)]
 with Zeroconf(interfaces=interfaces,ip_version=IPVersion.V4Only) as zc,httpx.Client(verify=context,timeout=8,trust_env=False,follow_redirects=False) as c:
  browser=ServiceBrowser(zc,SERVICE,listener)
  try:
   while not args.duration or time.monotonic()-start<args.duration:
    with listener.lock:values=list(listener.values.values())
    for observed,candidates in values:
     if time.monotonic()-observed>25:continue
     for q in candidates:
      key=(q['node_id'],q['ip'])
      if time.monotonic()<next_admit.get(key,0):continue
      next_admit[key]=time.monotonic()+10
      try:r=zone_request(c,'POST',args.zone_url,'/api/v1/discovery/candidates',args.zone_ip,json=q);print('ADMISSION',q['node_id'],r.status_code,flush=True)
      except httpx.HTTPError as e:print('UNAVAILABLE',q['node_id'],type(e).__name__,flush=True)
    try:
     r=zone_request(c,'GET',args.zone_url,'/api/v1/discovery/routes',args.zone_ip);r.raise_for_status();routes=r.json()
     if args.output:peer_files(args.output,routes,json.loads(Path(args.topology).read_text(encoding='utf-8')),args.self_node)
     if args.results:Path(args.results).write_text(json.dumps(routes,indent=2),encoding='utf-8')
    except (httpx.HTTPError,OSError) as e:print('DIRECTORY_UNAVAILABLE',type(e).__name__,flush=True)
    time.sleep(2)
    # DNS-SD cache entries outlive the admission lease. Refresh from the
    # library's live cache so services are periodically re-probed by the zone.
    with listener.lock:names=list(listener.values)
    for name in names:listener.add_service(zc,SERVICE,name)
  finally:browser.cancel()

def mesh(args):
 context=ssl.create_default_context(cafile=str(Path(args.tls_dir)/'ca.crt'));context.minimum_version=ssl.TLSVersion.TLSv1_3;context.load_cert_chain(str(Path(args.tls_dir)/'tls.crt'),str(Path(args.tls_dir)/'tls.key'));start=time.monotonic()
 with httpx.Client(verify=context,timeout=8,trust_env=False) as c:
  while not args.duration or time.monotonic()-start<args.duration:
   try:
    r=zone_request(c,'GET',args.zone_url,'/api/v1/discovery/routes',args.zone_ip);r.raise_for_status();peer_files(args.output,r.json(),json.loads(Path(args.topology).read_text(encoding='utf-8')),args.self_node)
   except (httpx.HTTPError,OSError) as e:print('DIRECTORY_UNAVAILABLE',type(e).__name__,flush=True)
   time.sleep(5)

def main():
 p=argparse.ArgumentParser(description=__doc__);sub=p.add_subparsers(dest='mode',required=True)
 a=sub.add_parser('advertise');a.add_argument('--node',required=True);a.add_argument('--http-port',type=int,default=8443);a.add_argument('--broker-port',type=int,default=8883);a.add_argument('--address');a.add_argument('--broker-address');a.add_argument('--worker-port',type=int,default=0);a.add_argument('--worker-address')
 b=sub.add_parser('browse');b.add_argument('--zone-url',required=True);b.add_argument('--zone-ip');b.add_argument('--tls-dir',required=True);b.add_argument('--output');b.add_argument('--topology');b.add_argument('--self-node');b.add_argument('--results')
 m=sub.add_parser('mesh');m.add_argument('--zone-url',required=True);m.add_argument('--zone-ip');m.add_argument('--tls-dir',required=True);m.add_argument('--output',required=True);m.add_argument('--topology',required=True);m.add_argument('--self-node',required=True)
 for item in (a,b):item.add_argument('--interface',default='auto')
 for item in (a,b,m):item.add_argument('--duration',type=int,default=0)
 args=p.parse_args();{'advertise':advertise,'browse':browse,'mesh':mesh}[args.mode](args)
if __name__=='__main__':main()
