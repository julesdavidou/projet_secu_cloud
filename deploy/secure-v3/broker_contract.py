"""Exact own-origin MQTT bridge contract, shared by LAN and Edge export."""
def outgoing(topology,node):
 nodes={n['id'] for n in topology['nodes']}
 if node not in nodes:raise ValueError('unprovisioned bridge identity')
 actors=['central','admin']+['zone-'+z for z in topology['zones']]+['edge-'+n for n in sorted(nodes)]
 own='edge-'+node;root='lab/placement/v1/'
 topics=[root+'presence/'+own,root+'zone/+/node/'+node+'/+',root+'central/request/'+own]
 topics +=[root+'zone-'+z+'/request/'+own for z in topology['zones']]
 topics +=[root+a+'/reply/'+own for a in actors if a!=own]
 topics +=[root+a+'/request/'+own for a in actors if a.startswith('edge-') and a!=own]
 return topics

def connection(topology,node,name,hostname,port):
 value=['connection '+name,'local_username bridge-'+node,'address '+hostname+':'+str(port),'remote_clientid '+name,'bridge_protocol_version mqttv50','bridge_cafile /tmp/secure/ca.crt','bridge_certfile /tmp/secure/client.crt','bridge_keyfile /tmp/secure/client.key','bridge_insecure false','notifications false','cleansession true','restart_timeout 1']
 value +=['topic '+topic+' out 1' for topic in outgoing(topology,node)]
 value +=['topic lab/placement/v1/edge-'+node+'/request/+ in 1','topic lab/placement/v1/edge-'+node+'/reply/+ in 1']
 return value
