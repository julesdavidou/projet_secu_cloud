"""Actual multicast, TLS admission, dynamic broker config and HTTP routing.
Advertisers are separate processes in each real Edge network namespace.
"""
import json,ssl,time,os
from pathlib import Path
import httpx

def client(actor):
 root=Path('/pki')/actor;c=ssl.create_default_context(cafile=str(root/'ca.crt'));c.minimum_version=ssl.TLSVersion.TLSv1_3;c.load_cert_chain(str(root/'tls.crt'),str(root/'tls.key'));return httpx.Client(verify=c,timeout=10,trust_env=False)

with client('zone-A') as c:
 routes={}
 for _ in range(30):
  r=c.get('https://zone-a:8443/api/v1/discovery/routes');assert r.status_code==200,(r.status_code,r.text);routes=r.json()
  if all(k in routes for k in ['edge-a:8443','edge-b:8443','broker-a1:8883','broker-b1:8883','reconstructor-a1:8443','reconstructor-b1:8443']):break
  time.sleep(2)
 else:raise AssertionError('DNS-SD peers not admitted: '+str(routes))
 assert routes['edge-a:8443']['address']==os.environ['EDGE_A_IP']+':8443'
 assert routes['edge-b:8443']['address']==os.environ['EDGE_B_IP']+':8443'
 # A claimed ID cannot take over another Edge's TLS endpoint.
 time.sleep(2)
 r=c.post('https://zone-a:8443/api/v1/discovery/candidates',json={'node_id':'A1','ip':os.environ['EDGE_B_IP'],'http_port':8443});assert r.status_code==422,(r.status_code,r.text)
 for q in [{'node_id':'unprovisioned','ip':os.environ['EDGE_A_IP'],'http_port':8443},{'node_id':'A1','ip':'8.8.8.8','http_port':443}]:
  r=c.post('https://zone-a:8443/api/v1/discovery/candidates',json=q);assert r.status_code==422
with client('edge-A1') as c:
 r=c.post('https://zone-a:8443/api/v1/discovery/candidates',json={'node_id':'A1','ip':os.environ['EDGE_A_IP'],'http_port':8443});assert r.status_code==403
result={'multicast':'actual Docker bridge UDP5353','tls_admission':'PASS','wrong_identity':'REFUSED','unknown_device':'REFUSED','public_ip':'REFUSED','edge_cannot_admit_itself':'PASS','routes':routes,'physical_lan':'NOT_TESTED','windows_multicast_nat':'native helper required'}
Path('/results/discovery.json').write_text(json.dumps(result,indent=2));print(json.dumps(result,indent=2),flush=True)
